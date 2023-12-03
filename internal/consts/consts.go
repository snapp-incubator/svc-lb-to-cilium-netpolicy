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

package consts

import (
	corev1 "k8s.io/api/core/v1"
)

const (
	CiliumNetworkPolicyCreateError                  string             = "failed to create the CiliumNetworkPolicy object: %w"
	CiliumNetworkPolicyCreateSkipInfo               string             = "skip the CiliumNetworkPolicy object creation since the Service type is not LoadBalancer"
	CiliumNetworkPolicyDeleteError                  string             = "failed to delete the CiliumNetworkPolicy object: %w"
	CiliumNetworkPolicyDeleteSkipInfo               string             = "skip the CiliumNetworkPolicy object deletion since the object is not found"
	CiliumNetworkPolicyFindError                    string             = "failed to find the CiliumNetworkPolicy object owned by the service object"
	CiliumNetworkPolicyListError                    string             = "failed to get the list of CiliumNetworkPolicy objects: %w"
	CiliumNetworkPolicyUpdateError                  string             = "failed to update the CiliumNetworkPolicy object: %w"
	CiliumNetworkPolicyUpdateSkipInfo               string             = "skip update to the current CiliumNetworkPolicy object since it is the same as the desired CiliumNetworkPolicy object"
	ControllerReferenceSetError                     string             = "failed to set ownerReference on the CiliumNetworkPolicy object: %w"
	NamespaceExcludedLabelsMatchInfo                string             = "Namespace matched against excluded labels matcher \"%s\""
	NamespaceExcludedNamesMatchInfo                 string             = "Namespace matched against excluded names list"
	NamespaceExcludedNoCiliumNetworkPolicyMatchInfo string             = "Namespace matched against \"no existing unmanaged CiliumNetworkPolicy object\" rule"
	NamespaceExcludedNoNetworkPolicyMatchInfo       string             = "Namespace matched against \"no existing NetworkPolicy object\" rule"
	NamespaceExclusionCacheEntryNotSetInfo          string             = "The namespace exclusion cache entry is not set yet"
	NamespaceExclusionGetNamespaceError             string             = "failed to get Namespace object: %w"
	NamespaceExclusionReasonAddedInfo               string             = " and specific exclusion reason is added to its cache entry"
	NamespaceExclusionStateError                    string             = "failed to determine Namespace exclusion state: %w"
	NetworkPolicyListError                          string             = "failed to get the list of NetworkPolicy objects: %w"
	ServiceAddFinalizerError                        string             = "failed to add finalizer to the Service object: %w"
	ServiceGetError                                 string             = "failed to get the Service object: %w"
	ServiceFinalizerString                          string             = "snappcloud.io/ensure-cnp-is-deleted"
	ServiceNotFoundInfo                             string             = "Service object not found; returned and not requeued"
	ServiceRemoveFinalizerError                     string             = "failed to remove finalizer from the Service object: %w"
	ServiceTypeLoadBalancer                         corev1.ServiceType = "LoadBalancer"
	ServiceTypeNotLoadBalancerInfo                  string             = "Service type is not LoadBalancer; returned and not requeued"
)
