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

package utils

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IsDeleted checks if DeletionTimestamp field is set on the object.
func IsDeleted(obj metav1.Object) bool {
	return !obj.GetDeletionTimestamp().IsZero()
}

// IsMapSubset checks if a map is subset of another map.
func IsMapSubset[K, V comparable](set, subset map[K]V) bool {
	if len(subset) > len(set) {
		return false
	}

	for k, vsub := range subset {
		if vm, found := set[k]; !found || vm != vsub {
			return false
		}
	}

	return true
}

func OverWriteAnnotations(objectMeta *metav1.ObjectMeta, annotations map[string]string) {
	objectMeta.Annotations = annotations
}

func OverWriteLabels(objectMeta *metav1.ObjectMeta, labels map[string]string) {
	objectMeta.Labels = labels
}
