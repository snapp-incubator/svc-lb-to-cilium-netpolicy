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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	DefaultNamespace string = "default"
	DummyName        string = "dummy"
	// Both ServiceName and CiliumNetworkPolicyName must be equal.
	CiliumNetworkPolicyName string = "test"
	// Set base on the configuration
	ExcludedNamespaceNameViaLabelsSet string               = "ns2"
	ExcludedNamespaceNameViaName      string               = "ns1"
	KubernetesFinalizer               corev1.FinalizerName = "kubernetes"
	ServiceFinalizerString            string               = "snappcloud.io/ensure-cnp-is-deleted"
	// Both ServiceName and CiliumNetworkPolicyName must be equal.
	ServiceName  string = "test"
	WaitInterval        = 2
)

var (
	CiliumNetworkPolicyAdditionalLabels map[string]string             = map[string]string{"snappcloud.io/controller-managed": "true", "snappcloud.io/controller": "svc-lb-to-cilium-netpolicy"}
	CiliumNetworkPolicyAllowedEntities  cilium_policy_api.EntitySlice = []cilium_policy_api.Entity{"cluster"}
	CiliumNetworkPolicyAnnotations      map[string]string             = map[string]string{"snappcloud.io/team": "snappcloud", "snappcloud.io/sub-team": "network"}
	// Set base on the configuration
	ExcludedNamespaceLabelsViaLabelsSet map[string]string    = map[string]string{"k1": "v1", "k2": "v2"}
	ServiceLabels                       map[string]string    = map[string]string{"app.kubernetes.io/name": "test", "k2": "v2"}
	ServicePorts                        []corev1.ServicePort = []corev1.ServicePort{{Name: "http", Port: 80}}
	ServiceSelector                     map[string]string    = map[string]string{"app.kubernetes.io/name": "test", "k2": "v2"}
)

var _ = Describe("Testing LoadBalancer Service to CiliumNetworkPolicy Controller", func() {
	Context("Testing reconcile loop functionality", Ordered, func() {

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

		getCiliumNetworkPolicyLabels := func(service *corev1.Service) map[string]string {
			labels := make(map[string]string, len(service.Labels))

			for k, v := range service.Labels {
				labels[k] = v
			}

			maps.Copy(labels, CiliumNetworkPolicyAdditionalLabels)

			return labels
		}

		getCiliumNetworkPolicyEndpointSelectorLabels := func(service *corev1.Service) map[string]string {
			endpointSelectorLabelsMap := make(map[string]string, len(service.Spec.Selector))

			for k, v := range service.Spec.Selector {
				endpointSelectorLabelsMap[cilium_labels.LabelSourceK8s+cilium_labels.PathDelimiter+k] = v
			}

			return endpointSelectorLabelsMap
		}

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

		It("should not create CiliumNetworkPolicy object when 'Service type is ClusterIP' and 'Namespace is not excluded'", func() {
			serviceObj := getSampleService(ServiceName, DefaultNamespace)
			serviceObj.Spec.Type = corev1.ServiceTypeClusterIP

			// This CiliumNetworkPolicy object is dummy since it's only used to bypass the 'NoPolicyRule' rule.
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: DummyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &corev1.Service{})).To(Succeed())

			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)

			deleteService(serviceObj)
		})

		It("should not create CiliumNetworkPolicy object when 'Service type is NodePort' and 'Namespace is not excluded'", func() {
			serviceObj := getSampleService(ServiceName, DefaultNamespace)
			serviceObj.Spec.Type = corev1.ServiceTypeNodePort

			// This CiliumNetworkPolicy object is dummy since it's only used to bypass the 'NoPolicyRule' rule.
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: DummyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &corev1.Service{})).To(Succeed())

			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)

			deleteService(serviceObj)
		})

		It("should not create CiliumNetworkPolicy object when 'Service type is ExternalName' and 'Namespace is not excluded'", func() {
			serviceObj := getSampleService(ServiceName, DefaultNamespace)
			serviceObj.Spec.Type = corev1.ServiceTypeExternalName
			serviceObj.Spec.ExternalName = "snappcloud.io"

			// This CiliumNetworkPolicy object is dummy since it's only used to bypass the 'NoPolicyRule' rule.
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: DummyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &corev1.Service{})).To(Succeed())

			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)

			deleteService(serviceObj)
		})

		It("should not create CiliumNetworkPolicy object when 'Service type is LoadBalancer' and 'Namespace is excluded based on its name'", func() {
			namespaceObj := getSampleNamespace(ExcludedNamespaceNameViaName, make(map[string]string, 0))
			serviceObj := getSampleService(ServiceName, ExcludedNamespaceNameViaName)

			// Create a namespace with the name 'ExcludedNamespaceName'
			Expect(k8sClient.Create(context.Background(), namespaceObj)).To(Succeed())
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: ExcludedNamespaceNameViaName}, &corev1.Namespace{})).To(Succeed())

			// This CiliumNetworkPolicy object is dummy since it's only used to bypass the 'NoPolicyRule' rule.
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, ExcludedNamespaceNameViaName)

			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: ExcludedNamespaceNameViaName, Name: DummyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: ExcludedNamespaceNameViaName, Name: ServiceName}, &corev1.Service{})).To(Succeed())

			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: ExcludedNamespaceNameViaName, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)

			deleteService(serviceObj)
		})

		It("should not create CiliumNetworkPolicy object when 'Service type is LoadBalancer' and 'Namespace is excluded based on its labels'", func() {
			namespaceObj := getSampleNamespace(ExcludedNamespaceNameViaLabelsSet, ExcludedNamespaceLabelsViaLabelsSet)
			serviceObj := getSampleService(ServiceName, ExcludedNamespaceNameViaLabelsSet)

			// Create a namespace with the name 'ExcludedNamespaceNameViaLabelsSet' and the labels 'ExcludedNamespaceLabelsViaLabelsSet'
			Expect(k8sClient.Create(context.Background(), namespaceObj)).To(Succeed())
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: ExcludedNamespaceNameViaLabelsSet}, &corev1.Namespace{})).To(Succeed())

			// This CiliumNetworkPolicy object is dummy since it's only used to bypass the 'NoPolicyRule' rule.
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, ExcludedNamespaceNameViaLabelsSet)

			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: ExcludedNamespaceNameViaLabelsSet, Name: DummyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: ExcludedNamespaceNameViaLabelsSet, Name: ServiceName}, &corev1.Service{})).To(Succeed())

			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: ExcludedNamespaceNameViaLabelsSet, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)

			deleteService(serviceObj)
		})

		It("should not create CiliumNetworkPolicy object when 'Service type is LoadBalancer' and 'Namespace is excluded based on NoPolicy rule'", func() {
			serviceObj := getSampleService(ServiceName, DefaultNamespace)

			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &corev1.Service{})).To(Succeed())

			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			deleteService(serviceObj)
		})

		It("should create CiliumNetworkPolicy object when 'Service type is LoadBalancer' and 'Namespace is not excluded'", func() {
			serviceObj := getSampleService(ServiceName, DefaultNamespace)

			// This CiliumNetworkPolicy object is dummy since it's only used to bypass the 'NoPolicyRule' rule.
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: DummyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &corev1.Service{})).To(Succeed())
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)

			deleteService(serviceObj)
		})

		It("should set CiliumNetworkPolicy object \"Spec.EndpointSelector.MatchLabels\" based on Service object \"Spec.Selector\"", func() {
			serviceObj := getSampleService(ServiceName, DefaultNamespace)

			// This CiliumNetworkPolicy object is dummy since it's only used to bypass the 'NoPolicyRule' rule.
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: DummyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &corev1.Service{})).To(Succeed())

			ciliumNetworkPolicyObj := ciliumv2.CiliumNetworkPolicy{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumNetworkPolicyObj)).To(Succeed())
			Expect(ciliumNetworkPolicyObj.Spec.EndpointSelector.MatchLabels).To(Equal(getCiliumNetworkPolicyEndpointSelectorLabels(serviceObj)))

			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)

			deleteService(serviceObj)
		})

		It("should set CiliumNetworkPolicy object \"Spec.Ingress[0].FromEntities\" based on CiliumNetworkPolicyAllowedEntities variable", func() {
			serviceObj := getSampleService(ServiceName, DefaultNamespace)

			// This CiliumNetworkPolicy object is dummy since it's only used to bypass the 'NoPolicyRule' rule.
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: DummyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &corev1.Service{})).To(Succeed())

			ciliumNetworkPolicyObj := ciliumv2.CiliumNetworkPolicy{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumNetworkPolicyObj)).To(Succeed())
			Expect(ciliumNetworkPolicyObj.Spec.Ingress[0].FromEntities).To(Equal(CiliumNetworkPolicyAllowedEntities))

			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)

			deleteService(serviceObj)
		})

		It("should set CiliumNetworkPolicy object \"ObjectMeta.Annotations\" based on CiliumNetworkPolicyAnnotations variable", func() {
			serviceObj := getSampleService(ServiceName, DefaultNamespace)

			// This CiliumNetworkPolicy object is dummy since it's only used to bypass the 'NoPolicyRule' rule.
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: DummyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &corev1.Service{})).To(Succeed())

			ciliumNetworkPolicyObj := ciliumv2.CiliumNetworkPolicy{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumNetworkPolicyObj)).To(Succeed())
			for k, v := range CiliumNetworkPolicyAnnotations {
				Expect(ciliumNetworkPolicyObj.ObjectMeta.Annotations[k]).To(Equal(v))
			}

			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)

			deleteService(serviceObj)
		})

		It("should set CiliumNetworkPolicy object \"ObjectMeta.Labels\" based on Service object \"ObjectMeta.Labels\" and upsert additional labels defined in CiliumNetworkPolicyAdditionalLabels variable", func() {
			serviceObj := getSampleService(ServiceName, DefaultNamespace)

			// This CiliumNetworkPolicy object is dummy since it's only used to bypass the 'NoPolicyRule' rule.
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: DummyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			Expect(k8sClient.Create(context.Background(), serviceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &corev1.Service{})).To(Succeed())

			ciliumNetworkPolicyObj := ciliumv2.CiliumNetworkPolicy{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumNetworkPolicyObj)).To(Succeed())
			Expect(ciliumNetworkPolicyObj.ObjectMeta.Labels).To(Equal(getCiliumNetworkPolicyLabels(serviceObj)))

			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)

			deleteService(serviceObj)
		})

		It("should add finalizer string to Service object after creating CiliumNetworkPolicy object", func() {
			desiredServiceObj := getSampleService(ServiceName, DefaultNamespace)
			currentServiceObj := corev1.Service{}

			// This CiliumNetworkPolicy object is dummy since it's only used to bypass the 'NoPolicyRule' rule.
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: DummyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			Expect(k8sClient.Create(context.Background(), desiredServiceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &currentServiceObj)).To(Succeed())
			Expect(currentServiceObj.ObjectMeta.Finalizers).To(ContainElement(ServiceFinalizerString))

			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)

			deleteService(desiredServiceObj)
		})

		It("should delete CiliumNetworkPolicy object when Service type is transitioned from LoadBalancer into ClusterIP", func() {
			desiredServiceObj := getSampleService(ServiceName, DefaultNamespace)
			currentServiceObj := corev1.Service{}

			// This CiliumNetworkPolicy object is dummy since it's only used to bypass the 'NoPolicyRule' rule.
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: DummyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			Expect(k8sClient.Create(context.Background(), desiredServiceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &currentServiceObj)).To(Succeed())
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			currentServiceObj.Spec.Type = corev1.ServiceTypeClusterIP
			Expect(k8sClient.Update(context.Background(), &currentServiceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)

			deleteService(desiredServiceObj)
		})

		It("should delete CiliumNetworkPolicy object when Service type is transitioned from LoadBalancer into NodePort", func() {
			desiredServiceObj := getSampleService(ServiceName, DefaultNamespace)
			currentServiceObj := corev1.Service{}

			// This CiliumNetworkPolicy object is dummy since it's only used to bypass the 'NoPolicyRule' rule.
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: DummyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			Expect(k8sClient.Create(context.Background(), desiredServiceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &currentServiceObj)).To(Succeed())
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			currentServiceObj.Spec.Type = corev1.ServiceTypeNodePort
			Expect(k8sClient.Update(context.Background(), &currentServiceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)

			deleteService(desiredServiceObj)
		})

		It("should delete CiliumNetworkPolicy object when Service type is transitioned from LoadBalancer into ExternalName", func() {
			desiredServiceObj := getSampleService(ServiceName, DefaultNamespace)
			currentServiceObj := corev1.Service{}

			// This CiliumNetworkPolicy object is dummy since it's only used to bypass the 'NoPolicyRule' rule.
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: DummyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			Expect(k8sClient.Create(context.Background(), desiredServiceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &currentServiceObj)).To(Succeed())
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			currentServiceObj.Spec.Type = corev1.ServiceTypeExternalName
			currentServiceObj.Spec.ExternalName = "snappcloud.io"

			Expect(k8sClient.Update(context.Background(), &currentServiceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)

			deleteService(desiredServiceObj)
		})

		It("should delete finalizer string from Service object after deleting CiliumNetworkPolicy object", func() {
			desiredServiceObj := getSampleService(ServiceName, DefaultNamespace)
			currentServiceObj := corev1.Service{}

			// This CiliumNetworkPolicy object is dummy since it's only used to bypass the 'NoPolicyRule' rule.
			dummyCiliumNetworkPolicyObj := getSampleCiliumNetworkPolicy(DummyName, DefaultNamespace)

			Expect(k8sClient.Create(context.Background(), dummyCiliumNetworkPolicyObj)).To(Succeed())

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: DummyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			Expect(k8sClient.Create(context.Background(), desiredServiceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &currentServiceObj)).To(Succeed())
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: CiliumNetworkPolicyName}, &ciliumv2.CiliumNetworkPolicy{})).To(Succeed())

			currentServiceObj.Spec.Type = corev1.ServiceTypeClusterIP
			Expect(k8sClient.Update(context.Background(), &currentServiceObj)).To(Succeed())

			time.Sleep(WaitInterval * time.Second)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: ServiceName}, &currentServiceObj)).To(Succeed())
			Expect(currentServiceObj.ObjectMeta.Finalizers).To(Not(ContainElement(ServiceFinalizerString)))

			deleteCiliumNetworkPolicy(dummyCiliumNetworkPolicyObj)

			deleteService(desiredServiceObj)
		})
	})
})
