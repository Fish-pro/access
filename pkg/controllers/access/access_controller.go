package resourcescounter

import (
	"fmt"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/access-io/access/pkg/generated/clientset/versioned"
	accessinformers "github.com/access-io/access/pkg/generated/informers/externalversions/access/v1alpha1"
	accesslisters "github.com/access-io/access/pkg/generated/listers/access/v1alpha1"
)

const (
	controllerAgentName = "access-agent"
)

// Controller define the option of controller
type Controller struct {
	client  kubernetes.Interface
	aClient versioned.Interface

	informer accessinformers.AccessInformer
	lister   accesslisters.AccessLister

	workqueue workqueue.RateLimitingInterface
	synced    cache.InformerSynced

	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder
}

// NewController return a controller and add event handler
func NewController(
	client kubernetes.Interface,
	aClient versioned.Interface,
	informer accessinformers.AccessInformer,
	recorder record.EventRecorder) *Controller {

	klog.V(4).Info("Creating event broadcaster")

	controller := &Controller{
		client:    client,
		aClient:   aClient,
		lister:    informer.Lister(),
		informer:  informer,
		recorder:  recorder,
		workqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerAgentName),
	}

	klog.Info("Setting up event handlers")
	informer.Informer().AddEventHandlerWithResyncPeriod(cache.ResourceEventHandlerFuncs{
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

	return controller
}

// Run worker and sync the queue obj to self logic
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	klog.Info("Starting resources counter controller")

	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.synced); !ok {
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
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)

		key, ok := obj.(string)
		if !ok {
			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.syncHandler(key); err != nil {
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.workqueue.Forget(obj)
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
	return nil
}

// cannot find resource kind from obj,so we need case all gvr
func (c *Controller) enqueue(obj interface{}) {
	key := ""
	c.workqueue.Add(key)
}
