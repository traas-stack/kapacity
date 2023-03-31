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

package v1alpha1

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:shortName=rp

// ReplicaProfile controls the number and state of replicas of the target workload.
// It is used by IntelligentHorizontalPodAutoscaler to do actual workload scaling.
type ReplicaProfile struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ReplicaProfileSpec   `json:"spec,omitempty"`
	Status ReplicaProfileStatus `json:"status,omitempty"`
}

// ReplicaProfileSpec defines the desired state of ReplicaProfile.
type ReplicaProfileSpec struct {
	// ScaleTargetRef points to the target resource to scale.
	ScaleTargetRef autoscalingv2.CrossVersionObjectReference `json:"scaleTargetRef"`

	// OnlineReplicas is the desired number of online replicas.
	// +optional
	// +kubebuilder:validation:Minimum=0
	OnlineReplicas int32 `json:"onlineReplicas,omitempty"`

	// CutoffReplicas is the desired number of cutoff replicas.
	// +optional
	// +kubebuilder:validation:Minimum=0
	CutoffReplicas int32 `json:"cutoffReplicas,omitempty"`

	// StandbyReplicas is the desired number of standby replicas.
	// +optional
	// +kubebuilder:validation:Minimum=0
	StandbyReplicas int32 `json:"standbyReplicas,omitempty"`

	// Paused means if the replica control is paused.
	// +optional
	Paused bool `json:"paused,omitempty"`

	// Behavior configures the behavior of ReplicaProfile.
	// If not set, default behavior will be set.
	// +optional
	// +kubebuilder:default={podSorter:{type:"WorkloadDefault"},podTrafficController:{type:"ReadinessGate"}}
	Behavior ReplicaProfileBehavior `json:"behavior"`
}

// ReplicaProfileBehavior defines the behavior of ReplicaProfile.
type ReplicaProfileBehavior struct {
	// PodSorter is used to decide the priority of pods when scaling.
	// If not set, default pod sorter will be set to WorkloadDefault.
	// +optional
	// +kubebuilder:default={type:"WorkloadDefault"}
	PodSorter PodSorter `json:"podSorter"`

	// PodTrafficController is used to control pod traffic when scaling.
	// If not set, default pod traffic controller will be set to ReadinessGate.
	// +optional
	// +kubebuilder:default={type:"ReadinessGate"}
	PodTrafficController PodTrafficController `json:"podTrafficController"`
}

// PodSorter is used to decide the priority of pods when scaling.
type PodSorter struct {
	// Type is the type of pod sorter.
	// It defaults to WorkloadDefault.
	// +optional
	// +kubebuilder:validation:Enum=WorkloadDefault;External
	// +kubebuilder:default=WorkloadDefault
	Type PodSorterType `json:"type"`

	// External refers to a user specified external pod sorter.
	// +optional
	External *ExternalPodSorter `json:"external,omitempty"`
}

type PodSorterType string

const (
	// WorkloadDefaultPodSorterType is the default pod sorter of target workload.
	WorkloadDefaultPodSorterType PodSorterType = "WorkloadDefault"

	// ExternalPodSorterType is the external pod sorter specified by user.
	ExternalPodSorterType PodSorterType = "External"
)

// ExternalPodSorter defines the user specified external pod sorter.
type ExternalPodSorter struct {
	// Name is the name of the sorter.
	// It must be unique across all external pod sorters of the ReplicaProfile.
	Name string `json:"name"`

	// Config is used to pass arbitrary config data to the sorter.
	// +optional
	Config map[string]string `json:"config,omitempty"`
}

// PodTrafficController is used to control pod traffic when scaling.
type PodTrafficController struct {
	// Type is the type of pod traffic controller.
	// It defaults to ReadinessGate.
	// +optional
	// +kubebuilder:validation:Enum=ReadinessGate;External
	// +kubebuilder:default=ReadinessGate
	Type PodTrafficControllerType `json:"type"`

	// External refers to a user specified external pod traffic controller.
	// +optional
	External *ExternalPodTrafficController `json:"external,omitempty"`
}

type PodTrafficControllerType string

const (
	// ReadinessGatePodTrafficControllerType controls pod traffic by setting its readiness gate.
	ReadinessGatePodTrafficControllerType PodTrafficControllerType = "ReadinessGate"

	// ExternalPodTrafficControllerType controls pod traffic by the external controller specified by user.
	ExternalPodTrafficControllerType PodTrafficControllerType = "External"
)

// ExternalPodTrafficController defines the user specified external pod traffic controller.
type ExternalPodTrafficController struct {
	// Name is the name of the controller.
	// It must be unique across all external pod traffic controllers of the ReplicaProfile.
	Name string `json:"name"`

	// Config is used to pass arbitrary config to the controller.
	// +optional
	Config map[string]string `json:"config,omitempty"`
}

// ReplicaProfileStatus defines the observed state of ReplicaProfile.
type ReplicaProfileStatus struct {
	// OnlineReplicas is the current number of online replicas.
	// +optional
	// +kubebuilder:validation:Minimum=0
	OnlineReplicas int32 `json:"onlineReplicas,omitempty"`

	// CutoffReplicas is the current number of cutoff replicas.
	// +optional
	// +kubebuilder:validation:Minimum=0
	CutoffReplicas int32 `json:"cutoffReplicas,omitempty"`

	// StandbyReplicas is the current number of standby replicas.
	// +optional
	// +kubebuilder:validation:Minimum=0
	StandbyReplicas int32 `json:"standbyReplicas,omitempty"`

	// Conditions represents current conditions of the ReplicaProfile.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type ReplicaProfileConditionType string

const (
	// ReplicaProfileApplied means all the operations for ensuring the replica profile are applied.
	ReplicaProfileApplied ReplicaProfileConditionType = "Applied"

	// ReplicaProfileEnsured means the replica profile is ensured.
	ReplicaProfileEnsured ReplicaProfileConditionType = "Ensured"
)

//+kubebuilder:object:root=true

// ReplicaProfileList contains a list of ReplicaProfile
type ReplicaProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ReplicaProfile `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ReplicaProfile{}, &ReplicaProfileList{})
}
