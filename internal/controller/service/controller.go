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
	"reflect"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/snapp-incubator/svc-lb-to-cilium-netpolicy/internal/consts"
)

// TODO: reduce rbac verbs as much as possible
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;update
//+kubebuilder:rbac:groups=core,resources=services/finalizers,verbs=update
//+kubebuilder:rbac:groups=cilium.io,resources=ciliumnetworkpolicies,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// It instantiate a new ReconcilerExtended struct and start the reconciliation flow.
func (re *ReconcilerExtended) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("reconcile loop")

	re.cnp = nil
	re.logger = logger
	re.request = &req
	re.service = &corev1.Service{}

	result, err := re.manageLogic(ctx)

	return *result, err
}

// manageLogic manages the reconciliation logics and flow.
func (re *ReconcilerExtended) manageLogic(ctx context.Context) (*ctrl.Result, error) {
	var logicFuncs []logicFunc

	var shouldExcludeNS bool

	err := re.Client.Get(ctx, re.request.NamespacedName, re.service)

	if client.IgnoreNotFound(err) != nil {
		return &ctrl.Result{Requeue: true}, fmt.Errorf(consts.ServiceGetError, err)
	} else if err != nil {
		// Service object not found.
		// Return and don't requeue.
		re.logger.Info(consts.ServiceNotFoundInfo)

		//nolint:nilerr
		return &ctrl.Result{Requeue: false}, nil
	}

	re.logger = re.logger.WithValues("namespace", re.request.Namespace)

	re.cacheLock.Lock()

	// CiliumNetworkPolicy objects neither should be created nor exist in some namespaces.
	// Therefore, specific namespaces are excluded by some configurations.
	// shouldExcludeNamespace determine the namespace exclusion status based on the configurations defined.
	if re.namespaceExclusionCache[re.request.Namespace] == nil {
		re.logger.Info("The namespace exclusion cache entry is not set")

		err := re.determineExclusionReasons(ctx)
		if err != nil {
			return &ctrl.Result{Requeue: true}, fmt.Errorf(consts.NamespaceExclusionStateError, err)
		}
	}

	exclusionReasons := re.namespaceExclusionCache[re.request.Namespace]

	re.cacheLock.Unlock()

	shouldExcludeNS = determineExclusionStatus(exclusionReasons)

	re.logger = re.logger.WithValues("namespaceExcluded", shouldExcludeNS)

	logicFuncs = append(logicFuncs, re.findCNPByOwner)

	if re.shouldDeleteCNP() || shouldExcludeNS {
		logicFuncs = append(logicFuncs,
			re.handleCNPDelete,
			re.removeFinalizer)
	} else {
		logicFuncs = append(logicFuncs,
			re.handleCNPUpdateOrCreate,
			re.addFinalizer)
	}

	// logicFuncs execution steps:
	// 1. Call each logicFunc and evaluate its result and error.
	// 2. If both the result and the error are nil, call the next logicFunc (Go to step 1).
	// 3. If the result or the error is not nil, return the result and err.
	for _, logicFunc := range logicFuncs {
		result, err := logicFunc(ctx)
		if (result != nil) || (err != nil) {
			return result, err
		}
	}

	return &ctrl.Result{Requeue: false}, nil
}

// findCNPByOwner tries to find the CiliumNetworkPolicy object owned by the Service object using OwnerReference.
func (re *ReconcilerExtended) findCNPByOwner(ctx context.Context) (*ctrl.Result, error) {
	ciliumNetworkPolicyList := ciliumv2.CiliumNetworkPolicyList{}

	if err := re.Client.List(ctx, &ciliumNetworkPolicyList, client.InNamespace(re.service.Namespace)); err != nil {
		return &ctrl.Result{Requeue: true}, fmt.Errorf(consts.CiliumNetworkPolicyListError, err)
	}

	for _, ciliumNetworkPolicy := range ciliumNetworkPolicyList.Items {
		for _, ownerRef := range ciliumNetworkPolicy.GetOwnerReferences() {
			if ownerRef.UID == re.service.UID {
				//nolint:gosec
				re.cnp = &ciliumNetworkPolicy

				return nil, nil
			}
		}
	}

	if re.isServiceLoadBalancer() {
		re.logger.Info(consts.CiliumNetworkPolicyFindError)
	}

	return nil, nil
}

// handleCNPUpdateOrCreate handles the creation or update of the CiliumNetworkPolicy object if the Service type is "LoadBalancer".
func (re *ReconcilerExtended) handleCNPUpdateOrCreate(ctx context.Context) (*ctrl.Result, error) {
	var result *ctrl.Result

	var err error

	// Do not create or update the CiliumNetworkPolicy object if the service type is not "LoadBalancer".
	if !re.isServiceLoadBalancer() {
		re.logger.Info(consts.ServiceTypeNotLoadBalancerInfo)

		return &ctrl.Result{Requeue: false}, nil
	}

	if re.isCNPFound() {
		result, err = re.updateCNP(ctx)
	} else {
		result, err = re.createCNP(ctx)
	}

	return result, err
}

// handleCNPDelete handles the deletion of the CiliumNetworkPolicy object.
func (re *ReconcilerExtended) handleCNPDelete(ctx context.Context) (*ctrl.Result, error) {
	if !re.isCNPFound() {
		re.logger.Info(consts.CiliumNetworkPolicyDeleteSkipInfo)

		return nil, nil
	}

	if err := re.Client.Delete(ctx, re.cnp); err != nil {
		return &ctrl.Result{Requeue: true}, fmt.Errorf(consts.CiliumNetworkPolicyDeleteError, err)
	}

	return nil, nil
}

// updateCNP updates the current CiliumNetworkPolicy object to the desired CiliumNetworkPolicy object.
func (re *ReconcilerExtended) updateCNP(ctx context.Context) (*ctrl.Result, error) {
	var shouldUpdate bool

	desiredCNP, err := re.buildCNP()
	if err != nil {
		re.logger.Info("failed to build cnp", "buildCNP", err)

		return &ctrl.Result{}, nil
	}

	// Check if CiliumNetworkPolicy object spec was changed, if so set as desired.
	if !reflect.DeepEqual(re.cnp.Spec.DeepCopy(), desiredCNP.Spec) {
		desiredCNP.Spec.DeepCopyInto(re.cnp.Spec)

		shouldUpdate = true
	}
	// Check if CiliumNetworkPolicy object labels map was changed, if so set as desired.
	if !reflect.DeepEqual(re.cnp.ObjectMeta.Labels, desiredCNP.ObjectMeta.Labels) {
		re.cnp.ObjectMeta.Labels = desiredCNP.ObjectMeta.Labels

		shouldUpdate = true
	}
	// Check if CiliumNetworkPolicy object annotations map was changed, if so set as desired.
	if !reflect.DeepEqual(re.cnp.ObjectMeta.Annotations, desiredCNP.ObjectMeta.Annotations) {
		re.cnp.ObjectMeta.Annotations = desiredCNP.ObjectMeta.Annotations

		shouldUpdate = true
	}

	if shouldUpdate {
		if err := re.Client.Update(ctx, re.cnp); err != nil {
			return &ctrl.Result{Requeue: true}, fmt.Errorf(consts.CiliumNetworkPolicyUpdateError, err)
		}
	} else {
		re.logger.Info(consts.CiliumNetworkPolicyUpdateSkipInfo)
	}

	return nil, nil
}

// createCNP creates the desired CiliumNetworkPolicy object.
func (re *ReconcilerExtended) createCNP(ctx context.Context) (*ctrl.Result, error) {
	desiredCNP, err := re.buildCNP()
	if err != nil {
		re.logger.Info("failed to build cnp", "buildCNP", err)

		return &ctrl.Result{}, nil
	}

	if err := controllerutil.SetControllerReference(re.service, desiredCNP, re.scheme); err != nil {
		return &ctrl.Result{Requeue: true}, fmt.Errorf(consts.ControllerReferenceSetError, err)
	}

	if err := re.Client.Create(ctx, desiredCNP); err != nil {
		return &ctrl.Result{Requeue: true}, fmt.Errorf(consts.CiliumNetworkPolicyCreateError, err)
	}

	return nil, nil
}

// addFinalizer adds the finalizer string to the Service object and update it if the object's list of finalizers is updated.
func (re *ReconcilerExtended) addFinalizer(ctx context.Context) (*ctrl.Result, error) {
	err := retry.RetryOnConflict(retry.DefaultBackoff,
		func() error {
			if err := re.Client.Get(ctx, re.request.NamespacedName, re.service); err != nil {
				return err
			}

			if controllerutil.ContainsFinalizer(re.service, consts.ServiceFinalizerString) {
				return nil
			}

			controllerutil.AddFinalizer(re.service, consts.ServiceFinalizerString)

			return re.Client.Update(ctx, re.service)
		},
	)

	if err != nil {
		return &ctrl.Result{Requeue: true}, fmt.Errorf(consts.ServiceAddFinalizerError, err)
	}

	return nil, nil
}

// removeFinalizer remove the finalizer string from the Service object and update it if the object's list of finalizers is updated.
func (re *ReconcilerExtended) removeFinalizer(ctx context.Context) (*ctrl.Result, error) {
	err := retry.RetryOnConflict(retry.DefaultBackoff,
		func() error {
			if err := re.Client.Get(ctx, re.request.NamespacedName, re.service); err != nil {
				return err
			}

			if !controllerutil.ContainsFinalizer(re.service, consts.ServiceFinalizerString) {
				return nil
			}

			controllerutil.RemoveFinalizer(re.service, consts.ServiceFinalizerString)

			return re.Client.Update(ctx, re.service)
		},
	)

	if err != nil {
		return &ctrl.Result{Requeue: true}, fmt.Errorf(consts.ServiceRemoveFinalizerError, err)
	}

	return nil, nil
}
