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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/traas-stack/kapacity/api/v1alpha1"
)

// CronHorizontal provides horizontal portraits with replicas values based on cron rules.
type CronHorizontal struct {
	cronTaskTriggerManager *cronTaskTriggerManager
}

// NewCronHorizontal creates a new CronHorizontal with the given event trigger.
func NewCronHorizontal(eventTrigger chan event.GenericEvent) Horizontal {
	return &CronHorizontal{
		cronTaskTriggerManager: newCronTaskTriggerManager(eventTrigger),
	}
}

func (*CronHorizontal) GetPortraitIdentifier(*v1alpha1.IntelligentHorizontalPodAutoscaler, *v1alpha1.HorizontalPortraitProvider) string {
	return string(v1alpha1.CronHorizontalPortraitProviderType)
}

func (h *CronHorizontal) UpdatePortraitSpec(_ context.Context, ihpa *v1alpha1.IntelligentHorizontalPodAutoscaler, cfg *v1alpha1.HorizontalPortraitProvider) error {
	return h.cronTaskTriggerManager.StartCronTaskTrigger(types.NamespacedName{Namespace: ihpa.Namespace, Name: ihpa.Name}, ihpa, cfg.Cron.Crons)
}

func (h *CronHorizontal) FetchPortraitValue(_ context.Context, ihpa *v1alpha1.IntelligentHorizontalPodAutoscaler, cfg *v1alpha1.HorizontalPortraitProvider) (*v1alpha1.HorizontalPortraitValue, error) {
	rc, expireTime, err := getActiveReplicaCron(cfg.Cron.Crons)
	if err != nil {
		return nil, err
	}

	if rc == nil {
		return nil, nil
	}

	return &v1alpha1.HorizontalPortraitValue{
		Provider:   h.GetPortraitIdentifier(ihpa, cfg),
		Replicas:   rc.Replicas,
		ExpireTime: metav1.NewTime(expireTime),
	}, nil
}

func (h *CronHorizontal) CleanupPortrait(_ context.Context, ihpa *v1alpha1.IntelligentHorizontalPodAutoscaler, _ string) error {
	h.cronTaskTriggerManager.StopCronTaskTrigger(types.NamespacedName{Namespace: ihpa.Namespace, Name: ihpa.Name})
	return nil
}
