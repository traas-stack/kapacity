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

package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
	"github.com/traas-stack/kapacity/pkg/util"
)

// DynamicHorizontal provides horizontal portraits with replicas values from external HorizontalPortraits.
type DynamicHorizontal struct {
	client                 client.Client
	cronTaskTriggerManager *cronTaskTriggerManager
	timerTriggerManager    *timerTriggerManager
}

// NewDynamicHorizontal creates a new DynamicHorizontal with the given Kubernetes client and event trigger.
func NewDynamicHorizontal(client client.Client, eventTrigger chan event.GenericEvent) Horizontal {
	return &DynamicHorizontal{
		client:                 client,
		cronTaskTriggerManager: newCronTaskTriggerManager(eventTrigger),
		timerTriggerManager:    newTimerTriggerManager(eventTrigger),
	}
}

func (*DynamicHorizontal) GetPortraitIdentifier(_ *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler, cfg *autoscalingv1alpha1.HorizontalPortraitProvider) string {
	return fmt.Sprintf("%s-%s", autoscalingv1alpha1.DynamicHorizontalPortraitProviderType, cfg.Dynamic.PortraitType)
}

func (h *DynamicHorizontal) UpdatePortraitSpec(ctx context.Context, ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler, cfg *autoscalingv1alpha1.HorizontalPortraitProvider) error {
	hpNamespacedName := types.NamespacedName{
		Namespace: ihpa.Namespace,
		Name:      buildHorizontalPortraitName(ihpa.Name, cfg.Dynamic.PortraitType),
	}

	hpSpec := autoscalingv1alpha1.HorizontalPortraitSpec{
		ScaleTargetRef: ihpa.Spec.ScaleTargetRef,
		PortraitSpec:   cfg.Dynamic.PortraitSpec,
	}

	hp := &autoscalingv1alpha1.HorizontalPortrait{}
	if err := h.client.Get(ctx, hpNamespacedName, hp); err != nil {
		if apierrors.IsNotFound(err) {
			hp = &autoscalingv1alpha1.HorizontalPortrait{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hpNamespacedName.Namespace,
					Name:      hpNamespacedName.Name,
					OwnerReferences: []metav1.OwnerReference{
						*util.NewControllerRef(ihpa),
					},
				},
				Spec: hpSpec,
			}

			if err := h.client.Create(ctx, hp); err != nil {
				return fmt.Errorf("failed to create HorizontalPortrait %q: %v", hpNamespacedName, err)
			}

			return nil
		}

		return fmt.Errorf("failed to get HorizontalPortrait %q: %v", hpNamespacedName, err)
	}

	if apiequality.Semantic.DeepEqual(hp.Spec, hpSpec) {
		return nil
	}

	patch := client.MergeFrom(hp.DeepCopy())
	hp.Spec = hpSpec
	if err := h.client.Patch(ctx, hp, patch); err != nil {
		return fmt.Errorf("failed to patch HorizontalPortrait %q: %v", hpNamespacedName, err)
	}

	return nil
}

func (h *DynamicHorizontal) FetchPortraitValue(ctx context.Context, ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler, cfg *autoscalingv1alpha1.HorizontalPortraitProvider) (*autoscalingv1alpha1.HorizontalPortraitValue, error) {
	hp := &autoscalingv1alpha1.HorizontalPortrait{}
	hpNamespacedName := types.NamespacedName{
		Namespace: ihpa.Namespace,
		Name:      buildHorizontalPortraitName(ihpa.Name, cfg.Dynamic.PortraitType),
	}

	if err := h.client.Get(ctx, hpNamespacedName, hp); err != nil {
		return nil, fmt.Errorf("failed to get HorizontalPortrait %q: %v", hpNamespacedName, err)
	}

	if hp.Status.PortraitData == nil {
		return nil, nil
	}

	v, err := h.buildPortraitValue(ctx, ihpa, h.GetPortraitIdentifier(ihpa, cfg), hpNamespacedName, hp.Status.PortraitData)
	if err != nil {
		return nil, fmt.Errorf("failed to build portrait value from data of HorizontalPortrait %q: %v", hpNamespacedName, err)
	}
	return v, nil
}

func (h *DynamicHorizontal) CleanupPortrait(ctx context.Context, ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler, identifier string) error {
	hp := &autoscalingv1alpha1.HorizontalPortrait{}
	hpNamespacedName := types.NamespacedName{
		Namespace: ihpa.Namespace,
		Name:      buildHorizontalPortraitName(ihpa.Name, autoscalingv1alpha1.PortraitType(strings.Split(identifier, "-")[1])),
	}

	h.cronTaskTriggerManager.StopCronTaskTrigger(hpNamespacedName)
	h.timerTriggerManager.StopTimerTrigger(hpNamespacedName)

	if err := h.client.Get(ctx, hpNamespacedName, hp); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get HorizontalPortrait %q: %v", hpNamespacedName, err)
	}

	if err := client.IgnoreNotFound(h.client.Delete(ctx, hp)); err != nil {
		return fmt.Errorf("failed to delete HorizontalPortrait %q: %v", hpNamespacedName, err)
	}
	return nil
}

func (h *DynamicHorizontal) buildPortraitValue(ctx context.Context, ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler,
	provider string, hpNamespacedName types.NamespacedName, portraitData *autoscalingv1alpha1.HorizontalPortraitData) (*autoscalingv1alpha1.HorizontalPortraitValue, error) {
	now := time.Now()
	expireTime := time.Time{}
	if portraitData.ExpireTime != nil {
		expireTime = portraitData.ExpireTime.Time
	}
	// cleanup and return nothing if the portrait data is expired
	if !expireTime.IsZero() && !now.Before(expireTime) {
		h.cronTaskTriggerManager.StopCronTaskTrigger(hpNamespacedName)
		h.timerTriggerManager.StopTimerTrigger(hpNamespacedName)
		return nil, nil
	}

	var (
		replicas         int32
		needTimerTrigger = true
		timerTriggerTime time.Time
	)
	switch portraitData.Type {
	case autoscalingv1alpha1.StaticHorizontalPortraitDataType:
		replicas = portraitData.Static.Replicas
	case autoscalingv1alpha1.CronHorizontalPortraitDataType:
		if err := h.cronTaskTriggerManager.StartCronTaskTrigger(hpNamespacedName, ihpa, portraitData.Cron.Crons); err != nil {
			return nil, fmt.Errorf("failed to run cron task for portrait data: %v", err)
		}

		rc, cronExpireTime, err := getActiveReplicaCron(portraitData.Cron.Crons)
		if err != nil {
			return nil, fmt.Errorf("failed to get active replica cron from portrait data: %v", err)
		}

		if rc != nil {
			replicas = rc.Replicas
			if expireTime.IsZero() || cronExpireTime.Before(expireTime) {
				expireTime = cronExpireTime
				// we don't need an additional timer trigger in this case because the cron trigger would handle this
				needTimerTrigger = false
			}
		}
	case autoscalingv1alpha1.TimeSeriesHorizontalPortraitDataType:
		series := portraitData.TimeSeries.TimeSeries
		now := now.Unix()
		// find the latest point which shall take effect
		found := false
		for i := len(series) - 1; i >= 0; i-- {
			point := series[i]
			if point.Timestamp > now {
				continue
			}

			found = true
			replicas = point.Replicas
			// adjust expire time to the time of next point (if there is)
			if i < len(series)-1 {
				nextTime := time.Unix(series[i+1].Timestamp, 0)
				if expireTime.IsZero() || nextTime.Before(expireTime) {
					expireTime = nextTime
				}
			}
			break
		}
		if !found {
			// no point shall take effect now, trigger when the first (earliest) point take into effect
			timerTriggerTime = time.Unix(series[0].Timestamp, 0)
		}
	default:
		return nil, fmt.Errorf("unknown horizontal portrait data type %q", portraitData.Type)
	}

	if !expireTime.IsZero() && (timerTriggerTime.IsZero() || expireTime.Before(timerTriggerTime)) {
		timerTriggerTime = expireTime
	}
	if needTimerTrigger && !timerTriggerTime.IsZero() {
		h.timerTriggerManager.StartTimerTrigger(ctx, hpNamespacedName, ihpa, timerTriggerTime)
	}

	if replicas > 0 {
		return &autoscalingv1alpha1.HorizontalPortraitValue{
			Provider:   provider,
			Replicas:   replicas,
			ExpireTime: &metav1.Time{Time: expireTime},
		}, nil
	} else {
		return nil, nil
	}
}

func buildHorizontalPortraitName(name string, portraitType autoscalingv1alpha1.PortraitType) string {
	return strings.ToLower(fmt.Sprintf("%s-%s", name, portraitType))
}
