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
	k8sautoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:shortName=ihpa

// IntelligentHorizontalPodAutoscaler is the configuration for an intelligent horizontal pod autoscaler,
// which automatically manages the replica count of the target workload
// based on the horizontal portraits provided by specified portrait providers.
type IntelligentHorizontalPodAutoscaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IntelligentHorizontalPodAutoscalerSpec   `json:"spec,omitempty"`
	Status IntelligentHorizontalPodAutoscalerStatus `json:"status,omitempty"`
}

// IntelligentHorizontalPodAutoscalerSpec defines the desired state of IntelligentHorizontalPodAutoscaler.
type IntelligentHorizontalPodAutoscalerSpec struct {
	// ScaleTargetRef points to the target resource to scale.
	ScaleTargetRef k8sautoscalingv2.CrossVersionObjectReference `json:"scaleTargetRef"`

	// Paused means if the autoscaler is paused.
	// +optional
	Paused bool `json:"paused,omitempty"`

	// MinReplicas is the lower limit for the number of replicas to which the autoscaler can scale down.
	// +kubebuilder:validation:Minimum=1
	MinReplicas int32 `json:"minReplicas"`

	// MaxReplicas is the upper limit for the number of replicas to which the autoscaler can scale up.
	// It cannot be less than MinReplicas.
	// +kubebuilder:validation:Minimum=1
	MaxReplicas int32 `json:"maxReplicas"`

	// ScaleMode is the scaling mode of the autoscaler.
	// +optional
	// +kubebuilder:validation:Enum=Auto;Preview
	// +kubebuilder:default=Auto
	ScaleMode ScaleMode `json:"scaleMode"`

	// PortraitProviders contains configurations of portrait providers
	// that are used to generate horizontal portraits for the scale target.
	PortraitProviders []HorizontalPortraitProvider `json:"portraitProviders"`

	// Behavior configures the scaling behavior of the autoscaler.
	// +optional
	Behavior IntelligentHorizontalPodAutoscalerBehavior `json:"behavior,omitempty"`

	// StabilityCheckers contains configurations of stability checkers
	// that are used to ensure stability during autoscaling.
	// TODO: reconsider the place of this field
	// +optional
	StabilityCheckers []StabilityChecker `json:"stabilityCheckers,omitempty"`
}
type ScaleMode string

const (
	// ScaleModeAuto automatically scales the target resource based on its horizontal portraits.
	ScaleModeAuto ScaleMode = "Auto"

	// ScaleModePreview generates horizontal portraits without actually scaling the target resource.
	ScaleModePreview ScaleMode = "Preview"
)

// HorizontalPortraitProvider defines a specific horizontal portrait provider.
type HorizontalPortraitProvider struct {
	// Type is the type of the horizontal portrait provider.
	// +kubebuilder:validation:Enum=Static;Cron;Dynamic
	Type HorizontalPortraitProviderType `json:"type"`

	// Priority is the priority of the horizontal portrait generated by this provider.
	// A valid portrait with higher priority overrides ones with lower priority.
	// The bigger the number, the higher the priority.
	// If multiple portraits have the same priority,
	// the one which desires most replicas at current time would override the other ones.
	Priority int32 `json:"priority"`

	// Static is the configuration for static horizontal portrait provider.
	// +optional
	Static *StaticHorizontalPortraitProvider `json:"static,omitempty"`

	// Cron is the configuration for cron horizontal portrait provider.
	// +optional
	Cron *CronHorizontalPortraitProvider `json:"cron,omitempty"`

	// Dynamic is the configuration for dynamic horizontal portrait provider.
	// Note that the PortraitType must be unique across all dynamic horizontal portrait providers of the autoscaler.
	// +optional
	Dynamic *DynamicHorizontalPortraitProvider `json:"dynamic,omitempty"`
}

type HorizontalPortraitProviderType string

const (
	// StaticHorizontalPortraitProviderType is a built-in horizontal portrait provider
	// which always provides a static replicas value.
	StaticHorizontalPortraitProviderType HorizontalPortraitProviderType = "Static"

	// CronHorizontalPortraitProviderType is a built-in horizontal portrait provider
	// which provides replicas values based on cron rules.
	CronHorizontalPortraitProviderType HorizontalPortraitProviderType = "Cron"

	// DynamicHorizontalPortraitProviderType is a horizontal portrait provider
	// which provides replicas value(s) from an external HorizontalPortrait.
	DynamicHorizontalPortraitProviderType HorizontalPortraitProviderType = "Dynamic"
)

// StaticHorizontalPortraitProvider defines a built-in horizontal portrait provider
// which always provides a static replicas value.
type StaticHorizontalPortraitProvider struct {
	// Replicas is the desired number of online replicas.
	// +kubebuilder:validation:Minimum=1
	Replicas int32 `json:"replicas"`
}

// CronHorizontalPortraitProvider defines a built-in horizontal portrait provider
// which provides replicas values based on cron rules.
type CronHorizontalPortraitProvider struct {
	// Crons contains cron rule based replicas values.
	// +kubebuilder:validation:MinItems=1
	Crons []ReplicaCron `json:"crons"`
}

// ReplicaCron defines the cron rule for a desired replicas value.
type ReplicaCron struct {
	// Name is the name of this cron rule.
	// It must be unique across all rules.
	Name string `json:"name"`

	// Description is an additional description of this cron rule.
	// +optional
	Description string `json:"description,omitempty"`

	// TimeZone is the time zone in which the cron would run.
	// Defaults to UTC.
	// +optional
	// +kubebuilder:default=UTC
	TimeZone string `json:"timeZone"`

	// Start is the cron of time from which the rule takes effect.
	// It must be a valid standard cron expression.
	Start string `json:"start"`

	// End is the cron of time to which the rule takes effect.
	// It must be a valid standard cron expression.
	End string `json:"end"`

	// Replicas is the desired number of online replicas within this cron rule.
	// +kubebuilder:validation:Minimum=1
	Replicas int32 `json:"replicas"`
}

// DynamicHorizontalPortraitProvider defines a horizontal portrait provider which provides replicas value(s) from
// an external HorizontalPortrait.
// The spec of the HorizontalPortrait is managed by the provider and the status of which is produced
// by the HorizontalPortrait controller or other external systems.
type DynamicHorizontalPortraitProvider struct {
	PortraitSpec `json:",inline"`
}

// IntelligentHorizontalPodAutoscalerBehavior defines the configuration of the scaling behavior of the autoscaler.
type IntelligentHorizontalPodAutoscalerBehavior struct {
	// ScaleUp is the behavior configuration for scaling up.
	// +optional
	ScaleUp ScalingBehavior `json:"scaleUp,omitempty"`

	// ScaleDown is the behavior configuration for scaling down.
	// +optional
	ScaleDown ScalingBehavior `json:"scaleDown,omitempty"`

	// ReplicaProfile is used to configure the behavior of the underlying ReplicaProfile.
	// +optional
	ReplicaProfile *ReplicaProfileBehavior `json:"replicaProfile,omitempty"`
}

// ScalingBehavior defines the scaling behavior for one direction.
type ScalingBehavior struct {
	// GrayStrategy is the configuration of the strategy for gray change of replicas.
	// If not set, gray change will be disabled.
	// +optional
	GrayStrategy *GrayStrategy `json:"grayStrategy,omitempty"`
}

// GrayStrategy defines the strategy for gray change of replicas when scaling
// from replicas specified by previous HorizontalPortraitValue to which specified by current HorizontalPortraitValue.
type GrayStrategy struct {
	// GrayState is the desired state of pods that in gray stage.
	// For scaling up, it can only be set to "Online".
	// For scaling down, it can either be set to "Cutoff", "Standby" or "Deleted".
	// +kubebuilder:validation:Enum=Online;Cutoff;Standby;Deleted
	GrayState PodState `json:"grayState"`

	// ChangeIntervalSeconds is the interval time between each gray change.
	// +kubebuilder:validation:Minimum=1
	ChangeIntervalSeconds int32 `json:"changeIntervalSeconds"`

	// ChangePercent is the percentage of the total change of replica numbers which is used to
	// calculate the amount of pods to change in each gray change.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	ChangePercent int32 `json:"changePercent"`

	// ObservationSeconds is the additional observation time after the gray change reaching 100%.
	// During the observation time, all pods that in gray stage would be kept in GrayState
	// and the stability ensurance mechanism for the gray change would continue to take effect.
	// If not set, the gray change will not have observation time.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ObservationSeconds int32 `json:"observationSeconds,omitempty"`
}

type PodState string

const (
	// PodStateOnline means the pod is running and should handle full traffic.
	PodStateOnline PodState = "Online"

	// PodStateCutoff means the pod is running but would handle no traffic.
	PodStateCutoff PodState = "Cutoff"

	// PodStateStandby means the pod is running but its resources are reclaimed (aka hibernating).
	PodStateStandby PodState = "Standby"

	// PodStateDeleted means the pod is deleted (scaled down).
	PodStateDeleted PodState = "Deleted"
)

// StabilityChecker defines a stability checker which is used to ensure stability during autoscaling.
type StabilityChecker struct {
	// Type is the type of stability checker.
	// +kubebuilder:validation:Enum=Metrics;External
	Type StabilityCheckerType `json:"type"`

	// StabilizationAction is the action to perform in order to ensure stability
	// when the checker detects anomalies.
	// +kubebuilder:validation:Enum=Pause;Rollback
	StabilizationAction StabilizationActionType `json:"stabilizationAction"`

	// CoolDownSeconds is the cooldown time after the checker no longer detects anomalies
	// after which the autoscaling process will resume normal.
	// +optional
	// +kubebuilder:validation:Minimum=0
	CoolDownSeconds int32 `json:"coolDownSeconds,omitempty"`

	// Metrics is the configuration for metrics stability checker.
	// +optional
	Metrics *MetricsStabilityChecker `json:"metrics,omitempty"`

	// External is the configuration for external stability checker.
	// +optional
	External *ExternalStabilityChecker `json:"external,omitempty"`
}

type StabilityCheckerType string

const (
	// MetricsStabilityCheckerType is a built-in stability checker which checks for anomalies based on metrics rules.
	MetricsStabilityCheckerType StabilityCheckerType = "Metrics"

	// ExternalStabilityCheckerType is an external stability checker.
	ExternalStabilityCheckerType StabilityCheckerType = "External"
)

type StabilizationActionType string

const (
	// StabilizationActionPause pauses an autoscaling process.
	StabilizationActionPause StabilizationActionType = "Pause"

	// StabilizationActionRollback rollbacks an autoscaling process.
	StabilizationActionRollback StabilizationActionType = "Rollback"
)

// MetricsStabilityChecker defines a built-in stability checker which checks for anomalies based on metrics rules.
type MetricsStabilityChecker struct {
	// Metrics contains metrics rules of the checker.
	// +kubebuilder:validation:MinItems=1
	Metrics []MetricSpec `json:"metrics"`
}

// ExternalStabilityChecker defines an external stability checker.
type ExternalStabilityChecker struct {
	// Name is the name of the checker.
	// It must be unique across all external stability checkers of the autoscaler.
	Name string `json:"name"`

	// Config is used to pass arbitrary config to the checker.
	// +optional
	Config map[string]string `json:"config,omitempty"`
}

// IntelligentHorizontalPodAutoscalerStatus defines the observed state of IntelligentHorizontalPodAutoscaler.
type IntelligentHorizontalPodAutoscalerStatus struct {
	// PreviousPortraitValue is the last valid portrait value produced by portrait providers.
	// +optional
	PreviousPortraitValue *HorizontalPortraitValue `json:"previousPortraitValue,omitempty"`

	// CurrentPortraitValue is the current valid portrait value produced by portrait providers.
	// +optional
	CurrentPortraitValue *HorizontalPortraitValue `json:"currentPortraitValue,omitempty"`

	// Gray represents the current gray status of replicas change.
	// +optional
	Gray *GrayStatus `json:"gray,omitempty"`

	// Conditions represents current conditions of the IntelligentHorizontalPodAutoscaler.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// HorizontalPortraitValue contains the desired replicas produced by a portrait with optional expire time.
type HorizontalPortraitValue struct {
	// Provider is the unique identification of the provider of the portrait from which this value is produced.
	Provider string `json:"provider"`

	// Replicas is the desired number of online replicas.
	// +kubebuilder:validation:Minimum=1
	Replicas int32 `json:"replicas"`

	// ExpireTime indicates when this portrait value will expire.
	// +optional
	ExpireTime metav1.Time `json:"expireTime,omitempty"`
}

// GrayStatus is the representation of gray status of replicas change.
type GrayStatus struct {
	// GrayPercent is the current gray percentage of the total change.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	GrayPercent int32 `json:"grayPercent"`

	// LastUpdateTime is the last update time of GrayPercent.
	LastUpdateTime metav1.Time `json:"lastUpdateTime"`
}

type IntelligentHorizontalPodAutoscalerConditionType string

const (
	// ScalingActive indicates that the IHPA controller is able to scale if necessary:
	// it's correctly configured, can fetch the desired portraits, and isn't paused.
	ScalingActive IntelligentHorizontalPodAutoscalerConditionType = "ScalingActive"

	// ScalingLimited indicates that the scale of current portrait would be above or
	// below the range for the IHPA, and has thus been capped.
	ScalingLimited IntelligentHorizontalPodAutoscalerConditionType = "ScalingLimited"

	// GrayProgressing indicates that current scaling is in gray progress from previous portrait
	// and has not reached the desired scale of current portrait.
	GrayProgressing IntelligentHorizontalPodAutoscalerConditionType = "GrayProgressing"
)

//+kubebuilder:object:root=true

// IntelligentHorizontalPodAutoscalerList contains a list of IntelligentHorizontalPodAutoscaler.
type IntelligentHorizontalPodAutoscalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IntelligentHorizontalPodAutoscaler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IntelligentHorizontalPodAutoscaler{}, &IntelligentHorizontalPodAutoscalerList{})
}
