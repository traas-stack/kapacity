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

package mutating

import (
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	podtraffic "github.com/traas-stack/kapacity/pkg/pod/traffic"
	"github.com/traas-stack/kapacity/pkg/util"
)

const (
	labelInjectPodReadinessGate = "kapacitystack.io/inject-pod-readiness-gate"
)

func injectReadinessGate(req admission.Request, pod *corev1.Pod) (changed bool) {
	if req.Operation != admissionv1.Create || req.SubResource != "" {
		return false
	}
	if pod.Labels[labelInjectPodReadinessGate] != "true" {
		return false
	}

	return util.AddPodReadinessGate(&pod.Spec, podtraffic.ReadinessGateOnline)
}

func defaultReadinessGateStatus(req admission.Request, pod *corev1.Pod) (changed bool) {
	if req.Operation != admissionv1.Update || req.SubResource != "status" {
		return false
	}
	if pod.Labels[labelInjectPodReadinessGate] != "true" {
		return false
	}

	return util.AddPodCondition(&pod.Status, &corev1.PodCondition{
		Type:   podtraffic.ReadinessGateOnline,
		Status: corev1.ConditionTrue,
	})
}
