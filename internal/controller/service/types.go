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

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// logicFunc is a function definition representing separated reconciliation logic.
type logicFunc func(context.Context) (*ctrl.Result, error)

// ReconcilerExtended extends the Reconciler struct with more fields to share between functions.
type ReconcilerExtended struct {
	client.Client
	cnp     *ciliumv2.CiliumNetworkPolicy
	logger  logr.Logger
	request *reconcile.Request
	scheme  *runtime.Scheme
	service *corev1.Service
}
