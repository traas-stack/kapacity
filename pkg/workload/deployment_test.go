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

package workload

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeployment_Sort(t *testing.T) {
	// impl it
}

func TestDeployment_CanSelectPodsToScaleDown(t *testing.T) {
	deployment := Deployment{}
	result := deployment.CanSelectPodsToScaleDown(context.Background())
	assert.False(t, result, "can't select pods to scale down for deployment resource")
}

func TestDeployment_SelectPodsToScaleDown(t *testing.T) {
	pods := []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod-1",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod-2",
			},
		},
	}

	deployment := Deployment{}
	err := deployment.SelectPodsToScaleDown(context.Background(), pods)
	assert.NotNil(t, err, "does not support select pods for deployment resource")
}
