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
	"encoding/json"

	k8sautoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:shortName=hp

// HorizontalPortrait represents a horizontal portrait (the expectation of replicas) of a scale target
// with metrics and algorithm configurations that are used to generate the portrait.
type HorizontalPortrait struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HorizontalPortraitSpec   `json:"spec,omitempty"`
	Status HorizontalPortraitStatus `json:"status,omitempty"`
}

// HorizontalPortraitSpec defines the desired state of HorizontalPortrait.
type HorizontalPortraitSpec struct {
	// ScaleTargetRef points to the target resource to scale.
	ScaleTargetRef k8sautoscalingv2.CrossVersionObjectReference `json:"scaleTargetRef"`

	PortraitSpec `json:",inline"`
}

// PortraitSpec defines general specs of portrait.
type PortraitSpec struct {
	// PortraitType is the type of portrait. Different type has different portrait generating logic.
	PortraitType PortraitType `json:"portraitType"`

	// Metrics contains the specifications for which to use to generate the portrait.
	// +optional
	Metrics []MetricSpec `json:"metrics,omitempty"`

	// Algorithm is the algorithm for which to use to generate the portrait.
	Algorithm PortraitAlgorithm `json:"algorithm"`
}

type PortraitType string

const (
	// ReactivePortraitType generates portrait reactively based on latest metrics.
	ReactivePortraitType PortraitType = "Reactive"

	// PredictivePortraitType generates portrait by predicting the future based on historical metrics.
	PredictivePortraitType PortraitType = "Predictive"

	// TODO
	BurstPortraitType PortraitType = "Burst"
)

// MetricSpec represents the configuration for a single metric.
// It is an extended autoscalingv2.MetricSpec.
type MetricSpec struct {
	k8sautoscalingv2.MetricSpec `json:",inline"`

	// Name is the unique identifier of this metric spec.
	// It must be unique across all metric specs if specified.
	// +optional
	Name string `json:"name,omitempty"`

	// Operator is an optional binary arithmetic operator which is used to specify
	// a custom comparison rule "<actual> <operator> <target>" for the metric.
	// Note that not all use cases support this.
	// +optional
	// +kubebuilder:validation:Enum===;>;<;>=;<=
	Operator Operator `json:"operator,omitempty"`
}

type Operator string

const (
	// Equals means that two numbers are equal.
	Equals Operator = "=="

	// GreaterThan means that left number is greater than right one.
	GreaterThan Operator = ">"

	// LessThan means that left number is less than right one.
	LessThan Operator = "<"

	// GreaterThanOrEquals means that left number is greater than or equal to right one.
	GreaterThanOrEquals Operator = ">="

	// LessThanOrEquals means that left number is less than or equal to right one.
	LessThanOrEquals Operator = "<="
)

// PortraitAlgorithm represents the configuration for a specific type of algorithm used for portrait generating.
type PortraitAlgorithm struct {
	// Type is the type of algorithm.
	Type PortraitAlgorithmType `json:"type"`

	// KubeHPA is the configuration for KubeHPA algorithm.
	// +optional
	KubeHPA *KubeHPAPortraitAlgorithm `json:"kubeHPA,omitempty"`

	// Config is the general configuration data for arbitrary algorithm those
	// used by external user-defined portraits.
	// TODO: consider if we can make it structural
	// +optional
	Config map[string]string `json:"config,omitempty"`
}

type PortraitAlgorithmType string

const (
	// KubeHPAPortraitAlgorithmType is the Kubernetes HPA algorithm.
	KubeHPAPortraitAlgorithmType PortraitAlgorithmType = "KubeHPA"
)

// KubeHPAPortraitAlgorithm defines parameters of KubeHPA algorithm.
type KubeHPAPortraitAlgorithm struct {
	// SyncPeriod is the period for syncing the portrait.
	// +optional
	// +kubebuilder:default="15s"
	SyncPeriod metav1.Duration `json:"syncPeriod"`

	// Tolerance is the tolerance for when resource usage suggests upscaling/downscaling.
	// Should be a string formatted float64 number.
	// +optional
	// +kubebuilder:default="0.1"
	Tolerance json.Number `json:"tolerance"`

	// CPUInitializationPeriod is the period after pod start when CPU samples might be skipped.
	// +optional
	// +kubebuilder:default="5m"
	CPUInitializationPeriod metav1.Duration `json:"cpuInitializationPeriod"`

	// InitialReadinessDelay is period after pod start during which readiness changes
	// are treated as readiness being set for the first time. The only effect of this is that
	// HPA will disregard CPU samples from unready pods that had last readiness change during that period.
	// +optional
	// +kubebuilder:default="30s"
	InitialReadinessDelay metav1.Duration `json:"initialReadinessDelay"`
}

// HorizontalPortraitStatus defines the observed state of HorizontalPortrait.
type HorizontalPortraitStatus struct {
	// PortraitData is the data of generated portrait.
	// +optional
	PortraitData *HorizontalPortraitData `json:"portraitData,omitempty"`

	// Conditions represents current conditions of the HorizontalPortrait.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// HorizontalPortraitData represents the data of the portrait produced by algorithms.
type HorizontalPortraitData struct {
	// Type is the type of this portrait data.
	// +kubebuilder:validation:Enum=Static;Cron;TimeSeries
	Type HorizontalPortraitDataType `json:"type"`

	// Static refers to static portrait data.
	// +optional
	Static *StaticHorizontalPortraitData `json:"static,omitempty"`

	// Cron refers to cron portrait data.
	// +optional
	Cron *CronHorizontalPortraitData `json:"cron,omitempty"`

	// TimeSeries refers to time series portrait data.
	// +optional
	TimeSeries *TimeSeriesHorizontalPortraitData `json:"timeSeries,omitempty"`

	// ExpireTime indicates when this portrait data will expire.
	// +optional
	ExpireTime *metav1.Time `json:"expireTime,omitempty"`
}

type HorizontalPortraitDataType string

const (
	// StaticHorizontalPortraitDataType is data with a static replicas value.
	StaticHorizontalPortraitDataType HorizontalPortraitDataType = "Static"

	// CronHorizontalPortraitDataType is data with cron based replicas values.
	CronHorizontalPortraitDataType HorizontalPortraitDataType = "Cron"

	// TimeSeriesHorizontalPortraitDataType is data with time series based replicas values.
	TimeSeriesHorizontalPortraitDataType HorizontalPortraitDataType = "TimeSeries"
)

// StaticHorizontalPortraitData defines the static portrait data.
type StaticHorizontalPortraitData struct {
	// Replicas is the desired number of online replicas.
	// +kubebuilder:validation:Minimum=0
	Replicas int32 `json:"replicas"`
}

// CronHorizontalPortraitData defines the cron portrait data.
type CronHorizontalPortraitData struct {
	// Crons contains cron rule based replicas values.
	// +kubebuilder:validation:MinItems=1
	Crons []ReplicaCron `json:"crons"`
}

// TimeSeriesHorizontalPortraitData defines the time series portrait data.
type TimeSeriesHorizontalPortraitData struct {
	// TimeSeries is time series of replicas.
	// The points of the series MUST be ordered by time.
	// The specified replicas would take effect from that point to the next point.
	// Thus, the last point could last forever until the whole portrait data become expired.
	// +kubebuilder:validation:MinItems=1
	TimeSeries []ReplicaTimeSeriesPoint `json:"timeSeries"`
}

// ReplicaTimeSeriesPoint represents a specific point of the time series of replicas.
type ReplicaTimeSeriesPoint struct {
	// Timestamp is the unix time stamp of this point.
	// +kubebuilder:validation:Minimum=0
	Timestamp int64 `json:"timestamp"`

	// Replicas is the desired number of online replicas from this point.
	// +kubebuilder:validation:Minimum=0
	Replicas int32 `json:"replicas"`
}

type HorizontalPortraitConditionType string

const (
	// PortraitGenerated means the portrait has been successfully generated and populated to the status.
	PortraitGenerated HorizontalPortraitConditionType = "PortraitGenerated"
)

//+kubebuilder:object:root=true

// HorizontalPortraitList contains a list of HorizontalPortrait.
type HorizontalPortraitList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HorizontalPortrait `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HorizontalPortrait{}, &HorizontalPortraitList{})
}
