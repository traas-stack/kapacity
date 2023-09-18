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
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewCtrlPodLister creates a corev1listers.PodLister wrapper for given controller-runtime client.
func NewCtrlPodLister(client client.Client) corev1listers.PodLister {
	return &ctrlPodLister{
		client: client,
	}
}

type ctrlPodLister struct {
	client client.Client
}

type ctrlPodNamespaceLister struct {
	client    client.Client
	namespace string
}

func (l *ctrlPodLister) List(selector labels.Selector) ([]*corev1.Pod, error) {
	podList := &corev1.PodList{}
	if err := l.client.List(context.TODO(), podList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, err
	}
	return convertPodListToPointerSlice(podList), nil
}

func (l *ctrlPodLister) Pods(namespace string) corev1listers.PodNamespaceLister {
	return &ctrlPodNamespaceLister{
		client:    l.client,
		namespace: namespace,
	}
}

func (l *ctrlPodNamespaceLister) List(selector labels.Selector) ([]*corev1.Pod, error) {
	podList := &corev1.PodList{}
	if err := l.client.List(context.TODO(), podList, client.InNamespace(l.namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, err
	}
	return convertPodListToPointerSlice(podList), nil
}

func (l *ctrlPodNamespaceLister) Get(name string) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	if err := l.client.Get(context.TODO(), types.NamespacedName{Namespace: l.namespace, Name: name}, pod); err != nil {
		return nil, err
	}
	return pod, nil
}

func convertPodListToPointerSlice(podList *corev1.PodList) []*corev1.Pod {
	s := make([]*corev1.Pod, len(podList.Items))
	for i := range podList.Items {
		s[i] = &podList.Items[i]
	}
	return s
}
