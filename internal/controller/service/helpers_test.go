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
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// TestBuildCNP_LabelFilter verifies that the label filter functions correctly
// in accordance with Cilium's label rules. This test is based on the
// Cilium documentation: https://github.com/cilium/cilium/blob/v1.15.8/Documentation/operations/performance/scalability/identity-relevant-labels.rst
func TestBuildCNP_LabelFilter(t *testing.T) {

	tests := []struct {
		name    string
		service *corev1.Service
		given   map[string]string
		want    map[string]string
	}{

		{name: "ShouldNotBeFiltered",
			given: map[string]string{"app": "test", "stack": "backend"},
			want:  map[string]string{"k8s.app": "test", "k8s.stack": "backend"}},

		{name: "ShouldBeFiltered_statefulset.kubernetes.io/pod-name",
			given: map[string]string{"statefulset.kubernetes.io/pod-name": "test", "stack": "backend"},
			want:  map[string]string{"k8s.stack": "backend"}},

		{name: "ShouldBeFiltered_k8s.io",
			given: map[string]string{"k8s.io/fake": "test", "stack": "backend"},
			want:  map[string]string{"k8s.stack": "backend"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := &ReconcilerExtended{

				service: &corev1.Service{Spec: corev1.ServiceSpec{Selector: tt.given}},
			}
			if got := re.buildCNP().Spec.EndpointSelector.MatchLabels; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ReconcilerExtended.buildCNP() = %v, want %v", got, tt.want)
			}
		})
	}
}
