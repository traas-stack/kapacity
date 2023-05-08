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
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
)

var (
	observedGeneration int64 = 1
	horizontalPortrait       = &v1alpha1.HorizontalPortrait{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HorizontalPortrait",
			APIVersion: "autoscaling.kapacity.traas.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "hp",
		},
	}
)

func TestSetConditionInList_EmptyConditionList(t *testing.T) {
	inputList := make([]metav1.Condition, 0)
	successGeneratePortrait := "SucceededGeneratePortrait"
	conditionType := string(v1alpha1.PortraitGenerated)
	conditionStatus := metav1.ConditionTrue

	conditionList := SetConditionInList(inputList, conditionType, conditionStatus, observedGeneration, successGeneratePortrait, "")
	if len(conditionList) != 1 {
		t.Errorf("condition size unexpected, expetcted 1, actaul %d", len(conditionList))
	}

	assert.True(t, len(conditionList) == 1)

	condition := conditionList[0]
	assert.Equal(t, conditionType, condition.Type)
	assert.Equal(t, conditionStatus, condition.Status)
	assert.Equal(t, successGeneratePortrait, condition.Reason)
}

func TestSetConditionInList_ExistConditionType(t *testing.T) {
	conditionReason := "SucceededGeneratePortrait"
	conditionType := string(v1alpha1.PortraitGenerated)
	conditionStatus := metav1.ConditionTrue
	inputList := []metav1.Condition{
		{
			Type:   conditionType,
			Status: metav1.ConditionFalse,
		},
	}

	conditionList := SetConditionInList(inputList, conditionType, conditionStatus, observedGeneration, conditionReason, "")
	assert.Equal(t, 1, len(conditionList))

	condition := conditionList[0]
	assert.Equal(t, conditionType, condition.Type)
	assert.Equal(t, conditionStatus, condition.Status)
	assert.Equal(t, conditionReason, condition.Reason)
}

func TestParseGVK(t *testing.T) {
	groupVersion, err := ParseGVK(horizontalPortrait.APIVersion, horizontalPortrait.Kind)
	assert.Nil(t, err)
	assert.Equal(t, horizontalPortrait.GroupVersionKind(), groupVersion)
}

func TestBuildControllerOwnerRef(t *testing.T) {
	ownerRef := BuildControllerOwnerRef(horizontalPortrait)
	assert.Equal(t, horizontalPortrait.GroupVersionKind().Kind, ownerRef.Kind)
	assert.Equal(t, horizontalPortrait.APIVersion, ownerRef.APIVersion)
	assert.Equal(t, horizontalPortrait.ObjectMeta.Name, ownerRef.Name)
	assert.Equal(t, horizontalPortrait.ObjectMeta.UID, ownerRef.UID)
}

func TestParseScaleSelector(t *testing.T) {
	labelSelector := "key=value"
	ls, err := ParseScaleSelector(labelSelector)
	assert.Nil(t, err)
	assert.Equal(t, labelSelector, ls.String())
}
