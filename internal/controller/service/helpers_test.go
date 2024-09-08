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

	v1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/apis/meta/v1"
	cilium_labels "github.com/cilium/cilium/pkg/labels"
	"github.com/cilium/cilium/pkg/policy/api"
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
		wantErr bool
	}{

		{name: "ShouldNotBeFiltered",
			given:   map[string]string{"app": "test", "stack": "backend"},
			want:    map[string]string{"k8s.app": "test", "k8s.stack": "backend"},
			wantErr: false,
		},

		{name: "ShouldBeFiltered_statefulset.kubernetes.io/pod-name",
			given:   map[string]string{"statefulset.kubernetes.io/pod-name": "test", "stack": "backend"},
			want:    map[string]string{"k8s.stack": "backend"},
			wantErr: false,
		},

		{name: "ShouldBeEmptyReusltWithError",
			given:   map[string]string{"statefulset.kubernetes.io/pod-name": "test"},
			want:    map[string]string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := &ReconcilerExtended{
				//cnp:     tt.fields.cnp,
				service: &corev1.Service{Spec: corev1.ServiceSpec{Selector: tt.given}},
			}
			gotCnp, err := re.buildCNP()
			if (err != nil) != tt.wantErr {
				t.Errorf("ReconcilerExtended.buildCNP() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil && !reflect.DeepEqual(gotCnp.Spec.EndpointSelector.MatchLabels, tt.want) {
				t.Errorf("ReconcilerExtended.buildCNP() = %v, want %v", gotCnp.Spec.EndpointSelector.MatchLabels, tt.want)
			}
		})
	}

}

func Test_getEndponitSelector(t *testing.T) {

	tests := []struct {
		name  string
		given map[string]string
		want  map[string]string

		wantErr bool
	}{
		{name: "ShouldNotBeFiltered",
			given:   map[string]string{"app": "test", "stack": "backend"},
			want:    map[string]string{"app": "test", "stack": "backend"},
			wantErr: false,
		},

		{name: "ShouldBeFiltered",
			given:   map[string]string{"statefulset.kubernetes.io/pod-name": "test", "stack": "backend"},
			want:    map[string]string{"stack": "backend"},
			wantErr: false,
		},
		{name: "ShouldBeEmptyReusltWithError",
			given:   map[string]string{"statefulset.kubernetes.io/pod-name": "test"},
			want:    map[string]string{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := &ReconcilerExtended{}
			gotCiliumEndPointSelector, err := re.getEndponitSelector(tt.given)
			if (err != nil) != tt.wantErr {
				t.Errorf("getEndponitSelector() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			epSelector := api.NewESFromK8sLabelSelector(cilium_labels.LabelSourceK8sKeyPrefix, &v1.LabelSelector{MatchLabels: tt.want})
			if !reflect.DeepEqual(gotCiliumEndPointSelector, epSelector) {
				t.Errorf("getEndponitSelector() = %v, want %v", gotCiliumEndPointSelector, epSelector)
			}
		})
	}
}
