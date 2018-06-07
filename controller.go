package main

import (
	"encoding/json"
	e "errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/ashleyschuett/gce-ingress-backend-services/pkg/annotations"
	"github.com/ashleyschuett/gce-ingress-backend-services/pkg/gce"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"

	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	corelistersv1 "k8s.io/client-go/listers/core/v1"
	extensionslistersv1 "k8s.io/client-go/listers/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	kGCE "k8s.io/kubernetes/pkg/cloudprovider/providers/gce"

	googleV1 "google.golang.org/api/compute/v1"
)

const controllerAgentName = "gce-backend-ingress-controller"

// Controller is the controller implementation for Database resources
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface

	ingressLister extensionslistersv1.IngressLister
	ingressSynced cache.InformerSynced

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder

	// this is a the lister that will be used for finding out the type of
	// gce backend that is backing the kubernetes ingress
	serviceLister corelistersv1.ServiceLister

	// refernce to gce auth to use for getting resouces from the google cloud api
	cloud *kGCE.GCECloud
}

// NewController returns a new controller
func NewController(
	kubeclientset kubernetes.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory) *Controller {

	// obtain references to shared index informers for the Ingress and Service
	// types.
	ingressInformer := kubeInformerFactory.Extensions().V1beta1().Ingresses()
	serviceLister := kubeInformerFactory.Core().V1().Services()

	// Create event broadcaster
	glog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		kubeclientset: kubeclientset,
		ingressLister: ingressInformer.Lister(),
		ingressSynced: ingressInformer.Informer().HasSynced,
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), ""),
		recorder:      recorder,
		serviceLister: serviceLister.Lister(),
		// Create auth with gce cloud provider module
		cloud: gce.New(),
	}

	glog.Info("Setting up event handlers")

	// we will watch ingress resources for any update event, if there is a change
	// between the old resource and the new one we check the resources state
	// with google cloud to make sure the ingresses desired state is reflected
	// on the backend service in the cloud
	ingressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(old, cur interface{}) {
			curIng := cur.(*extensions.Ingress)
			if !gce.IsGCEIngress(curIng) && !gce.IsGCEMultiClusterIngress(curIng) {
				return
			}

			if reflect.DeepEqual(old, cur) {
				glog.V(5).Infof("Ingress sync event %v", curIng.Name)
				return
			}

			glog.V(3).Infof("Ingress %v changed, syncing", curIng.Name)
			controller.enqueueIngress(cur)
		},
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	glog.Info("Starting GCE Ingress Update controller")

	// Wait for the caches to be synced before starting workers
	glog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.ingressSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	glog.Info("Starting workers")
	// Launch two workers to process Database resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	glog.Info("Started workers")
	<-stopCh
	glog.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Database resource to be synced.
		if err := c.syncHandler(key); err != nil {
			fmt.Println(err)
			return fmt.Errorf("error syncing '%s': %s", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		glog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It does this by finding if there are any settings
// for each ingress backend specified and then updates the associated gce backend
func (c *Controller) syncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Get the Ingress resource with this namespace/name
	ingress, err := c.ingressLister.Ingresses(namespace).Get(name)
	if err != nil {
		// The Ingress resource no longer exists
		if errors.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("ingress '%s' in work queue no longer exists", key))
			return nil
		}

		return err
	}

	// backend will be in the form of `{ backendId : "status" }`
	var backendsAnnotation map[string]string
	err = json.Unmarshal([]byte(ingress.Annotations[annotations.Backends]), &backendsAnnotation)
	// There are no backends assoicated with this inggress
	if err != nil {
		return nil
	}

	urlmaplist, err := c.cloud.GetUrlMap(ingress.Annotations[annotations.URLMap])
	if err != nil {
		return nil
	}

	settings := ingress.Annotations[annotations.ServiceSettings]

	return c.updateBackends(ingress, urlmaplist, settings)
}

func getPathMatcherID(urlmaps *googleV1.UrlMap, host string) string {
	for _, rule := range urlmaps.HostRules {
		for _, h := range rule.Hosts {
			if h == host {
				return rule.PathMatcher
			}
		}
	}

	return ""
}

func getPathMatcher(urlmaps *googleV1.UrlMap, name string) *googleV1.PathMatcher {
	for _, matcher := range urlmaps.PathMatchers {
		if matcher.Name == name {
			return matcher
		}
	}

	return &googleV1.PathMatcher{}
}

func parseBackendID(url string) string {
	sl := strings.Split(url, "/")
	backendID := sl[len(sl)-1]
	return backendID
}

// getBackendID gets the id of the gce backend id for a ingress backend
// by matching its hosta and path to the url map, and parsting the
// service url
func getBackendID(urlmaps *googleV1.UrlMap, host, path string) string {
	if host == "*" && path == "" {
		return getDefaultBackendID(urlmaps)
	}

	pathMatcherID := getPathMatcherID(urlmaps, host)
	pathMatcher := getPathMatcher(urlmaps, pathMatcherID)
	for _, rules := range pathMatcher.PathRules {

		backendID := parseBackendID(rules.Service)
		for _, p := range rules.Paths {
			if p == path {
				return backendID
			}
		}
	}

	return ""
}

// getBackendSettings parses the settings specifed under the service-settings
// annotation. These are keyed by host/path in the annotation
func getBackendSettings(backendName string, settings string) (map[string]interface{}, error) {
	var settingsMap map[string]interface{}
	err := json.Unmarshal([]byte(settings), &settingsMap)
	if err != nil {
		return nil, err
	}

	if _, ok := settingsMap[backendName]; !ok {
		return settingsMap, nil
	}

	bs, ok := settingsMap[backendName].(map[string]interface{})
	if !ok {
		return nil, e.New("Backend settings map is of inncorrect type, needs to be in the form of map[string]interface{}")
	}

	return bs, nil
}

func getDefaultBackendID(urlmap *googleV1.UrlMap) string {
	return parseBackendID(urlmap.DefaultService)
}

func (c *Controller) updateBackend(ingress *extensions.Ingress, urlmap *googleV1.UrlMap, settings, host string, http extensions.HTTPIngressPath) error {
	path := http.Path
	backendID := getBackendID(urlmap, host, path)
	name := host + path

	backendSettings, err := getBackendSettings(name, settings)
	if err != nil {
		return err
	}

	// If there are no settings for the backend we should move on to checking the next one
	if backendSettings == nil {
		return nil
	}

	glog.V(9).Infof("Updating Backend: %s", backendID)
	err = gce.UpdateBackendService(c.cloud, c.serviceLister, http.Backend, backendID, ingress.Namespace, backendSettings)
	if err != nil {
		return err
	}

	glog.Infof("Successfully updated backend: %s", backendID)
	return nil
}

// updateBackends checks for the default backend on the ingress and updates
// the corresponding default backend in gce, then looks for settings associated
// with each ingress backend under rules, and updates the gcp backend
func (c *Controller) updateBackends(ingress *extensions.Ingress, urlmap *googleV1.UrlMap, settings string) error {
	glog.V(8).Info("Attempting to update ingresses backend services in google cloud... ")

	hasErrors := false
	if ingress.Spec.Backend != nil {
		err := c.updateBackend(ingress, urlmap, settings, "*", extensions.HTTPIngressPath{"", *ingress.Spec.Backend})
		if err != nil {
			hasErrors = true
			glog.Error(err)
		}
	}

	// check each backend to see if there are any specific settings that
	// need to be set
	if ingress.Spec.Rules == nil {
		return nil
	}

	for _, rule := range ingress.Spec.Rules {
		host := rule.Host

		for _, http := range rule.HTTP.Paths {
			err := c.updateBackend(ingress, urlmap, settings, host, http)
			if err != nil {
				hasErrors = true
				glog.Error(err)
			}
		}
	}

	if hasErrors {
		return e.New("There where errors when syncing settings")
	}

	return nil
}

// enqueueIngress takes a Ingress resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than Ingress.
func (c *Controller) enqueueIngress(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.workqueue.AddRateLimited(key)
}
