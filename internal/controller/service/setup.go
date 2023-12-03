/*
Copyright 2023.

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
	"context"
	"fmt"
	"sync"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/snapp-incubator/svc-lb-to-cilium-netpolicy/internal/config"
	"github.com/snapp-incubator/svc-lb-to-cilium-netpolicy/internal/consts"
	"github.com/snapp-incubator/svc-lb-to-cilium-netpolicy/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func newCustomEventHandler(handler customEventHandlerFunc) handler.EventHandler {
	return &customEventHandler{
		handler: handler,
	}
}

func (h *customEventHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	reqs := h.handler(ctx, evt.Object, nil, createEvent)

	for _, req := range reqs {
		q.Add(req)
	}
}

func (h *customEventHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	reqs := h.handler(ctx, evt.ObjectNew, evt.ObjectOld, updateEvent)

	for _, req := range reqs {
		q.Add(req)
	}
}

func (h *customEventHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	reqs := h.handler(ctx, nil, evt.Object, deleteEvent)

	for _, req := range reqs {
		q.Add(req)
	}
}

func (h *customEventHandler) Generic(_ context.Context, _ event.GenericEvent, _ workqueue.RateLimitingInterface) {
	// no implementation yet
}

//nolint:varnamelen
func (re *ReconcilerExtended) namespacEventHandler(ctx context.Context, objNew client.Object, objOld client.Object, et eventType) []ctrl.Request {
	namespaceNew, ok := objNew.(*corev1.Namespace)
	if objNew != nil && !ok {
		return []ctrl.Request{}
	}

	namespaceOld, ok := objOld.(*corev1.Namespace)
	if objOld != nil && !ok {
		return []ctrl.Request{}
	}

	var namespace string

	switch et {
	case createEvent, updateEvent:
		namespace = namespaceNew.GetName()
	case deleteEvent:
		namespace = namespaceOld.GetName()
	}

	if namespace == "" {
		return []ctrl.Request{}
	}

	logger := log.FromContext(ctx).WithName("namespace event handler").WithValues("namespace", namespace, "event", et)

	re.cacheLock.Lock()
	defer re.cacheLock.Unlock()

	// Check if the Namespace is present in the exclusion cache.
	// If the entry for this Namespace is nil, return empty array.
	// This implies the processing will be handled in the reconcile loop.
	cacheEntry := re.namespaceExclusionCache[namespace]
	if cacheEntry == nil {
		logger.Info(consts.NamespaceExclusionCacheEntryNotSetInfo)

		return []ctrl.Request{}
	}

	// Determine the exclusion status of the specified Namespace from the cache.
	// This oldStatus is used for comparison purposes later in the code to detect any changes
	// in the exclusion status of the Namespace.
	oldStatus := determineExclusionStatus(cacheEntry)

	switch et {
	case updateEvent:
		var namespaceLabelsExcluded bool

		cfg := config.GetConfig()

		for _, matchLabel := range cfg.Controller.Exclude.NamespaceSelector.MatchLabels {
			if utils.IsMapSubset(namespaceNew.GetLabels(), matchLabel.Labels) {
				namespaceLabelsExcluded = true

				// Add exclusion reason for the namespace indicating the source of the exclusion
				cacheEntry[namespaceLabels] = empty{}

				logger.Info(fmt.Sprintf(consts.NamespaceExcludedLabelsMatchInfo, matchLabel.MatcherName) +
					consts.NamespaceExclusionReasonAddedInfo)

				break
			}
		}

		if !namespaceLabelsExcluded {
			// Remove exclusion reason if set
			delete(cacheEntry, namespaceLabels)

			logger.Info("The exclusion reason 'namespaceLabels' is removed from the namespace cache entry")
		}

	case createEvent:
		return []ctrl.Request{}

	case deleteEvent:
		return []ctrl.Request{}
	}

	// Determine the exclusion status of the specified Namespace from the cache.
	// This newStatus is used for comparison purposes to detect any changes
	// in the exclusion status of the Namespace.
	newStatus := determineExclusionStatus(cacheEntry)

	// Check if there has been a change in the exclusion status of the namespace.
	// In case of a status change, trigger the reconciliation process for Service objects of type LoadBalancer within the specified namespace.
	// This is achieved by returning these objects to be added to the queue.
	// The reconciliation ensures that the cluster state is aligned with the updated exclusion status.
	if newStatus != oldStatus {
		serviceList := &corev1.ServiceList{}

		if err := re.Client.List(ctx, serviceList, client.InNamespace(namespace), client.MatchingFields{"spec.type": "LoadBalancer"}); err != nil {
			logger.Error(err, "failed to list Service of type 'LoadBalancer' objects to queue for reconciling")

			return []ctrl.Request{}
		}

		if len(serviceList.Items) == 0 {
			return []ctrl.Request{}
		}

		reqs := make([]ctrl.Request, len(serviceList.Items))

		for i, service := range serviceList.Items {
			reqs[i] = ctrl.Request{NamespacedName: types.NamespacedName{Namespace: service.Namespace, Name: service.Name}}
		}

		return reqs
	}

	return []ctrl.Request{}
}

//nolint:varnamelen
func (re *ReconcilerExtended) networkPolicyEventHandler(ctx context.Context, objNew client.Object, objOld client.Object, et eventType) []ctrl.Request {
	networkPolicyNew, ok := objNew.(*networkingv1.NetworkPolicy)
	if objNew != nil && !ok {
		return []ctrl.Request{}
	}

	networkPolicyOld, ok := objOld.(*networkingv1.NetworkPolicy)
	if objOld != nil && !ok {
		return []ctrl.Request{}
	}

	var namespace string

	switch et {
	case createEvent, updateEvent:
		namespace = networkPolicyNew.GetNamespace()
	case deleteEvent:
		namespace = networkPolicyOld.GetNamespace()
	}

	if namespace == "" {
		return []ctrl.Request{}
	}

	logger := log.FromContext(ctx).WithName("networkpolicy event handler").WithValues("namespace", namespace, "event", et)

	re.cacheLock.Lock()
	defer re.cacheLock.Unlock()

	// Check if the Namespace is present in the exclusion cache.
	// If the entry for this Namespace is nil, return empty array.
	// This implies the processing will be handled in the reconcile loop.
	cacheEntry := re.namespaceExclusionCache[namespace]
	if cacheEntry == nil {
		logger.Info(consts.NamespaceExclusionCacheEntryNotSetInfo)

		return []ctrl.Request{}
	}

	// Determine the exclusion status of the specified Namespace from the cache.
	// This oldStatus is used for comparison purposes later in the code to detect any changes
	// in the exclusion status of the Namespace.
	oldStatus := determineExclusionStatus(cacheEntry)

	switch et {
	case createEvent:
		// Remove exclusion reason if set
		delete(cacheEntry, noNetworkPolicyExistence)

		logger.Info("The exclusion reason 'noNetworkPolicyExistence' is removed from the namespace cache entry")

	case deleteEvent:
		networkPolicyList := &networkingv1.NetworkPolicyList{}

		// Attempt to list NetworkPolicy objects within the specified namespace.
		// In case of an error, the error state is signaled by setting 'cacheEntry' to nil. This action effectively resets the state
		// of the cache for the namespace in question, ensuring that any stale or incorrect data is cleared.
		if err := re.Client.List(ctx, networkPolicyList, client.InNamespace(namespace)); err != nil {
			logger.Error(err, "failed to list NetworkPolicy objects")

			// The 'nolint:ineffassign,wastedassign' directive is used to bypass lint warnings about the ineffectiveness of the assignment,
			// acknowledging that this operation is intentional for error handling and state reset purposes.
			//nolint:ineffassign,wastedassign
			cacheEntry = nil

			return []ctrl.Request{}
		}

		if len(networkPolicyList.Items) > 0 {
			// Remove exclusion reason if set
			delete(cacheEntry, noNetworkPolicyExistence)

			logger.Info("The exclusion reason 'noNetworkPolicyExistence' is removed from the namespace cache entry")
		} else {
			// Add exclusion reason for the namespace indicating the source of the exclusion
			cacheEntry[noNetworkPolicyExistence] = empty{}
		}

	case updateEvent:
		return []ctrl.Request{}
	}

	// Determine the exclusion status of the specified Namespace from the cache.
	// This newStatus is used for comparison purposes to detect any changes
	// in the exclusion status of the Namespace.
	newStatus := determineExclusionStatus(cacheEntry)

	// Check if there has been a change in the exclusion status of the namespace.
	// In case of a status change, trigger the reconciliation process for Service objects of type LoadBalancer within the specified namespace.
	// This is achieved by returning these objects to be added to the queue.
	// The reconciliation ensures that the cluster state is aligned with the updated exclusion status.
	if newStatus != oldStatus {
		serviceList := &corev1.ServiceList{}

		if err := re.Client.List(ctx, serviceList, client.InNamespace(namespace), client.MatchingFields{"spec.type": "LoadBalancer"}); err != nil {
			logger.Error(err, "failed to list Service of type 'LoadBalancer' objects to queue for reconciling")

			return []ctrl.Request{}
		}

		if len(serviceList.Items) == 0 {
			return []ctrl.Request{}
		}

		reqs := make([]ctrl.Request, len(serviceList.Items))

		for i, service := range serviceList.Items {
			reqs[i] = ctrl.Request{NamespacedName: types.NamespacedName{Namespace: service.Namespace, Name: service.Name}}
		}

		return reqs
	}

	return []ctrl.Request{}
}

//nolint:varnamelen
func (re *ReconcilerExtended) ciliumNetworkPolicyEventHandler(ctx context.Context, objNew client.Object, objOld client.Object, et eventType) []ctrl.Request {
	ciliumNetworkPolicyNew, ok := objNew.(*ciliumv2.CiliumNetworkPolicy)
	if objNew != nil && !ok {
		return []ctrl.Request{}
	}

	ciliumNetworkPolicyOld, ok := objOld.(*ciliumv2.CiliumNetworkPolicy)
	if objOld != nil && !ok {
		return []ctrl.Request{}
	}

	var namespace string

	switch et {
	case createEvent, updateEvent:
		namespace = ciliumNetworkPolicyNew.GetNamespace()
	case deleteEvent:
		namespace = ciliumNetworkPolicyOld.GetNamespace()
	}

	if namespace == "" {
		return []ctrl.Request{}
	}

	logger := log.FromContext(ctx).WithName("ciliumnetworkpolicy event handler").WithValues("namespace", namespace, "event", et)

	reqs := make([]ctrl.Request, 0)

	defer func() {
		reqs = deduplicateRequests(reqs)
	}()

	// Check for potential owner object of the ciliumNetworkPolicy.
	if req := getOwnerReconcileRequest(ctx, ciliumNetworkPolicyNew); req != nil {
		reqs = append(reqs, *req)
	}

	// Similarly, check for owner object for the old state of the ciliumNetworkPolicy.
	// This ensures that any changes in ownership between the old and new states are captured.
	if req := getOwnerReconcileRequest(ctx, ciliumNetworkPolicyOld); req != nil {
		reqs = append(reqs, *req)
	}

	re.cacheLock.Lock()
	defer re.cacheLock.Unlock()

	// Check if the Namespace is present in the exclusion cache.
	// If the entry for this Namespace is nil, return empty array.
	// This implies the processing will be handled in the reconcile loop.
	cacheEntry := re.namespaceExclusionCache[namespace]
	if cacheEntry == nil {
		logger.Info(consts.NamespaceExclusionCacheEntryNotSetInfo)

		return reqs
	}

	// Determine the exclusion status of the specified Namespace from the cache.
	// This oldStatus is used for comparison purposes later in the code to detect any changes
	// in the exclusion status of the Namespace.
	oldStatus := determineExclusionStatus(cacheEntry)

	switch et {
	case createEvent:
		if utils.IsMapSubset(ciliumNetworkPolicyNew.GetLabels(), controllerLabelsMap) {
			return reqs
		}

		// Remove exclusion reason if set
		delete(cacheEntry, noUnmanagedCiliumNetworkPolicyExistence)

		logger.Info("The exclusion reason 'noCiliumNetworkPolicyExistence' is removed from the namespace cache entry")

	case updateEvent, deleteEvent:
		ciliumNetworkPolicyList := &ciliumv2.CiliumNetworkPolicyList{}

		// Attempt to construct a label selector to list CiliumNetworkPolicy objects not containing the controller labels
		// This is necessary as the rule should check the presence of unmanaged policies.
		// In case of an error, the error state is signaled by setting 'cacheEntry' to nil. This action effectively resets the state
		// of the cache for the namespace in question, ensuring that any stale or incorrect data is cleared.
		labelSelector := labels.NewSelector()

		for labelKey, labelValue := range controllerLabelsMap {
			vals := make([]string, 1)
			vals[0] = labelValue
			requirement, err := labels.NewRequirement(labelKey, selection.NotEquals, vals)

			if err != nil {
				// The 'nolint:ineffassign,wastedassign' directive is used to bypass lint warnings about the ineffectiveness of the assignment,
				// acknowledging that this operation is intentional for error handling and state reset purposes.
				//nolint:ineffassign,wastedassign
				cacheEntry = nil

				return reqs
			}

			labelSelector = labelSelector.Add(*requirement)
		}

		// Attempt to list CiliumNetworkPolicy objects within the specified namespace.
		// In case of an error, the error state is signaled by setting 'cacheEntry' to nil. This action effectively resets the state
		// of the cache for the namespace in question, ensuring that any stale or incorrect data is cleared.
		if err := re.Client.List(ctx, ciliumNetworkPolicyList, client.InNamespace(namespace), &client.ListOptions{LabelSelector: labelSelector}); err != nil {
			logger.Error(err, "failed to list CiliumNetworkPolicy objects")

			// The 'nolint:ineffassign,wastedassign' directive is used to bypass lint warnings about the ineffectiveness of the assignment,
			// acknowledging that this operation is intentional for error handling and state reset purposes.
			//nolint:ineffassign,wastedassign
			cacheEntry = nil

			return reqs
		}

		if len(ciliumNetworkPolicyList.Items) > 0 {
			// Remove exclusion reason if set
			delete(cacheEntry, noUnmanagedCiliumNetworkPolicyExistence)

			logger.Info("The exclusion reason 'noCiliumNetworkPolicyExistence' is removed from the namespace cache entry")
		} else {
			// Add exclusion reason for the namespace indicating the source of the exclusion
			cacheEntry[noUnmanagedCiliumNetworkPolicyExistence] = empty{}
		}
	}

	// Determine the exclusion status of the specified Namespace from the cache.
	// This newStatus is used for comparison purposes to detect any changes
	// in the exclusion status of the Namespace.
	newStatus := determineExclusionStatus(cacheEntry)

	// Check if there has been a change in the exclusion status of the namespace.
	// In case of a status change, trigger the reconciliation process for Service objects of type LoadBalancer within the specified namespace.
	// This is achieved by returning these objects to be added to the queue.
	// The reconciliation ensures that the cluster state is aligned with the updated exclusion status.
	if newStatus != oldStatus {
		serviceList := &corev1.ServiceList{}

		if err := re.Client.List(ctx, serviceList, client.InNamespace(namespace), client.MatchingFields{"spec.type": "LoadBalancer"}); err != nil {
			logger.Error(err, "failed to list Service of type 'LoadBalancer' objects to queue for reconciling")

			return reqs
		}

		if len(serviceList.Items) == 0 {
			return reqs
		}

		for _, service := range serviceList.Items {
			req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: service.Name}}
			reqs = append(reqs, req)
		}
	}

	return reqs
}

// NewReconcilerExtended instantiate a new ReconcilerExtended struct and returns it.
func NewReconcilerExtended(mgr manager.Manager) *ReconcilerExtended {
	return &ReconcilerExtended{
		cacheLock:               &sync.Mutex{},
		Client:                  mgr.GetClient(),
		namespaceExclusionCache: make(map[string]map[namespaceExclusionReason]empty),
		scheme:                  mgr.GetScheme(),
	}
}

// SetupWithManager sets up the controller with the manager.
func (re *ReconcilerExtended) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		// This watch EventHandler responds to create / delete / update events by *reconciling the owner object* ( equivalent of calling Owns(&ciliumv2.CiliumNetworkPolicy{}) )
		// in addition to handling cache update flow.
		Watches(&ciliumv2.CiliumNetworkPolicy{}, newCustomEventHandler(re.ciliumNetworkPolicyEventHandler)).
		Watches(&networkingv1.NetworkPolicy{}, newCustomEventHandler(re.networkPolicyEventHandler)).
		Watches(&corev1.Namespace{}, newCustomEventHandler(re.namespacEventHandler)).
		Complete(re)
}
