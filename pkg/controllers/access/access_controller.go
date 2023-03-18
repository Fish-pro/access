package access

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierror "k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/access-io/access/pkg/apis/access/v1alpha1"
	accessclient "github.com/access-io/access/pkg/generated/clientset/versioned"
	"github.com/access-io/access/pkg/generated/clientset/versioned/scheme"
	accessinformers "github.com/access-io/access/pkg/generated/informers/externalversions/access/v1alpha1"
	accesslister "github.com/access-io/access/pkg/generated/listers/access/v1alpha1"
)

const (
	maxRetries     = 15
	ControllerName = "access-controller"
)

// NewAccessController returns a new *Controller.
func NewAccessController(
	kubeClient kubernetes.Interface,
	client accessclient.Interface,
	accessInformer accessinformers.AccessInformer) (*Controller, error) {
	broadcaster := record.NewBroadcaster()
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: ControllerName})

	r := &Controller{
		kubeClient:       kubeClient,
		client:           client,
		accessLister:     accessInformer.Lister(),
		accessSynced:     accessInformer.Informer().HasSynced,
		queue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "access"),
		workerLoopPeriod: time.Second,
		eventBroadcaster: broadcaster,
		eventRecorder:    recorder,
	}

	accessInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    r.addAccess,
		UpdateFunc: r.updateAccess,
		DeleteFunc: r.deleteAccess,
	})

	return r, nil
}

type Controller struct {
	kubeClient       kubernetes.Interface
	client           accessclient.Interface
	eventBroadcaster record.EventBroadcaster
	eventRecorder    record.EventRecorder

	accessLister accesslister.AccessLister
	accessSynced cache.InformerSynced

	queue workqueue.RateLimitingInterface

	workerLoopPeriod time.Duration
}

// Run will not return until stopCh is closed. workers determines how many
// access will be handled in parallel.
func (r *Controller) Run(ctx context.Context, workers int) {
	defer utilruntime.HandleCrash()

	// Start events processing pipelinr.
	r.eventBroadcaster.StartStructuredLogging(0)
	r.eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: r.kubeClient.CoreV1().Events("")})
	defer r.eventBroadcaster.Shutdown()

	defer r.queue.ShutDown()

	klog.Infof("Starting access controller")
	defer klog.Infof("Shutting down access controller")

	if !cache.WaitForNamedCacheSync("access", ctx.Done(), r.accessSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, r.worker, r.workerLoopPeriod)
	}
	<-ctx.Done()
}

// worker runs a worker thread that just dequeues items, processes them, and
// marks them done. You may run as many of these in parallel as you wish; the
// workqueue guarantees that they will not end up processing the same service
// at the same time.
func (r *Controller) worker(ctx context.Context) {
	for r.processNextWorkItem(ctx) {
	}
}

func (r *Controller) processNextWorkItem(ctx context.Context) bool {
	key, quit := r.queue.Get()
	if quit {
		return false
	}
	defer r.queue.Done(key)

	err := r.syncAccess(ctx, key.(string))
	r.handleErr(err, key)

	return true
}

func (r *Controller) addAccess(obj interface{}) {
	a := obj.(*v1alpha1.Access)
	klog.V(4).InfoS("Adding access", "access", klog.KObj(a))
	r.enqueue(a)
}

func (r *Controller) updateAccess(old, cur interface{}) {
	oldAccess := old.(*v1alpha1.Access)
	curAccess := cur.(*v1alpha1.Access)
	klog.V(4).InfoS("Updating access", "access", klog.KObj(oldAccess))
	r.enqueue(curAccess)
}

func (r *Controller) deleteAccess(obj interface{}) {
	a, ok := obj.(*v1alpha1.Access)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		a, ok = tombstone.Obj.(*v1alpha1.Access)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Access %#v", obj))
			return
		}
	}
	klog.V(4).InfoS("Deleting access", "access", klog.KObj(a))
	r.enqueue(a)
}

func (r *Controller) enqueue(a *v1alpha1.Access) {
	key, err := cache.MetaNamespaceKeyFunc(a)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	r.queue.Add(key)
}

func (r *Controller) handleErr(err error, key interface{}) {
	if err == nil || apierror.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
		r.queue.Forget(key)
		return
	}

	ns, name, keyErr := cache.SplitMetaNamespaceKey(key.(string))
	if keyErr != nil {
		klog.ErrorS(err, "Failed to split meta namespace cache key", "cacheKey", key)
	}

	if r.queue.NumRequeues(key) < maxRetries {
		klog.V(2).InfoS("Error syncing access, retrying", "access", klog.KRef(ns, name), "err", err)
		r.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	klog.V(2).InfoS("Dropping access out of the queue", "access", klog.KRef(ns, name), "err", err)
	r.queue.Forget(key)
}

func (r *Controller) syncAccess(ctx context.Context, key string) error {
	return nil
}
