package access

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cilium/ebpf"
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
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/access-io/access/bpf/blips"
	accessv1alpha1 "github.com/access-io/access/pkg/apis/access/v1alpha1"
	"github.com/access-io/access/pkg/generated/clientset/versioned"
	accessinformers "github.com/access-io/access/pkg/generated/informers/externalversions/access/v1alpha1"
	accesslisters "github.com/access-io/access/pkg/generated/listers/access/v1alpha1"
)

const (
	controllerAgentName = "access-agent"
)

// Controller define the option of controller
type Controller struct {
	ctx    context.Context
	client versioned.Interface

	lister     accesslisters.AccessLister
	nodeLister corelisters.NodeLister

	queue        workqueue.RateLimitingInterface
	accessSynced cache.InformerSynced
	nodeSynced   cache.InformerSynced

	engine *blips.EbpfEngine

	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder
}

// NewController return a controller and add event handler
func NewController(
	ctx context.Context,
	client versioned.Interface,
	informer accessinformers.AccessInformer,
	nodeInformer coreinformers.NodeInformer,
	recorder record.EventRecorder,
	engine *blips.EbpfEngine) (*Controller, error) {

	klog.V(4).Info("Creating event broadcaster")

	controller := &Controller{
		ctx:          ctx,
		client:       client,
		lister:       informer.Lister(),
		nodeLister:   nodeInformer.Lister(),
		recorder:     recorder,
		engine:       engine,
		accessSynced: informer.Informer().HasSynced,
		nodeSynced:   nodeInformer.Informer().HasSynced,
		queue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerAgentName),
	}

	klog.Info("Setting up event handlers")
	_, err := informer.Informer().AddEventHandlerWithResyncPeriod(cache.ResourceEventHandlerFuncs{
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
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	klog.Info("Starting resources counter controller")

	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.accessSynced, c.nodeSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("Starting workers")
	// Launch two workers to process ServiceAccount resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")

	return nil
}

// runWorker wait obj by workerqueue
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// if resource change, run this func to count resources
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.queue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.queue.Done(obj)

		key, ok := obj.(string)
		if !ok {
			c.queue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.syncHandler(key); err != nil {
			c.queue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.queue.Forget(obj)
		klog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// syncHandler
func (c *Controller) syncHandler(key string) error {
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	access, err := c.lister.Get(name)
	if err != nil {
		return err
	}

	if !access.DeletionTimestamp.IsZero() {
		for _, ip := range access.Spec.IPs {
			var value string
			if err := c.engine.BpfObjs.Blacklist.LookupAndDelete(ip, &value); err != nil {
				return err
			}
		}
		if err := c.removeFinalizer(access); err != nil {
			return err
		}
	} else {
		if err := c.setFinalizer(access); err != nil {
			return err
		}
	}

	if len(access.Spec.IPs) == 0 {
		return nil
	}

	for _, ip := range access.Spec.IPs {
		if err := c.engine.BpfObjs.Blacklist.Update(ip, "", ebpf.UpdateAny); err != nil {
			return err
		}
	}
	return nil
}

// setFinalizer set finalizer from the given access
func (c *Controller) setFinalizer(access *accessv1alpha1.Access) error {
	if sets.NewString(access.Finalizers...).Has(controllerAgentName) {
		return nil
	}

	access.Finalizers = append(access.Finalizers, controllerAgentName)
	_, err := c.client.SampleV1alpha1().Accesses().Update(c.ctx, access, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

// removeFinalizer remove finalizer from the given access
func (c *Controller) removeFinalizer(access *accessv1alpha1.Access) error {
	if len(access.Finalizers) == 0 {
		return nil
	}

	access.Finalizers = []string{}
	_, err := c.client.SampleV1alpha1().Accesses().Update(c.ctx, access, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

// cannot find resource kind from obj,so we need case all gvr
func (c *Controller) enqueue(obj interface{}) {
	access := obj.(*accessv1alpha1.Access)
	nodeName, err := getNodeName()
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	node, err := c.nodeLister.Get(string(nodeName))
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

func getNodeName() (types.NodeName, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}
	return types.NodeName(strings.ToLower(hostname)), nil
}
