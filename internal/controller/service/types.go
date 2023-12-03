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
	"sync"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type eventType string

const (
	createEvent  eventType = "CREATE"
	updateEvent  eventType = "UPDATE"
	deleteEvent  eventType = "DELETE"
	genericEvent eventType = "GENERIC"
)

type namespaceExclusionReason string

const (
	namespaceLabels                         namespaceExclusionReason = "NamespaceLabels"
	namespaceName                           namespaceExclusionReason = "NamespaceName"
	noNetworkPolicyExistence                namespaceExclusionReason = "NoNetworkPolicyExistence"
	noUnmanagedCiliumNetworkPolicyExistence namespaceExclusionReason = "NoUnmanagedCiliumNetworkPolicyExistence"
)

// empty is a struct type defined without any fields.
// In Go, it is frequently utilized to emulate set-like behavior in maps,
// especially when only keys are of interest and values are irrelevant.
// The zero-size characteristic of this struct ensures that it consumes minimal memory,
// making it an efficient choice for such implementations.
type empty struct{}

// logicFunc is a function definition representing separated reconciliation logic.
type logicFunc func(context.Context) (*ctrl.Result, error)

// customEventHandlerFunc is a function definition representing different handlers.
type customEventHandlerFunc func(context.Context, client.Object, client.Object, eventType) []ctrl.Request

// ReconcilerExtended extends the Reconciler struct with more fields to share between functions.
type ReconcilerExtended struct {
	cacheLock *sync.Mutex
	client.Client
	cnp                     *ciliumv2.CiliumNetworkPolicy
	logger                  logr.Logger
	namespaceExclusionCache map[string]map[namespaceExclusionReason]empty
	request                 *reconcile.Request
	scheme                  *runtime.Scheme
	service                 *corev1.Service
}

// customEventHandler is a struct that implements the handler.EventHandler interface.
// It is specifically implemented to manage certain types of events using the 'customHandlerFunc' function type.
// This is essential because it deals with 'event types' crucial in the 'namespaceExclusionCache' handling logic and,
// such event types are abstracted away by standard handlers.
type customEventHandler struct {
	handler customEventHandlerFunc
}
