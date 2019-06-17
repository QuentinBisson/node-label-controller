/*
Copyright 2019 QBisson.

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

package controller

import (
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	coreinformers "k8s.io/client-go/informers/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	
	"k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

const controllerAgentName = "node-label-controller"

const usesContainerLinuxLabel = "kubermatic.io/uses-container-linux"

const (
	// SuccessSynced is used as part of the Event 'reason' when a Node is synced
	SuccessSynced = "Synced"

	// MessageResourceSynced is the message used for an Event fired when a Node
	// is synced successfully
	MessageResourceSynced = "Node %s synced successfully"
)

// Controller is the controller implementation for Node resources
type Controller struct {
	// clientset is a standard kubernetes clientset
	clientset kubernetes.Interface

	nodesLister corelisters.NodeLister
	nodesSynced cache.InformerSynced

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder
}

func newRecorder(clientset kubernetes.Interface) record.EventRecorder {
	// Create event broadcaster
	klog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: clientset.CoreV1().Events("")})
	return eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})
}

// NewController returns a new node label controller
func NewController(
	clientset kubernetes.Interface,
	nodeInformer coreinformers.NodeInformer) *Controller {
	
	controller := &Controller{
		clientset:     clientset,
		nodesLister:   nodeInformer.Lister(),
		nodesSynced:   nodeInformer.Informer().HasSynced,
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Nodes"),
		recorder:      newRecorder(clientset),
	}

	klog.Info("Setting up event handlers")
		
	// Set up an event handler for when Node resources change. 
	// This handler will lookup the operating system of the given Node
	nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.handleNode,
		UpdateFunc: func(old, new interface{}) {
			controller.handleNode(new)
		},
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	klog.Info("Starting node label controller")

	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.nodesSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("Starting worker")
	// Launch one process to handle Node resources
	go wait.Until(c.runWorker, time.Second, stopCh)

	klog.Info("Started worker")
	<-stopCh
	klog.Info("Shutting down worker")

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
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Node resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
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

// Sync handlers adds the missing label to every Container Linux nodes.
func (c *Controller) syncHandler(key string) error {
	// Get the Node resource by its name
	node, err := c.nodesLister.Get(key)
	if err != nil {
		// The Node resource may no longer exist, in which case we stop processing.
		if errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("node '%s' in work queue no longer exists", key))
			return nil
		}

		return err
	}

	// We update the label block of the Node resource
	err = c.setNodeLabel(node, usesContainerLinuxLabel, "true")
	if err != nil {
		return err
	}

	c.recorder.Event(node, corev1.EventTypeNormal, SuccessSynced, fmt.Sprintf(MessageResourceSynced, node.Name))
	return nil
}

func (c *Controller) setNodeLabel(node *corev1.Node, labelKey string, labelValue string) error {
	// We DeepCopy the node to avoid cache sync issues
	node = node.DeepCopy()

	node.Labels[labelKey] = labelValue

	// Update the node labels
	_, err := c.clientset.CoreV1().Nodes().Update(node)

	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Node could not be patched with label : %s", err))
	}

	return nil
}

// enqueueNode takes a Node resource and get the node name 
// which is then put onto the work queue. This method should *not* be
// passed resources of any type other than Node.
func (c *Controller) enqueueNode(obj interface{}) {
	c.workqueue.Add(obj.(*corev1.Node).Name)
}

// handleNode will receive nodes. 
// If the node uses Container Linux, the node will be enqueued to be processed.
// Otherwise, it will simply be skipped.
func (c *Controller) handleNode(obj interface{}) {
	var node *corev1.Node
	var ok bool
	if node, ok = obj.(*corev1.Node); !ok {
		utilruntime.HandleError(fmt.Errorf("error decoding node, invalid type"))
		return
	}
	klog.V(4).Infof("Processing node: %s", node.GetName())

	// If the label is already set, no need to set if again
	if _, nodeLabelAlreadySet := node.Labels[usesContainerLinuxLabel]; nodeLabelAlreadySet {
		return
	}

	if strings.HasPrefix(node.Status.NodeInfo.OSImage, "Container Linux") {
		c.enqueueNode(node)
		return
	}
}