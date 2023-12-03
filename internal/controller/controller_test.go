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
	"time"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	cilium_slim_metav1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/apis/meta/v1"
	cilium_labels "github.com/cilium/cilium/pkg/labels"
	cilium_policy_api "github.com/cilium/cilium/pkg/policy/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/exp/maps"
	networkingv1 "k8s.io/api/networking/v1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Constants for test configuration
const (
	DefaultNamespace                  string               = "default"
	DummyName                         string               = "dummy"
	CiliumNetworkPolicyName           string               = "test" // Both ServiceName and CiliumNetworkPolicyName must be equal
	ExcludedNamespaceNameViaLabelsSet string               = "ns2"  // Set based on the configuration
	ExcludedNamespaceNameViaName      string               = "ns1"
	KubernetesFinalizer               corev1.FinalizerName = "kubernetes"
	ServiceFinalizerString            string               = "snappcloud.io/ensure-cnp-is-deleted"
	ServiceName                       string               = "test" // Both ServiceName and CiliumNetworkPolicyName must be equal
	WaitInterval                                           = 1
)

// Global variables for test configuration
var (
	CiliumNetworkPolicyControllerLabels                  map[string]string             = map[string]string{"snappcloud.io/controller-managed": "true", "snappcloud.io/controller": "svc-lb-to-cilium-netpolicy"}
	CiliumNetworkPolicyAllowedEntities                   cilium_policy_api.EntitySlice = []cilium_policy_api.Entity{cilium_policy_api.EntityCluster}
	CiliumNetworkPolicyAllowedEntitiesForExposedServices cilium_policy_api.EntitySlice = []cilium_policy_api.Entity{cilium_policy_api.EntityCluster, cilium_policy_api.EntityWorld}
	CiliumNetworkPolicyAnnotations                       map[string]string             = map[string]string{"snappcloud.io/team": "snappcloud", "snappcloud.io/sub-team": "network"}
	ExcludedNamespaceLabelsViaLabelsSet                  map[string]string             = map[string]string{"k1": "v1", "k2": "v2"} // Set based on the configuration
	MetallbAnnotations                                   map[string]string             = map[string]string{"metallb.universe.tf/address-pool": "vpn-access"}
	ServiceLabels                                        map[string]string             = map[string]string{"app.kubernetes.io/name": "test", "k2": "v2"}
	ServicePorts                                         []corev1.ServicePort          = []corev1.ServicePort{{Name: "http", Port: 80}}
	ServiceSelector                                      map[string]string             = map[string]string{"app.kubernetes.io/name": "test", "k2": "v2"}
)

// Main block for testing 'LoadBalancer Service' to CiliumNetworkPolicy controller
var _ = Describe("Testing LoadBalancer Service to CiliumNetworkPolicy Controller", func() {
	Context("Testing reconcile loop functionality", Ordered, func() {

		// Utility function to get a sample namespace
		getSampleNamespace := func(name string, labels map[string]string) *corev1.Namespace {
			return &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: labels,
				},
			}
		}

		// // https://book.kubebuilder.io/reference/envtest.html#namespace-usage-limitation
		// // https://github.com/kubernetes-sigs/controller-runtime/issues/880#issuecomment-749742403
		// deleteNamespace := func(namespace *corev1.Namespace) {
		// 	namespaceObj := &corev1.Namespace{}

		// 	Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: namespace.Name}, namespaceObj)).To(Succeed())

		// 	// Remove 'kubernetes' finalizer
		// 	finalizers := []corev1.FinalizerName{}
		// 	for _, finalizer := range namespaceObj.Spec.Finalizers {
		// 		if finalizer != KubernetesFinalizer {
		// 			finalizers = append(finalizers, finalizer)
		// 		}
		// 	}

		// 	namespacePatch := client.MergeFrom(namespaceObj.DeepCopy())

		// 	namespaceObj.Spec.Finalizers = finalizers

		// 	Expect(k8sClient.Patch(context.Background(), namespaceObj, namespacePatch)).To(Succeed())

		// 	Expect(k8sClient.Delete(context.Background(), namespace)).To(Succeed())

		// 	Eventually(func(g Gomega) {
		// 		err := k8sClient.Get(context.Background(), types.NamespacedName{
		// 			Name: namespace.Name,
		// 		}, &corev1.Namespace{})
		// 		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
		// 	}).Should(Succeed())
		// }

		// Utility function to get a sample Service
		getSampleService := func(name, namespace string) *corev1.Service {
			return &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
					Labels:    ServiceLabels,
				},
				Spec: corev1.ServiceSpec{
					Ports:    ServicePorts,
					Type:     corev1.ServiceTypeLoadBalancer,
					Selector: ServiceSelector,
				},
			}
		}

		// Utility function to get a sample exposed Service
		getSampleExposedService := func(name, namespace string) *corev1.Service {
			return &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   namespace,
					Name:        name,
					Labels:      ServiceLabels,
					Annotations: MetallbAnnotations,
				},
				Spec: corev1.ServiceSpec{
					Ports:    ServicePorts,
					Type:     corev1.ServiceTypeLoadBalancer,
					Selector: ServiceSelector,
				},
			}
		}

		// Utility function to delete a Service
		deleteService := func(service *corev1.Service) {
			Expect(k8sClient.Delete(context.Background(), service)).To(Succeed())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(context.Background(), types.NamespacedName{
					Namespace: service.Namespace,
					Name:      service.Name,
				}, &corev1.Service{})
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())
		}

		// Utility function to get labels for CiliumNetworkPolicy
		getCiliumNetworkPolicyLabels := func(service *corev1.Service) map[string]string {
			labels := make(map[string]string, len(service.Labels))

			for k, v := range service.Labels {
				labels[k] = v
			}

			maps.Copy(labels, CiliumNetworkPolicyControllerLabels)

			return labels
		}

		// Utility function to get endpoint selector labels for CiliumNetworkPolicy
		getCiliumNetworkPolicyEndpointSelectorLabels := func(service *corev1.Service) map[string]string {
			endpointSelectorLabelsMap := make(map[string]string, len(service.Spec.Selector))

			for k, v := range service.Spec.Selector {
				endpointSelectorLabelsMap[cilium_labels.LabelSourceK8s+cilium_labels.PathDelimiter+k] = v
			}

			return endpointSelectorLabelsMap
		}

		// Utility function to get a sample network policy
		getSampleNetworkPolicy := func(name, namespace string) *networkingv1.NetworkPolicy {
			return &networkingv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: networkingv1.NetworkPolicySpec{
					PodSelector: metav1.LabelSelector{},
				},
			}
		}

		// Utility function to delete a network policy
		deleteNetworkPolicy := func(networkPolicy *networkingv1.NetworkPolicy) {
			Expect(k8sClient.Delete(context.Background(), networkPolicy)).To(Succeed())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(context.Background(), types.NamespacedName{
					Namespace: networkPolicy.Namespace,
					Name:      networkPolicy.Name,
				}, &ciliumv2.CiliumNetworkPolicy{})
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())
		}

		// Utility function to get a sample CiliumNetworkPolicy
		getSampleCiliumNetworkPolicy := func(name, namespace string) *ciliumv2.CiliumNetworkPolicy {
			return &ciliumv2.CiliumNetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: &cilium_policy_api.Rule{
					EndpointSelector: cilium_policy_api.EndpointSelector{
						LabelSelector: &cilium_slim_metav1.LabelSelector{
							MatchLabels: make(map[string]string, 0),
						},
					},
					Ingress: []cilium_policy_api.IngressRule{
						{
							IngressCommonRule: cilium_policy_api.IngressCommonRule{
								FromEntities: cilium_policy_api.EntitySlice{
									cilium_policy_api.EntityAll,
								},
							},
						},
					},
				},
			}
		}

		// Utility function to delete a CiliumNetworkPolicy
		deleteCiliumNetworkPolicy := func(ciliumNetworkPolicy *ciliumv2.CiliumNetworkPolicy) {
			Expect(k8sClient.Delete(context.Background(), ciliumNetworkPolicy)).To(Succeed())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(context.Background(), types.NamespacedName{
					Namespace: ciliumNetworkPolicy.Namespace,
					Name:      ciliumNetworkPolicy.Name,
				}, &ciliumv2.CiliumNetworkPolicy{})
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())
		}

		// Test case: Verifies that a CiliumNetworkPolicy object is not created when the Service type is ClusterIP and the Namespace is not excluded
		It("should not create CiliumNetworkPolicy object when 'Service type is ClusterIP' and 'Namespace is not excluded'", func() {
			// Get a sample Service with type ClusterIP in the default namespace
			serviceObj := getSampleService(ServiceName, DefaultNamespace)
			serviceObj.Spec.Type = corev1.ServiceTypeClusterIP

			// Get a dummy CiliumNetworkPolicy object
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			// Create the dummy CiliumNetworkPolicy object to bypass the 'NoPolicyRule' rule and verify it succeeds
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Create the Service object and verify it succeeds
			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Verify that the corresponding CiliumNetworkPolicy object does not exist (indicating it was not created)
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// Cleanup: Delete the dummy CiliumNetworkPolicy and Service objects
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)
			deleteService(serviceObj)
		})

		// Test case: Verifies that a CiliumNetworkPolicy object is not created when the Service type is NodePort and the Namespace is not excluded
		It("should not create CiliumNetworkPolicy object when 'Service type is NodePort' and 'Namespace is not excluded'", func() {
			// Get a sample Service with type NodePort in the default namespace
			serviceObj := getSampleService(ServiceName, DefaultNamespace)
			serviceObj.Spec.Type = corev1.ServiceTypeNodePort

			// Get a dummy CiliumNetworkPolicy object
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			// Create the dummy CiliumNetworkPolicy object to bypass the 'NoPolicyRule' rule and verify it succeeds
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Create the Service object and verify it succeeds
			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Verify that the corresponding CiliumNetworkPolicy object does not exist (indicating it was not created)
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// Cleanup: Delete the dummy CiliumNetworkPolicy and Service objects
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)
			deleteService(serviceObj)
		})

		// Test case: Verifies that a CiliumNetworkPolicy object is not created when the Service type is ExternalName and the Namespace is not excluded
		It("should not create CiliumNetworkPolicy object when 'Service type is ExternalName' and 'Namespace is not excluded'", func() {
			// Get a sample Service with type ExternalName in the default namespace
			serviceObj := getSampleService(ServiceName, DefaultNamespace)
			serviceObj.Spec.Type = corev1.ServiceTypeExternalName
			serviceObj.Spec.ExternalName = "test.local"

			// Get a dummy CiliumNetworkPolicy object
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			// Create the dummy CiliumNetworkPolicy object to bypass the 'NoPolicyRule' rule and verify it succeeds
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Create the Service object and verify it succeeds
			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Verify that the corresponding CiliumNetworkPolicy object does not exist (indicating it was not created)
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// Cleanup: Delete the dummy CiliumNetworkPolicy and Service objects
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)
			deleteService(serviceObj)
		})

		// Test case: Verifies that a CiliumNetworkPolicy object is not created when the Service type is LoadBalancer and the Namespace is excluded based on its name
		It("should not create CiliumNetworkPolicy object when 'Service type is LoadBalancer' and 'Namespace is excluded based on its name'", func() {
			// Get a sample Namespace that is meant to be excluded based on its name
			namespaceObj := getSampleNamespace(ExcludedNamespaceNameViaName, make(map[string]string, 0))

			// Get a sample Service with type LoadBalancer in the excluded namespace
			serviceObj := getSampleService(ServiceName, ExcludedNamespaceNameViaName)

			// Create the Namespace object and verify it succeeds
			Expect(k8sClient.Create(context.Background(), namespaceObj)).To(Succeed())

			// Get a dummy CiliumNetworkPolicy object
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, ExcludedNamespaceNameViaName)

			// Create the dummy CiliumNetworkPolicy object to bypass the 'NoPolicyRule' rule and verify it succeeds
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Create the Service object and verify it succeeds
			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Verify that the corresponding CiliumNetworkPolicy object does not exist (indicating it was not created in the excluded namespace)
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: ExcludedNamespaceNameViaName, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// Cleanup: Delete the dummy CiliumNetworkPolicy and Service objects
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)
			deleteService(serviceObj)
		})

		// Test case: Verifies that a CiliumNetworkPolicy object is not created when the Service type is LoadBalancer and the Namespace is excluded based on its labels
		It("should not create CiliumNetworkPolicy object when 'Service type is LoadBalancer' and 'Namespace is excluded based on its labels'", func() {
			// Get a sample Namespace that is meant to be excluded based on its labels
			namespaceObj := getSampleNamespace(ExcludedNamespaceNameViaLabelsSet, ExcludedNamespaceLabelsViaLabelsSet)

			// Get a sample Service with type LoadBalancer in the excluded namespace
			serviceObj := getSampleService(ServiceName, ExcludedNamespaceNameViaLabelsSet)

			// Create the Namespace object and verify it succeeds
			Expect(k8sClient.Create(context.Background(), namespaceObj)).To(Succeed())

			// Get a dummy CiliumNetworkPolicy object
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, ExcludedNamespaceNameViaLabelsSet)

			// Create the dummy CiliumNetworkPolicy object to bypass the 'NoPolicyRule' rule and verify it succeeds
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Create the Service object and verify it succeeds
			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Verify that the corresponding CiliumNetworkPolicy object does not exist (indicating it was not created in the excluded namespace)
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: ExcludedNamespaceNameViaLabelsSet, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// Cleanup: Delete the dummy CiliumNetworkPolicy and Service objects
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)
			deleteService(serviceObj)
		})

		// Test case: Verifies that a CiliumNetworkPolicy object is not created when the Service type is LoadBalancer and the Namespace is excluded based on the NoPolicy rule
		It("should not create CiliumNetworkPolicy object when 'Service type is LoadBalancer' and 'Namespace is excluded based on the NoPolicy rule'", func() {
			// Get a sample Service with type LoadBalancer in the default namespace
			serviceObj := getSampleService(ServiceName, DefaultNamespace)

			// Create the Service object and verify it succeeds
			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Verify that a CiliumNetworkPolicy object does not exist for the Service
			// This confirms that the NoPolicy rule is in effect, and the Service type being LoadBalancer doesn't lead to CiliumNetworkPolicy creation
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// Cleanup: Delete the Service object
			deleteService(serviceObj)
		})

		// Test case: Verifies that a CiliumNetworkPolicy object is created when the Service type is LoadBalancer and the Namespace is not excluded due to the existence of a CiliumNetworkPolicy
		It("should create CiliumNetworkPolicy object when 'Service type is LoadBalancer' and 'Namespace is not excluded due to the existence of a CiliumNetworkPolicy'", func() {
			// Get a sample Service with type LoadBalancer in the default namespace
			serviceObj := getSampleService(ServiceName, DefaultNamespace)

			// Get a dummy CiliumNetworkPolicy object
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			// Create the dummy CiliumNetworkPolicy object to bypass the 'NoPolicyRule' rule and verify it succeeds
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Create the Service object and verify it succeeds
			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Verify the creation of a CiliumNetworkPolicy object corresponding to the Service
			// This confirms that the presence of a CiliumNetworkPolicy object leads to the creation of the actual CiliumNetworkPolicy for the LoadBalancer Service
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			// Cleanup: Delete the dummy CiliumNetworkPolicy and Service objects
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)
			deleteService(serviceObj)
		})

		// Test case: Verifies that a CiliumNetworkPolicy object is created when the Service type is LoadBalancer and the Namespace is not excluded due to the existence of a NetworkPolicy
		It("should create CiliumNetworkPolicy object when 'Service type is LoadBalancer' and 'Namespace is not excluded due to the existence of a NetworkPolicy'", func() {
			// Get a sample Service with type LoadBalancer in the default namespace
			serviceObj := getSampleService(ServiceName, DefaultNamespace)

			// Get a dummy NetworkPolicy object
			dummyNetworkPolicyObj := getSampleNetworkPolicy(DummyName, DefaultNamespace)

			// Create the dummy NetworkPolicy object to bypass the 'NoPolicyRule' rule and verify it succeeds
			Expect(k8sClient.Create(context.Background(), dummyNetworkPolicyObj)).To(Succeed())

			// Create the Service object and verify it succeeds
			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Verify the creation of a CiliumNetworkPolicy object corresponding to the Service
			// This confirms that the presence of a NetworkPolicy object leads to the creation of the actual CiliumNetworkPolicy for the LoadBalancer Service
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: DummyName}, &networkingv1.NetworkPolicy{})).To(Succeed())

			// Cleanup: Delete the dummy NetworkPolicy and Service objects
			deleteNetworkPolicy(dummyNetworkPolicyObj)
			deleteService(serviceObj)
		})

		// Test case: Verifies that all Service of type LoadBalancer within a Namespace are reconciled after the Namespace exclusion status is updated
		It("should reconcile all Service of type LoadBalancer within a Namespace after the Namespace exclusion status is updated (Namespace update event)", func() {
			// Get a sample Service with type LoadBalancer in the default namespace
			serviceObj := getSampleService(ServiceName, DefaultNamespace)

			// Create the Service object and verify it succeeds
			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			// Get a dummy CiliumNetworkPolicy object
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			// Create the dummy CiliumNetworkPolicy object to bypass the 'NoPolicyRule' rule and verify it succeeds
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Verify the creation of a CiliumNetworkPolicy object corresponding to the Service
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			// Retrieve and update the Namespace object to change its exclusion status
			namespaceObj := &corev1.Namespace{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: DefaultNamespace}, namespaceObj)).To(Succeed())
			namespaceObj.Labels = ExcludedNamespaceLabelsViaLabelsSet
			Expect(k8sClient.Update(context.Background(), namespaceObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Verify the absence of CiliumNetworkPolicy after Namespace update
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// Reset the Namespace labels
			namespaceObj = &corev1.Namespace{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: DefaultNamespace}, namespaceObj)).To(Succeed())
			namespaceObj.Labels = nil
			Expect(k8sClient.Update(context.Background(), namespaceObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Verify the presence of CiliumNetworkPolicy after Namespace update
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			// Cleanup: Delete the dummy CiliumNetworkPolicy and Service objects
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)
			deleteService(serviceObj)
		})

		// Test case: Verifies reconciliation of all LoadBalancer type Services within a Namespace following a NetworkPolicy create/delete event
		It("should reconcile all Service of type LoadBalancer objects within a Namespace after the Namespace exclusion status is updated (NetworkPolicy create/delete event)", func() {
			// Get a sample Service with type LoadBalancer in the default namespace
			serviceObj := getSampleService(ServiceName, DefaultNamespace)

			// Create and verify the Service object
			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Verify that a CiliumNetworkPolicy does not exist initially
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// Get a dummy NetworkPolicy object
			dummyNetworkPolicyObj := getSampleNetworkPolicy(DummyName, DefaultNamespace)

			// Create and verify the dummy NetworkPolicy object to trigger a create event
			Expect(k8sClient.Create(context.Background(), dummyNetworkPolicyObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Verify the creation of CiliumNetworkPolicy following the NetworkPolicy create event
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			// Delete the dummy NetworkPolicy object to trigger a delete event
			deleteNetworkPolicy(dummyNetworkPolicyObj)

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Verify the absence of CiliumNetworkPolicy following the NetworkPolicy delete event
			err = k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// Cleanup: Delete the Service object
			deleteService(serviceObj)
		})

		// Test case: Ensures reconciliation of all LoadBalancer type Services within a Namespace in response to CiliumNetworkPolicy create/update/delete events
		It("should reconcile all Service of type LoadBalancer objects within a Namespace after the Namespace exclusion status is updated (CiliumNetworkPolicy create/update/delete event)", func() {
			// Create and verify a sample LoadBalancer Service in the default namespace
			serviceObj := getSampleService(ServiceName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			// Verify that CiliumNetworkPolicy does not exist initially
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// Create a dummy CiliumNetworkPolicy with controller-specific labels and verify
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)
			dummyCiliumNetworkPolicyObj.Labels = CiliumNetworkPolicyControllerLabels
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Verify absence of CiliumNetworkPolicy following creation
			err = k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// Update the dummy CiliumNetworkPolicy by removing controller-specific labels and verify
			dummyCiliumNetworkPolicyObj = &ciliumv2.CiliumNetworkPolicy{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: DummyName}, dummyCiliumNetworkPolicyObj)).To(Succeed())
			dummyCiliumNetworkPolicyObj.Labels = nil
			Expect(k8sClient.Update(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Verify creation of CiliumNetworkPolicy following update
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			// Delete the dummy CiliumNetworkPolicy
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Verify absence of CiliumNetworkPolicy following update
			err = k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// Cleanup: Delete the Service object
			deleteService(serviceObj)
		})

		// Test case: Ensures that the CiliumNetworkPolicy object's Spec.EndpointSelector.MatchLabels are correctly set based on the Service object's Spec.Selector
		It("should set CiliumNetworkPolicy object \"Spec.EndpointSelector.MatchLabels\" based on Service object \"Spec.Selector\"", func() {
			// Create and verify a sample LoadBalancer Service in the default namespace
			serviceObj := getSampleService(ServiceName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			// Create a dummy CiliumNetworkPolicy object to bypass the 'NoPolicyRule' rule and verify its creation
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Retrieve the created CiliumNetworkPolicy object and verify its EndpointSelector.MatchLabels
			ciliumNetworkPolicyObj := ciliumv2.CiliumNetworkPolicy{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumNetworkPolicyObj)).To(Succeed())
			Expect(ciliumNetworkPolicyObj.Spec.EndpointSelector.MatchLabels).To(Equal(getCiliumNetworkPolicyEndpointSelectorLabels(serviceObj)))

			// Cleanup: Delete the dummy CiliumNetworkPolicy and Service objects
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)
			deleteService(serviceObj)
		})

		// Test case: Verifies that the CiliumNetworkPolicy object's Spec.Ingress[0].FromEntities are set based on the CiliumNetworkPolicyAllowedEntities variable for non-exposed services
		It("should set CiliumNetworkPolicy object \"Spec.Ingress[0].FromEntities\" based on CiliumNetworkPolicyAllowedEntities variable if the Service is not exposed", func() {
			// Create and verify a sample LoadBalancer Service in the default namespace
			serviceObj := getSampleService(ServiceName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			// Create a dummy CiliumNetworkPolicy object to bypass the 'NoPolicyRule' rule and verify its creation
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			//  Retrieve the created CiliumNetworkPolicy object and verify its Ingress FromEntities
			ciliumNetworkPolicyObj := ciliumv2.CiliumNetworkPolicy{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumNetworkPolicyObj)).To(Succeed())
			Expect(ciliumNetworkPolicyObj.Spec.Ingress[0].FromEntities).To(Equal(CiliumNetworkPolicyAllowedEntities))

			// Cleanup: Delete the dummy CiliumNetworkPolicy and Service objects
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)
			deleteService(serviceObj)
		})

		// Test case: Verifies that the CiliumNetworkPolicy object's Spec.Ingress[0].FromEntities are set based on the CiliumNetworkPolicyAllowedEntitiesForExposedServices variable for exposed services
		It("should set CiliumNetworkPolicy object \"Spec.Ingress[0].FromEntities\" based on CiliumNetworkPolicyAllowedEntities variable if the Service is exposed", func() {
			// Create and verify a sample LoadBalancer Service in the default namespace
			serviceObj := getSampleExposedService(ServiceName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			// Create a dummy CiliumNetworkPolicy object to bypass the 'NoPolicyRule' rule and verify its creation
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Retrieve the created CiliumNetworkPolicy object and verify its Ingress FromEntities for exposed services
			ciliumNetworkPolicyObj := ciliumv2.CiliumNetworkPolicy{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumNetworkPolicyObj)).To(Succeed())
			Expect(ciliumNetworkPolicyObj.Spec.Ingress[0].FromEntities).To(Equal(CiliumNetworkPolicyAllowedEntitiesForExposedServices))

			// Cleanup: Delete the dummy CiliumNetworkPolicy and Service objects
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)
			deleteService(serviceObj)
		})

		// Test case: Verifies that a CiliumNetworkPolicy object's annotations are set based on a predefined variable
		It("should set CiliumNetworkPolicy object 'ObjectMeta.Annotations' based on CiliumNetworkPolicyAnnotations variable", func() {
			// Create and verify a sample LoadBalancer Service in the default namespace
			serviceObj := getSampleService(ServiceName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			// Create a dummy CiliumNetworkPolicy object to bypass the 'NoPolicyRule' rule and verify its creation
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Retrieve the created CiliumNetworkPolicy object
			ciliumNetworkPolicyObj := ciliumv2.CiliumNetworkPolicy{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumNetworkPolicyObj)).To(Succeed())

			// Assert that each annotation in the CiliumNetworkPolicy object matches the predefined variable
			for k, v := range CiliumNetworkPolicyAnnotations {
				Expect(ciliumNetworkPolicyObj.ObjectMeta.Annotations[k]).To(Equal(v))
			}

			// Cleanup: Delete the dummy CiliumNetworkPolicy and Service objects
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)
			deleteService(serviceObj)
		})

		// Test case: Checks if CiliumNetworkPolicy object labels are set based on Service object labels and additional predefined labels
		It("should set CiliumNetworkPolicy object 'ObjectMeta.Labels' based on Service object 'ObjectMeta.Labels' and upsert additional labels defined in CiliumNetworkPolicyAdditionalLabels variable", func() {
			// Create and verify a sample LoadBalancer Service in the default namespace
			serviceObj := getSampleService(ServiceName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			// Create a dummy CiliumNetworkPolicy object to bypass the 'NoPolicyRule' rule and verify its creation
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Retrieve the created CiliumNetworkPolicy object and validate its labels
			time.Sleep(WaitInterval * time.Second)
			ciliumNetworkPolicyObj := ciliumv2.CiliumNetworkPolicy{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumNetworkPolicyObj)).To(Succeed())
			Expect(ciliumNetworkPolicyObj.ObjectMeta.Labels).To(Equal(getCiliumNetworkPolicyLabels(serviceObj)))

			// Cleanup: Delete the dummy CiliumNetworkPolicy and the Service object
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)
			deleteService(serviceObj)
		})

		// Test case: Verifies that a finalizer string is added to a Service object after creating a CiliumNetworkPolicy object
		It("should add finalizer string to Service object after creating CiliumNetworkPolicy object", func() {
			// Create and verify a sample LoadBalancer Service in the default namespace
			desiredServiceObj := getSampleService(ServiceName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), desiredServiceObj)).To(Succeed())

			// Create a dummy CiliumNetworkPolicy object to bypass the 'NoPolicyRule' rule and verify its creation
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Retrieve the created Service object, ensuring the finalizer string is present
			currentServiceObj := corev1.Service{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &currentServiceObj)).To(Succeed())
			Expect(currentServiceObj.ObjectMeta.Finalizers).To(ContainElement(ServiceFinalizerString))

			// Cleanup: Delete the dummy CiliumNetworkPolicy and the Service object
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)
			deleteService(desiredServiceObj)
		})

		// Test case: Verifies that a CiliumNetworkPolicy object is deleted when a Service type is transitioned from LoadBalancer to ClusterIP
		It("should delete CiliumNetworkPolicy object when Service type is transitioned from LoadBalancer into ClusterIP", func() {
			// Create and verify a sample LoadBalancer Service in the default namespace
			desiredServiceObj := getSampleService(ServiceName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), desiredServiceObj)).To(Succeed())

			// Create a dummy CiliumNetworkPolicy object to bypass the 'NoPolicyRule' rule and verify its creation
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Retrieve the created CiliumNetworkPolicy object and verify it succeeds
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			// Update the Service object to change its type to ClusterIP
			currentServiceObj := corev1.Service{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &currentServiceObj)).To(Succeed())
			currentServiceObj.Spec.Type = corev1.ServiceTypeClusterIP
			Expect(k8sClient.Update(context.Background(), &currentServiceObj)).To(Succeed())

			// Wait again for the state update
			time.Sleep(WaitInterval * time.Second)

			// Verify that the CiliumNetworkPolicy object is not found (i.e., deleted) after the Service type update
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// Cleanup: Delete the dummy CiliumNetworkPolicy and the Service object
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)
			deleteService(desiredServiceObj)
		})

		// Test case: Ensures that the CiliumNetworkPolicy object is deleted when a Service's type transitions from LoadBalancer to NodePort
		It("should delete CiliumNetworkPolicy object when Service type is transitioned from LoadBalancer into NodePort", func() {
			// Create and verify a sample LoadBalancer Service in the default namespace
			desiredServiceObj := getSampleService(ServiceName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), desiredServiceObj)).To(Succeed())

			// Create a dummy CiliumNetworkPolicy object to bypass the 'NoPolicyRule' rule and verify its creation
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Retrieve the created CiliumNetworkPolicy object and verify it succeeds
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			// Update the Service object to change its type to NodePort
			currentServiceObj := corev1.Service{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &currentServiceObj)).To(Succeed())
			currentServiceObj.Spec.Type = corev1.ServiceTypeNodePort
			Expect(k8sClient.Update(context.Background(), &currentServiceObj)).To(Succeed())

			// Wait again for the state update
			time.Sleep(WaitInterval * time.Second)

			// Verify that the CiliumNetworkPolicy object is not found (i.e., deleted) after the Service type update
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// Cleanup: Delete the dummy CiliumNetworkPolicy and the Service object
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)
			deleteService(desiredServiceObj)
		})

		// Test case: Ensures that the CiliumNetworkPolicy object is deleted when a Service's type transitions from LoadBalancer to ExternalName
		It("should delete CiliumNetworkPolicy object when Service type is transitioned from LoadBalancer into ExternalName", func() {
			// Create and verify a sample LoadBalancer Service in the default namespace
			desiredServiceObj := getSampleService(ServiceName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), desiredServiceObj)).To(Succeed())

			// Create a dummy CiliumNetworkPolicy object to bypass the 'NoPolicyRule' rule and verify its creation
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Retrieve the created CiliumNetworkPolicy object and verify it succeeds
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			// Update the Service object to change its type to ExternalName
			currentServiceObj := corev1.Service{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &currentServiceObj)).To(Succeed())
			currentServiceObj.Spec.Type = corev1.ServiceTypeExternalName
			currentServiceObj.Spec.ExternalName = "snappcloud.io"
			Expect(k8sClient.Update(context.Background(), &currentServiceObj)).To(Succeed())

			// Wait again for the state update
			time.Sleep(WaitInterval * time.Second)

			// Verify that the CiliumNetworkPolicy object is not found (i.e., deleted) after the Service type update
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// Cleanup: Delete the dummy CiliumNetworkPolicy and the Service object
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)
			deleteService(desiredServiceObj)
		})

		// Test case: Ensures that the finalizer string is removed from the Service object after deleting the associated CiliumNetworkPolicy object
		It("should delete finalizer string from Service object after deleting CiliumNetworkPolicy object", func() {
			// Create and verify a sample LoadBalancer Service in the default namespace
			desiredServiceObj := getSampleService(ServiceName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), desiredServiceObj)).To(Succeed())

			// Create a dummy CiliumNetworkPolicy object to bypass the 'NoPolicyRule' rule and verify its creation
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)
			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			// Wait for a specified interval to ensure the state is stable
			time.Sleep(WaitInterval * time.Second)

			// Retrieve the created CiliumNetworkPolicy object and verify it succeeds
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			// Fetch and validate the created Service object, ensuring the finalizer string is present
			currentServiceObj := corev1.Service{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &currentServiceObj)).To(Succeed())
			Expect(currentServiceObj.ObjectMeta.Finalizers).To(ContainElement(ServiceFinalizerString))

			// Update the Service object to change its type to ClusterIP
			currentServiceObj.Spec.Type = corev1.ServiceTypeClusterIP
			Expect(k8sClient.Update(context.Background(), &currentServiceObj)).To(Succeed())

			// Wait again for the state update
			time.Sleep(WaitInterval * time.Second)

			// Fetch and verify that the finalizer string is removed from the Service object after the update
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &currentServiceObj)).To(Succeed())
			Expect(currentServiceObj.ObjectMeta.Finalizers).To(Not(ContainElement(ServiceFinalizerString)))

			// Cleanup: Delete the dummy CiliumNetworkPolicy and the Service object
			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)
			deleteService(desiredServiceObj)
		})

	})
})
