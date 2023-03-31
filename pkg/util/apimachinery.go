/*
 Copyright 2023 The Kapacity Authors.

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

package util

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SetConditionInList sets the specific condition type on the given condition list to the specified value with the given reason and message.
// The condition will be added if it is not present. The new list will be returned.
func SetConditionInList(inputList []metav1.Condition, conditionType string, status metav1.ConditionStatus, observedGeneration int64, reason, message string) []metav1.Condition {
	resList := inputList
	var existingCond *metav1.Condition
	for i, condition := range resList {
		if condition.Type == conditionType {
			existingCond = &resList[i]
			break
		}
	}

	if existingCond == nil {
		resList = append(resList, metav1.Condition{
			Type: conditionType,
		})
		existingCond = &resList[len(resList)-1]
	}

	if existingCond.Status != status {
		existingCond.LastTransitionTime = metav1.Now()
	}

	existingCond.Status = status
	existingCond.ObservedGeneration = observedGeneration
	existingCond.Reason = reason
	existingCond.Message = message

	return resList
}

// ParseGVK parses the given api version and kind to schema.GroupVersionKind.
func ParseGVK(apiVersion, kind string) (schema.GroupVersionKind, error) {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return schema.GroupVersionKind{}, err
	}
	return gv.WithKind(kind), nil
}

// BuildControllerOwnerRef builds a controller owner reference to the given Kubernetes object.
func BuildControllerOwnerRef(obj client.Object) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: obj.GetObjectKind().GroupVersionKind().GroupVersion().String(),
		Kind:       obj.GetObjectKind().GroupVersionKind().Kind,
		Name:       obj.GetName(),
		UID:        obj.GetUID(),
		Controller: pointer.Bool(true),
	}
}

// ParseScaleSelector parses the selector string got from Kubernetes scale API to labels.Selector.
func ParseScaleSelector(selector string) (labels.Selector, error) {
	if selector == "" {
		return nil, fmt.Errorf("selector should not be empty")
	}
	parsedSelector, err := labels.Parse(selector)
	if err != nil {
		return nil, fmt.Errorf("failed to convert selector into a corresponding internal selector object: %v", err)
	}
	return parsedSelector, nil
}
