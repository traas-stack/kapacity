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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCtrlPodLister(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithObjects(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "ns1",
				Labels: map[string]string{
					"key1": "value1",
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod2",
				Namespace: "ns1",
				Labels: map[string]string{
					"key2": "value2",
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod3",
				Namespace: "ns2",
				Labels: map[string]string{
					"key1": "value1",
				},
			},
		},
	).Build()
	lister := NewCtrlPodLister(fakeClient)

	pods, err := lister.List(labels.SelectorFromSet(map[string]string{"key1": "value1"}))
	assert.Nil(t, err)
	assert.Equal(t, 2, len(pods))
	for _, pod := range pods {
		assert.NotEqual(t, "pod2", pod.Name)
	}

	pods, err = lister.Pods("ns1").List(labels.SelectorFromSet(map[string]string{"key1": "value1"}))
	assert.Nil(t, err)
	assert.Equal(t, 1, len(pods))
	assert.Equal(t, "pod1", pods[0].Name)
}
