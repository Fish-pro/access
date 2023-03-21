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
	"reflect"
	"strings"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/access-io/access/bpf/blips"
	accessv1alpha1 "github.com/access-io/access/pkg/apis/access/v1alpha1"
	accessversioned "github.com/access-io/access/pkg/generated/clientset/versioned"
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

// Controller define the option of controller
type Controller struct {
	client accessversioned.Interface

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

	// recorder can record the event
	recorder record.EventRecorder
}

// NewController return a controller and add event handler
func NewController(
	client accessversioned.Interface,
	informer accessinformers.AccessInformer,
	nodeInformer coreinformers.NodeInformer,
	recorder record.EventRecorder,
	engine *blips.EbpfEngine) (*Controller, error) {
	klog.V(4).Info("Creating event broadcaster")

	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	controller := &Controller{
		client:       client,
		lister:       informer.Lister(),
		nodeLister:   nodeInformer.Lister(),
		recorder:     recorder,
		engine:       engine,
		accessSynced: informer.Informer().HasSynced,
		nodeSynced:   nodeInformer.Informer().HasSynced,
		nodeName:     types.NodeName(strings.ToLower(hostname)),
		queue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerAgentName),
	}

	klog.Info("Setting up event handlers")
	_, err = informer.Informer().AddEventHandlerWithResyncPeriod(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controller.enqueue(obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			controller.enqueue(newObj)
		},
		DeleteFunc: func(obj interface{}) {
			controller.enqueue(obj)
		},
	}, time.Second*30)
	if err != nil {
		return nil, err
	}

	return controller, nil
}

// Run worker and sync the queue obj to self logic
func (c *Controller) Run(ctx context.Context) error {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	klog.Info("Starting resources counter controller")

	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(ctx.Done(), c.accessSynced, c.nodeSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("Starting workers")
	// Launch two workers to process ServiceAccount resources
	go wait.UntilWithContext(ctx, c.runWorker, time.Second)

	klog.Info("Started workers")
	<-ctx.Done()
	klog.Info("Shutting down workers")

	return nil
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
	c.handleErr(err, key)

	return true
}

func (c *Controller) handleErr(err error, key interface{}) {
	if err == nil || apierrors.HasStatusCause(err, v1.NamespaceTerminatingCause) {
		c.queue.Forget(key)
		return
	}

	ns, name, keyErr := cache.SplitMetaNamespaceKey(key.(string))
	if keyErr != nil {
		klog.ErrorS(err, "Failed to split meta namespace cache key", "cacheKey", key)
	}

	if c.queue.NumRequeues(key) < maxRetries {
		klog.V(2).InfoS("Error syncing deployment", "access", klog.KRef(ns, name), "err", err)
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	klog.V(2).InfoS("Dropping access out of the queue", "access", klog.KRef(ns, name), "err", err)
	c.queue.Forget(key)
}

// syncHandler sync the access object
func (c *Controller) syncHandler(ctx context.Context, key string) error {
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.ErrorS(err, "Failed to split meta namespace cache key", "cacheKey", key)
		return err
	}

	startTime := time.Now()
	klog.V(4).InfoS("Started syncing access", "access", klog.KRef("", name), "startTime", startTime)
	defer func() {
		klog.V(4).InfoS("Finished syncing access", "deployment", klog.KRef("", name), "duration", time.Since(startTime))
	}()

	access, err := c.lister.Get(name)
	if apierrors.IsNotFound(err) {
		klog.V(2).InfoS("Access has been deleted", "access", klog.KRef("", name))
		return nil
	}
	if err != nil {
		return err
	}

	a := access.DeepCopy()

	if !a.DeletionTimestamp.IsZero() {
		for _, ip := range a.Spec.IPs {
			var value string
			if err := c.engine.BpfObjs.Blacklist.LookupAndDelete(ip, &value); err != nil {
				klog.Errorf("Failed to delete blacklist ip %s: %w", ip, err)
				return err
			}
		}
		if err := c.removeFinalizer(ctx, a); err != nil {
			klog.Errorf("Failed to remove finalizer: %w", err)
			return err
		}
	} else {
		if err := c.setFinalizer(ctx, a); err != nil {
			klog.Errorf("Failed to set finalizer: %w", err)
			return err
		}
	}

	if len(a.Spec.IPs) == 0 {
		return nil
	}

	// write rule to ebpf map
	for _, ip := range a.Spec.IPs {
		long, err := linux.IPString2Long(ip)
		if err != nil {
			klog.Errorf("Failed to convert ip addr %s: %w", ip, err)
			return err
		}
		val := uint32(1)
		if err := c.engine.BpfObjs.Blacklist.Update(unsafe.Pointer(&long), &val, ebpf.UpdateAny); err != nil {
			klog.Errorf("Failed to update ebpf map ip %s: %w", ip, err)
			return err
		}
	}

	// list ips in node
	ips, err := ebpfmap.ListMapKey(c.engine.BpfObjs.Blacklist)
	if err != nil {
		klog.Errorf("Failed to list ebpf map: %w", err)
		return err
	}

	newStatus := accessv1alpha1.AccessStatus{
		NodeStatus: map[string][]string{
			string(c.nodeName): ips,
		},
	}
	for k, v := range a.Status.NodeStatus {
		newStatus.NodeStatus[k] = v
	}

	return c.updateAccessStatusInNeed(ctx, access, newStatus)
}

// updateAccessStatusInNeed update status if you need
func (c *Controller) updateAccessStatusInNeed(ctx context.Context, access *accessv1alpha1.Access, status accessv1alpha1.AccessStatus) error {
	if !reflect.DeepEqual(access.Status, status) {
		access.Status = status
		return retry.RetryOnConflict(retry.DefaultRetry, func() error {
			_, updateErr := c.client.SampleV1alpha1().Accesses().UpdateStatus(ctx, access, metav1.UpdateOptions{})
			if updateErr == nil {
				return nil
			}
			got, err := c.client.SampleV1alpha1().Accesses().Get(ctx, access.Name, metav1.GetOptions{})
			if err == nil {
				access := got.DeepCopy()
				access.Status = status
			} else {
				klog.Errorf("Failed to create/update access %s: %w", access.Name, err)
			}
			return updateErr
		})
	}
	return nil
}

// setFinalizer set finalizer from the given access
func (c *Controller) setFinalizer(ctx context.Context, access *accessv1alpha1.Access) error {
	if sets.NewString(access.Finalizers...).Has(controllerAgentName) {
		return nil
	}

	access.Finalizers = append(access.Finalizers, controllerAgentName)
	_, err := c.client.SampleV1alpha1().Accesses().Update(ctx, access, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

// removeFinalizer remove finalizer from the given access
func (c *Controller) removeFinalizer(ctx context.Context, access *accessv1alpha1.Access) error {
	if len(access.Finalizers) == 0 {
		return nil
	}

	access.Finalizers = []string{}
	_, err := c.client.SampleV1alpha1().Accesses().Update(ctx, access, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

// cannot find resource kind from obj,so we need case all gvr
func (c *Controller) enqueue(obj interface{}) {
	access := obj.(*accessv1alpha1.Access)
	node, err := c.nodeLister.Get(string(c.nodeName))
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	if len(access.Spec.NodeSelector) != 0 {
		if !labels.SelectorFromSet(access.Spec.NodeSelector).Matches(labels.Set(node.Labels)) {
			return
		}
	}

	var key string
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.queue.Add(key)
}
