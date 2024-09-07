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
	"errors"
	"fmt"
	"maps"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	cilium_slim_metav1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/apis/meta/v1"
	cilium_labels "github.com/cilium/cilium/pkg/labels"
	cilium_labelsfilter "github.com/cilium/cilium/pkg/labelsfilter"
	cilium_policy_api "github.com/cilium/cilium/pkg/policy/api"
	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/snapp-incubator/svc-lb-to-cilium-netpolicy/internal/config"
	"github.com/snapp-incubator/svc-lb-to-cilium-netpolicy/internal/consts"
	"github.com/snapp-incubator/svc-lb-to-cilium-netpolicy/pkg/utils"
)

var (
	controllerAnnotationsMap = map[string]string{"snappcloud.io/team": "snappcloud", "snappcloud.io/sub-team": "network"}
	controllerLabelsMap      = map[string]string{"snappcloud.io/controller-managed": "true", "snappcloud.io/controller": "svc-lb-to-cilium-netpolicy"}
)

func init() {
	if err := cilium_labelsfilter.ParseLabelPrefixCfg([]string{""}, ""); err != nil {
		log.Log.Info("error preparing cilium label filters", "error", err)
	}
}

// buildCNP builds and returns a CiliumNetworkPolicyand error based on the Service object.
func (re *ReconcilerExtended) buildCNP() (cnp *ciliumv2.CiliumNetworkPolicy, err error) {
	fromEntities := cilium_policy_api.EntitySlice{
		cilium_policy_api.EntityCluster,
		cilium_policy_api.EntityWorld,
	}

	endpointSelector, err := getEndponitSelector(re.service.Spec.Selector)
	if err == nil {
		cnp = &ciliumv2.CiliumNetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:        re.service.Name,
				Namespace:   re.service.Namespace,
				Labels:      re.getCNPDesiredLabels(),
				Annotations: controllerAnnotationsMap,
			},
			Spec: &cilium_policy_api.Rule{
				EndpointSelector: endpointSelector,
				Ingress: []cilium_policy_api.IngressRule{
					{
						IngressCommonRule: cilium_policy_api.IngressCommonRule{
							FromEntities: fromEntities,
						},
					},
				},
			},
		}
	}

	return cnp, err
}

// getEndponitSelector is used to filter and extract cilium endpoint selector from service labels.
//
//nolint:wsl
func getEndponitSelector(serviceSlector map[string]string) (ciliumEndPointSelector cilium_policy_api.EndpointSelector, err error) {
	identityLabels, informationLabels := cilium_labelsfilter.Filter(cilium_labels.Map2Labels(serviceSlector, cilium_labels.LabelSourceK8s))
	if len(informationLabels.K8sStringMap()) != 0 {
		log.Log.Info("excluded_labels", "information labels", informationLabels.String())
	}

	identityLabelSelector := cilium_slim_metav1.LabelSelector{MatchLabels: identityLabels.K8sStringMap()}
	ciliumEndPointSelector = cilium_policy_api.NewESFromK8sLabelSelector(cilium_labels.LabelSourceK8sKeyPrefix, &identityLabelSelector)
	if len(ciliumEndPointSelector.MatchLabels) == 0 {
		err = errors.New("endpointSelector is empty")
	}

	return ciliumEndPointSelector, err
}

// isLoadBalancerService returns true if the Service type is "LoadBalancer" and false otherwise.
func (re *ReconcilerExtended) isServiceLoadBalancer() bool {
	return (re.service.Spec.Type == consts.ServiceTypeLoadBalancer)
}

// isCNPfound returns true if the current CiliumNetworkPolicy is not nil and false otherwise.
func (re *ReconcilerExtended) isCNPFound() bool {
	return (re.cnp != nil)
}

// shouldDeleteCNP returns true if current CiliumNetworkPolicy must be deleted and false otherwise.
// A CiliumNetworkPolicy object must be deleted under these conditions:
// 1. Owner Service object is deleted.
// 2. Owner Service type is not "LoadBalancer" and it contains controller finalizer.
func (re *ReconcilerExtended) shouldDeleteCNP() bool {
	var result bool

	if utils.IsDeleted(re.service) {
		result = true
	}

	if !re.isServiceLoadBalancer() && controllerutil.ContainsFinalizer(re.service, consts.ServiceFinalizerString) {
		result = true
	}

	return result
}

// getCNPDesiredLabels adds controller labels to the Service object labels and return it.
func (re *ReconcilerExtended) getCNPDesiredLabels() map[string]string {
	labels := make(map[string]string, len(re.service.Labels))

	for k, v := range re.service.Labels {
		labels[k] = v
	}

	// Add controller labels to the labels map to be able to find CiliumNetworkPolicy objects controlled by this controllers.
	maps.Copy(labels, controllerLabelsMap)

	return labels
}

// determineExclusionStatus determines the namespace exclusion status based on the exclusion reasons.
// It returns true if the namespace should be excluded and false otherwise.
func determineExclusionStatus(exclusionReasons map[namespaceExclusionReason]empty) bool {
	if exclusionReasons == nil {
		return false
	}

	var nonNetworkPolicy bool

	var nonUnmanagedCiliumNetworkPolicy bool

	for reason := range exclusionReasons {
		if reason == namespaceName || reason == namespaceLabels {
			return true
		}

		if reason == noNetworkPolicyExistence {
			nonNetworkPolicy = true
		}

		if reason == noUnmanagedCiliumNetworkPolicyExistence {
			nonUnmanagedCiliumNetworkPolicy = true
		}

		if nonNetworkPolicy && nonUnmanagedCiliumNetworkPolicy {
			return true
		}
	}

	return false
}

// determineExclusionReasons determine the namespace exclusion reasons based on the rules defined.
//
// Note:
// The function assumes that the cache is locked already so it must be called from a function that already has a lock on the cache.
func (re *ReconcilerExtended) determineExclusionReasons(ctx context.Context) error {
	var isCiliumNetworkPolicyAbsent bool

	var isNetworkPolicyAbsent bool

	var isNamespaceNameExcluded bool

	var isNamespaceLabelExcluded bool

	var err error

	isCiliumNetworkPolicyAbsent, err = re.isCiliumNetworkPolicyAbsent(ctx)
	if err != nil {
		return err
	}

	isNetworkPolicyAbsent, err = re.isNetworkPolicyAbsent(ctx)
	if err != nil {
		return err
	}

	isNamespaceNameExcluded = re.isNamespaceNameExcluded()

	isNamespaceLabelExcluded, err = re.isNamespaceLabelExcluded(ctx)
	if err != nil {
		return err
	}

	reasons := make(map[namespaceExclusionReason]empty)

	if isCiliumNetworkPolicyAbsent {
		reasons[noUnmanagedCiliumNetworkPolicyExistence] = struct{}{}
	}

	if isNetworkPolicyAbsent {
		reasons[noNetworkPolicyExistence] = struct{}{}
	}

	if isNamespaceNameExcluded {
		reasons[namespaceName] = struct{}{}
	}

	if isNamespaceLabelExcluded {
		reasons[namespaceLabels] = struct{}{}
	}

	re.namespaceExclusionCache[re.request.Namespace] = reasons

	return nil
}

// isNamespaceNameExcluded checks the Service object's namespace name against the excluded names list configured and returns true if matched.
func (re *ReconcilerExtended) isNamespaceNameExcluded() bool {
	cfg := config.GetConfig()

	if slices.Contains(cfg.Controller.Exclude.NamespaceSelector.MatchNames, re.service.Namespace) {
		re.logger.Info(consts.NamespaceExcludedNamesMatchInfo)

		return true
	}

	return false
}

// isNamespaceLabelExcluded checks the Service object's namespace labels map against each excluded labels map configured and returns true if matched.
func (re *ReconcilerExtended) isNamespaceLabelExcluded(ctx context.Context) (bool, error) {
	namespace := &corev1.Namespace{}

	cfg := config.GetConfig()

	if err := re.Client.Get(ctx, types.NamespacedName{Name: re.service.Namespace}, namespace); err != nil {
		return false, fmt.Errorf(consts.NamespaceExclusionGetNamespaceError, err)
	}

	for _, matchLabel := range cfg.Controller.Exclude.NamespaceSelector.MatchLabels {
		if utils.IsMapSubset(namespace.Labels, matchLabel.Labels) {
			re.logger.Info(fmt.Sprintf(consts.NamespaceExcludedLabelsMatchInfo, matchLabel.MatcherName))

			return true, nil
		}
	}

	return false, nil
}

// isNetworkPolicyAbsent list all the NetworkPolicy objects in the Service object's namespace and returns true if the list is empty.
func (re *ReconcilerExtended) isNetworkPolicyAbsent(ctx context.Context) (bool, error) {
	networkPolicyList := &networkingv1.NetworkPolicyList{}

	if err := re.Client.List(ctx, networkPolicyList, client.InNamespace(re.service.Namespace)); err != nil {
		return false, fmt.Errorf(consts.NetworkPolicyListError, err)
	}

	if len(networkPolicyList.Items) == 0 {
		re.logger.Info(consts.NamespaceExcludedNoNetworkPolicyMatchInfo)

		return true, nil
	}

	return false, nil
}

// isCiliumNetworkPolicyAbsent list all the CiliumNetworkPolicy objects in the Service object's namespace and returns true if the list is empty.
func (re *ReconcilerExtended) isCiliumNetworkPolicyAbsent(ctx context.Context) (bool, error) {
	ciliumNetworkPolicyList := &ciliumv2.CiliumNetworkPolicyList{}

	// Construct a label selector to list CiliumNetworkPolicy objects not containing the controller labels
	// This is necessary as the rule should check the presence of unmanaged policies.
	labelSelector := labels.NewSelector()

	for labelKey, labelValue := range controllerLabelsMap {
		vals := make([]string, 1)
		vals[0] = labelValue
		requirement, err := labels.NewRequirement(labelKey, selection.NotEquals, vals)

		if err != nil {
			return false, err
		}

		labelSelector = labelSelector.Add(*requirement)
	}

	if err := re.Client.List(ctx, ciliumNetworkPolicyList, client.InNamespace(re.service.Namespace), &client.ListOptions{LabelSelector: labelSelector}); err != nil {
		return false, fmt.Errorf(consts.CiliumNetworkPolicyListError, err)
	}

	if len(ciliumNetworkPolicyList.Items) == 0 {
		re.logger.Info(consts.NamespaceExcludedNoCiliumNetworkPolicyMatchInfo)

		return true, nil
	}

	return false, nil
}

// getOwnerReconcileRequest checks if a CiliumNetworkPolicy (cnp) has an owner that is a 'Service' object
// in the default Kubernetes API group ('v1'). If such an owner exists, the function constructs and returns
// a reconcile request for it.
//
// The function iterates over the owner references of the cnp. For each owner reference:
//   - It first checks if the reference is marked as a controller. If not, it skips to the next owner reference.
//   - Next, it attempts to parse the Group and Version from the OwnerReference's APIVersion. If parsing fails,
//     it logs the error and continues with the next owner reference.
//   - Then, it compares the parsed Group, Version, and Kind of the OwnerReference with the Service Group
//     (” for core group), Version ('v1'), and Kind ('Service'). If they match, it creates a reconcile Request
//     using the Name from the OwnerReference and the Namespace from the cnp.
//
// Note:
// The function assumes that the cnp's owner, if it's a Service, is part of the core Kubernetes API group ('v1').
func getOwnerReconcileRequest(ctx context.Context, cnp *ciliumv2.CiliumNetworkPolicy) *ctrl.Request {
	if cnp == nil {
		return nil
	}

	logger := log.FromContext(ctx).WithName("ciliumnetworkpolicy owner reference handler")

	for _, ref := range cnp.GetOwnerReferences() {
		if ref.Controller == nil || !*ref.Controller {
			continue
		}

		// Parse the Group/Version out of the OwnerReference
		refGV, err := schema.ParseGroupVersion(ref.APIVersion)
		if err != nil {
			logger.Error(err, "Could not parse OwnerReference APIVersion",
				"apiVersion", ref.APIVersion)

			continue
		}

		// Compare the OwnerReference Group/Version/Kind against the Service Group/Version/Kind.
		// If the two match, create a Request for the objected referred to by
		// the OwnerReference. Use the Name from the OwnerReference and the Namespace from the
		// object in the event.
		if refGV.Group == "" && refGV.Version == "v1" && ref.Kind == "Service" {
			request := reconcile.Request{NamespacedName: types.NamespacedName{
				Namespace: cnp.GetNamespace(),
				Name:      ref.Name,
			}}

			return &request
		}
	}

	return nil
}

// deduplicateRequests removes duplicate ctrl.Request objects from a slice.
func deduplicateRequests(requests []ctrl.Request) []ctrl.Request {
	seen := make(map[types.NamespacedName]bool)
	result := []ctrl.Request{}

	for _, req := range requests {
		if _, exists := seen[req.NamespacedName]; !exists {
			result = append(result, req)
			seen[req.NamespacedName] = true
		}
	}

	return result
}
