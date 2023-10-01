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
	"maps"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	cilium_slim_metav1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/apis/meta/v1"
	cilium_labels "github.com/cilium/cilium/pkg/labels"
	cilium_policy_api "github.com/cilium/cilium/pkg/policy/api"
	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/snapp-incubator/svc-lb-to-cilium-netpolicy/internal/config"
	"github.com/snapp-incubator/svc-lb-to-cilium-netpolicy/internal/consts"
	"github.com/snapp-incubator/svc-lb-to-cilium-netpolicy/pkg/utils"
)

var (
	desiredCNPannotationsMap      = map[string]string{"snappcloud.io/team": "snappcloud", "snappcloud.io/sub-team": "network"}
	desiredCNPadditionalLabelsMap = map[string]string{"snappcloud.io/controller-managed": "true", "snappcloud.io/controller": "svc-lb-to-cilium-netpolicy"}
)

// buildCNP builds and returns a CiliumNetworkPolicy struct based on the Service object.
func (re *ReconcilerExtended) buildCNP() *ciliumv2.CiliumNetworkPolicy {
	endpointSelectorLabelsMap := make(map[string]string, len(re.service.Spec.Selector))

	// Cilium imports labels from different sources.
	// It expects each label key to specify the source in the form of Source:Key
	// However, if you pass the labels in the form of map["Source:Key"]"Value", each label key would become invalid.
	// For example:
	// k8s:app.kubernetes.io/name -> k8s:app:kubernetes.io/name
	// The workaround is to add a prefix with "." instead of ":" to manipulate the labels key correctly.
	// k8s.app.kubernetes.io/name -> k8s:app.kubernetes.io/name

	// TODO: Build the labels using cilium_labels.Label struct.
	// ##########################################################################
	// CNPlabels := make(cilium_labels.Labels, len(re.service.Spec.Selector))
	// for k, v := range re.service.Spec.Selector {
	// 	CNPlabels[k] = cilium_labels.NewLabel(k, v, cilium_labels.LabelSourceK8s)
	// }
	// endpointSelectorLabelsMap := CNPlabels.StringMap()
	// ##########################################################################
	for k, v := range re.service.Spec.Selector {
		endpointSelectorLabelsMap[cilium_labels.LabelSourceK8s+cilium_labels.PathDelimiter+k] = v
	}

	cnp := &ciliumv2.CiliumNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:        re.service.Name,
			Namespace:   re.service.Namespace,
			Labels:      re.getCNPdesiredLabels(),
			Annotations: desiredCNPannotationsMap,
		},
		Spec: &cilium_policy_api.Rule{
			EndpointSelector: cilium_policy_api.EndpointSelector{
				LabelSelector: &cilium_slim_metav1.LabelSelector{
					MatchLabels: endpointSelectorLabelsMap,
				},
			},
			Ingress: []cilium_policy_api.IngressRule{
				{
					IngressCommonRule: cilium_policy_api.IngressCommonRule{
						FromEntities: cilium_policy_api.EntitySlice{
							cilium_policy_api.EntityCluster,
						},
					},
				},
			},
		},
	}

	return cnp
}

// isLoadBalancerService returns true if the Service type is "LoadBalancer" and false otherwise.
func (re *ReconcilerExtended) isServiceLoadBalancer() bool {
	return (re.service.Spec.Type == consts.ServiceTypeLoadBalancer)
}

// isCNPfound returns true if the current CiliumNetworkPolicy is not nil and false otherwise.
func (re *ReconcilerExtended) isCNPfound() bool {
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

// getCNPdesiredLabels adds controller labels to the Service object labels and return it.
func (re *ReconcilerExtended) getCNPdesiredLabels() map[string]string {
	labels := make(map[string]string, len(re.service.Labels))

	for k, v := range re.service.Labels {
		labels[k] = v
	}

	// Add controller labels to the labels map to be able to find CiliumNetworkPolicy objects controlled by this controllers.
	maps.Copy(labels, desiredCNPadditionalLabelsMap)

	return labels
}

// shouldExcludeNamespace determine the namespace exclusion status based on the configurations defined.
func (re *ReconcilerExtended) shouldExcludeNamespace(ctx context.Context) (bool, error) {
	var shouldExclude bool

	var err error

	shouldExclude, err = re.checkNoPolicyRule(ctx)
	if err != nil {
		return false, err
	}

	if shouldExclude {
		return true, nil
	}

	if re.checkExcludedNamespaceNames() {
		return true, nil
	}

	shouldExclude, err = re.checkExcludedNamespaceLabels(ctx)
	if err != nil {
		return false, err
	}

	if shouldExclude {
		return true, nil
	}

	return false, nil
}

// checkExcludedNamespaceNames checks the Service object's namespace name against the excluded names list configured and returns true if matched.
func (re *ReconcilerExtended) checkExcludedNamespaceNames() bool {
	cfg := config.GetConfig()

	if slices.Contains(cfg.Controller.Exclude.NamespaceSelector.MatchNames, re.service.Namespace) {
		re.logger.Info(consts.NamespaceExcludedNamesMatchInfo)

		return true
	}

	return false
}

// checkExcludedNamespaceLabels checks the Service object's namespace labels map against each excluded labels map configured and returns true if matched.
func (re *ReconcilerExtended) checkExcludedNamespaceLabels(ctx context.Context) (bool, error) {
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

// checkNoPolicyRule list all the NetworkPolicy and CiliumNetworkPolicy objects in the Service object's namespace and returns true if both lists are empty.
func (re *ReconcilerExtended) checkNoPolicyRule(ctx context.Context) (bool, error) {
	ciliumNetworkPolicyList := &ciliumv2.CiliumNetworkPolicyList{}
	networkPolicyList := &networkingv1.NetworkPolicyList{}

	// Construct a label selector to list CiliumNetworkPolicy objects not containing the controller labels
	// This is necessary as the rule should check the presence of unmanaged policies.
	CNPlabelSelector := labels.NewSelector()

	for labelKey, labelValue := range desiredCNPadditionalLabelsMap {
		vals := make([]string, 1)
		vals[0] = labelValue
		requirement, err := labels.NewRequirement(labelKey, selection.NotEquals, vals)

		if err != nil {
			return false, err
		}

		CNPlabelSelector = CNPlabelSelector.Add(*requirement)
	}

	if err := re.Client.List(ctx, ciliumNetworkPolicyList, client.InNamespace(re.service.Namespace), &client.ListOptions{LabelSelector: CNPlabelSelector}); err != nil {
		return false, fmt.Errorf(consts.CiliumNetworkPolicyListError, err)
	}

	if err := re.Client.List(ctx, networkPolicyList, client.InNamespace(re.service.Namespace)); err != nil {
		return false, fmt.Errorf(consts.NetworkPolicyListError, err)
	}

	if len(ciliumNetworkPolicyList.Items) == 0 && len(networkPolicyList.Items) == 0 {
		re.logger.Info(consts.NamespaceExcludedNoPolicyMatchInfo)

		return true, nil
	}

	return false, nil
}
