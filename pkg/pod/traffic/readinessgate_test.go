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

package traffic

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestOn(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithObjects().Build()
	readinessGate := ReadinessGate{Client: fakeClient}

	pods := []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod-1",
				Labels: map[string]string{
					"foo": "bar",
				},
			},
			Spec:   corev1.PodSpec{},
			Status: corev1.PodStatus{},
		},
	}
	// create pod
	for _, pod := range pods {
		assert.Nil(t, fakeClient.Create(context.Background(), pod))
	}
	defer cleanPods(fakeClient, pods)

	err := readinessGate.On(context.Background(), pods)
	assert.Nil(t, err)

	podList := &corev1.PodList{}
	ls, _ := labels.Parse("foo=bar")
	assert.Nil(t, fakeClient.List(context.Background(), podList, &client.ListOptions{LabelSelector: ls}))

	for _, pod := range podList.Items {
		assert.NotNil(t, pod.Status, "pod status should not nil for %s", pod.Name)
		assert.True(t, len(pod.Status.Conditions) > 0, "conditions should not empty for %s", pod.Name)
		assert.True(t, hasExpectedTraffic(pod.Status.Conditions, corev1.ConditionTrue), "pod traffic not on for %s", pod.Name)
	}
}

func TestOff(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithObjects().Build()
	readinessGate := ReadinessGate{Client: fakeClient}

	pods := []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod-2",
				Labels: map[string]string{
					"foo": "bar",
				},
			},
			Spec:   corev1.PodSpec{},
			Status: corev1.PodStatus{},
		},
	}

	// create pod
	for _, pod := range pods {
		assert.Nil(t, fakeClient.Create(context.Background(), pod))
	}
	defer cleanPods(fakeClient, pods)

	err := readinessGate.Off(context.Background(), pods)
	assert.Nil(t, err)

	podList := &corev1.PodList{}
	ls, _ := labels.Parse("foo=bar")
	assert.Nil(t, fakeClient.List(context.Background(), podList, &client.ListOptions{LabelSelector: ls}))

	for _, pod := range podList.Items {
		assert.NotNil(t, pod.Status, "pod status should not nil for %s", pod.Name)
		assert.True(t, len(pod.Status.Conditions) > 0, "conditions should not empty for %s", pod.Name)
		assert.True(t, hasExpectedTraffic(pod.Status.Conditions, corev1.ConditionFalse), "pod traffic not off for %s", pod.Name)
	}
}

func hasExpectedTraffic(conditions []corev1.PodCondition, expectedStatus corev1.ConditionStatus) bool {
	for _, condition := range conditions {
		if condition.Type == ReadinessGateOnline {
			return condition.Status == expectedStatus
		}
	}

	return false
}

func cleanPods(c client.Client, pods []*corev1.Pod) {
	for _, pod := range pods {
		_ = c.Delete(context.Background(), pod)
	}
}
