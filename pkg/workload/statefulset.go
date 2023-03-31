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
	"fmt"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// StatefulSet represents behaviors of a Kubernetes StatefulSet.
type StatefulSet struct{}

func (*StatefulSet) Sort(_ context.Context, pods []*corev1.Pod) ([]*corev1.Pod, error) {
	sort.Slice(pods, func(i, j int) bool {
		return getStatefulSetPodIndex(pods[i]) > getStatefulSetPodIndex(pods[j])
	})
	return pods, nil
}

func (*StatefulSet) CanSelectPodsToScaleDown(context.Context) bool {
	return false
}

func (*StatefulSet) SelectPodsToScaleDown(context.Context, []*corev1.Pod) error {
	return fmt.Errorf("StatefulSet does not support this operation")
}

func getStatefulSetPodIndex(pod *corev1.Pod) int {
	n, _ := strconv.Atoi(pod.Name[strings.LastIndex(pod.Name, "-")+1:])
	return n
}
