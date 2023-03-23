/*
Copyright 2023 The access Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package access

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/access-io/access/bpf/blips"
	accessv1alpha1 "github.com/access-io/access/pkg/apis/access/v1alpha1"
	accessversioned "github.com/access-io/access/pkg/generated/clientset/versioned"
	"github.com/access-io/access/pkg/generated/clientset/versioned/scheme"
	accessinformers "github.com/access-io/access/pkg/generated/informers/externalversions/access/v1alpha1"
	accesslisters "github.com/access-io/access/pkg/generated/listers/access/v1alpha1"
	"github.com/access-io/access/pkg/util/ebpfmap"
	"github.com/access-io/access/pkg/util/linux"
)

const (
	// maxRetries is the number of times a deployment will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the times
	// a deployment is going to be requeued:
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries          = 15
	controllerAgentName = "access-agent"
)

var defaultValue = uint32(1)

// Controller define the option of controller
type Controller struct {
	kubeClient kubernetes.Interface
	client     accessversioned.Interface

	engine   *blips.EbpfEngine
	nodeName types.NodeName

	// lister define the cache object
	lister     accesslisters.AccessLister
	nodeLister corelisters.NodeLister

	// synced define the sync for relist
	accessSynced cache.InformerSynced
	nodeSynced   cache.InformerSynced

	// Access that need to be synced
	queue workqueue.RateLimitingInterface

	eventBroadcaster record.EventBroadcaster
	eventRecorder    record.EventRecorder
}

// NewController return a controller and add event handler
func NewController(
	ctx context.Context,
	kubeClient kubernetes.Interface,
	client accessversioned.Interface,
	informer accessinformers.AccessInformer,
	nodeInformer coreinformers.NodeInformer,
	engine *blips.EbpfEngine) (*Controller, error) {
	logger := klog.FromContext(ctx)

	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	logger.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	controller := &Controller{
		client:           client,
		kubeClient:       kubeClient,
		engine:           engine,
		lister:           informer.Lister(),
		nodeLister:       nodeInformer.Lister(),
		accessSynced:     informer.Informer().HasSynced,
		nodeSynced:       nodeInformer.Informer().HasSynced,
		nodeName:         types.NodeName(strings.ToLower(hostname)),
		eventBroadcaster: eventBroadcaster,
		eventRecorder:    eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: controllerAgentName}),
		queue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerAgentName),
	}

	logger.Info("Setting up event handlers")
	_, err = informer.Informer().AddEventHandlerWithResyncPeriod(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controller.enqueue(logger, obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			controller.enqueue(logger, newObj)
		},
		DeleteFunc: func(obj interface{}) {
			controller.enqueue(logger, obj)
		},
	}, time.Second*30)
	if err != nil {
		logger.Error(err, "Failed to setting up event handlers")
		return nil, err
	}

	return controller, nil
}

// Run worker and sync the queue obj to self logic
func (c *Controller) Run(ctx context.Context) {
	defer utilruntime.HandleCrash()

	// Start events processing pipeline.
	c.eventBroadcaster.StartStructuredLogging(0)
	c.eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: c.kubeClient.CoreV1().Events(metav1.NamespaceAll)})
	defer c.eventBroadcaster.Shutdown()

	defer c.queue.ShutDown()

	logger := klog.FromContext(ctx)
	// Start the informer factories to begin populating the informer caches
	logger.Info("Starting controller", "controller", controllerAgentName)
	defer logger.Info("Shutting down controller", "controller", controllerAgentName)

	// Wait for the caches to be synced before starting worker
	logger.Info("Waiting for informer caches to sync")
	if !cache.WaitForCacheSync(ctx.Done(), c.accessSynced, c.nodeSynced) {
		logger.Error(fmt.Errorf("failed to sync informer"), "Informer caches to sync bad")
		return
	}

	logger.Info("Starting worker")
	go wait.UntilWithContext(ctx, c.runWorker, time.Second)

	<-ctx.Done()
}

// runWorker wait obj by queue
func (c *Controller) runWorker(ctx context.Context) {
	for c.processNextWorkItem(ctx) {
	}
}

// if resource change, run this func to count resources
func (c *Controller) processNextWorkItem(ctx context.Context) bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncHandler(ctx, key.(string))
	c.handleErr(ctx, err, key)

	return true
}

func (c *Controller) handleErr(ctx context.Context, err error, key interface{}) {
	logger := klog.FromContext(ctx)
	if err == nil || apierrors.HasStatusCause(err, v1.NamespaceTerminatingCause) {
		c.queue.Forget(key)
		return
	}
	ns, name, keyErr := cache.SplitMetaNamespaceKey(key.(string))
	if keyErr != nil {
		logger.Error(err, "Failed to split meta namespace cache key", "cacheKey", key)
	}

	if c.queue.NumRequeues(key) < maxRetries {
		logger.V(2).Info("Error syncing access", "access", klog.KRef(ns, name), "err", err)
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	logger.V(2).Info("Dropping access out of the queue", "deployment", klog.KRef(ns, name), "err", err)
	c.queue.Forget(key)
}

// syncHandler sync the access object
func (c *Controller) syncHandler(ctx context.Context, key string) error {
	logger := klog.FromContext(ctx)

	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		logger.Error(err, "Failed to split meta namespace cache key", "cacheKey", key)
		return err
	}

	startTime := time.Now()
	logger.V(4).Info("Started syncing access", "access", name, "startTime", startTime)
	defer func() {
		logger.V(4).Info("Finished syncing access", "access", name, "duration", time.Since(startTime))
	}()

	access, err := c.lister.Get(name)
	if apierrors.IsNotFound(err) {
		logger.Info("Access not found", "access", name)
		return nil
	} else if err != nil {
		logger.Error(err, "Failed to get access", "access", name)
		return err
	}

	node, err := c.nodeLister.Get(string(c.nodeName))
	if err != nil {
		logger.Error(err, "Failed to get node by node lister")
		return err
	}

	if access.Spec.NodeSelector != nil {
		if !labels.SelectorFromSet(access.Spec.NodeSelector).Matches(labels.Set(node.Labels)) {
			logger.V(4).Info("Access nodeSelector not match node", "access", name, "selector", access.Spec.NodeSelector)
			return nil
		}
	}

	if !access.DeletionTimestamp.IsZero() {
		for _, ip := range access.Spec.IPs {
			long, err := linux.IPString2Long(ip)
			if err != nil {
				logger.Error(err, "Failed to convert ip addr", "access", name, "ip", ip)
				return err
			}
			if err := c.engine.BpfObjs.XdpStatsMap.LookupAndDelete(unsafe.Pointer(&long), &defaultValue); err != nil {
				logger.Error(err, "Failed to delete blacklist ip", "access", name, "ip", ip)
				return err
			}
		}
	}

	if len(access.Spec.IPs) == 0 {
		logger.Error(err, "Access IPs is nil", "access", name)
		return nil
	}

	// write rule to ebpf map
	for _, ip := range access.Spec.IPs {
		long, err := linux.IPString2Long(ip)
		if err != nil {
			logger.Error(err, "Failed to convert ip addr", "access", name, "ip", ip)
			return err
		}
		if err := c.engine.BpfObjs.XdpStatsMap.Update(unsafe.Pointer(&long), &defaultValue, ebpf.UpdateAny); err != nil {
			logger.Error(err, "Failed to update ebpf map", "access", name, "ip", ip)
			return err
		}
	}

	// list ips in node
	ips, err := ebpfmap.ListMapKey(c.engine.BpfObjs.XdpStatsMap)
	if err != nil {
		logger.Error(err, "Failed to list ebpf map", "access", name)
		return err
	}

	newStatus := accessv1alpha1.AccessStatus{
		NodeStatus: make(map[string][]string),
	}
	for k, v := range access.Status.NodeStatus {
		newStatus.NodeStatus[k] = v
	}
	newStatus.NodeStatus[string(c.nodeName)] = ips

	logger.Info("Get access status", "access", name, "status", newStatus)

	return c.updateAccessStatusInNeed(ctx, access, newStatus)
}

// updateAccessStatusInNeed update status if you need
func (c *Controller) updateAccessStatusInNeed(ctx context.Context, access *accessv1alpha1.Access, status accessv1alpha1.AccessStatus) error {
	logger := klog.FromContext(ctx)
	if !equality.Semantic.DeepEqual(access.Status, status) {
		access.Status = status
		return retry.RetryOnConflict(retry.DefaultRetry, func() error {
			_, updateErr := c.client.SampleV1alpha1().Accesses().UpdateStatus(ctx, access, metav1.UpdateOptions{})
			if updateErr == nil {
				return nil
			}
			got, err := c.client.SampleV1alpha1().Accesses().Get(ctx, access.Name, metav1.GetOptions{})
			if err == nil {
				access = got.DeepCopy()
				access.Status = status
			} else {
				logger.Error(err, "Failed to get access", "access", access.Name)
			}
			return fmt.Errorf("failed to update access %s status: %w", access.Name, updateErr)
		})
	}
	return nil
}

// cannot find resource kind from obj,so we need case all gvr
func (c *Controller) enqueue(logger klog.Logger, obj interface{}) {
	access := obj.(*accessv1alpha1.Access)
	if len(access.Spec.NodeName) != 0 && access.Spec.NodeName != string(c.nodeName) {
		logger.V(4).Info("Access not match nodeName", "access", access.Name, "node", c.nodeName)
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", access, err))
		return
	}

	c.queue.Add(key)
}
