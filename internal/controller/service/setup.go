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
	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// NewReconcilerExtended instantiate a new ReconcilerExtended struct and returns it.
func NewReconcilerExtended(mgr manager.Manager) *ReconcilerExtended {
	return &ReconcilerExtended{
		Client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
	}
}

// SetupWithManager sets up the controller with the manager.
func (re *ReconcilerExtended) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Owns(&ciliumv2.CiliumNetworkPolicy{}).
		Complete(re)
}
