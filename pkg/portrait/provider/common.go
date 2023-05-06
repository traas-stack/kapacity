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
	"sync"
	"time"

	"github.com/mitchellh/hashstructure/v2"
	"github.com/robfig/cron/v3"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
	"github.com/traas-stack/kapacity/pkg/util"
)

type cronTask struct {
	Hash     uint64
	Crontabs []*cron.Cron
}

func (c *cronTask) Stop() {
	for _, crontab := range c.Crontabs {
		crontab.Stop()
	}
}

func (c *cronTask) Start() {
	for _, crontab := range c.Crontabs {
		crontab.Start()
	}
}

type cronTaskTriggerManager struct {
	cronTaskMap  sync.Map // map[types.NamespacedName]*cronTask
	eventTrigger chan event.GenericEvent
}

func newCronTaskTriggerManager(eventTrigger chan event.GenericEvent) *cronTaskTriggerManager {
	return &cronTaskTriggerManager{
		eventTrigger: eventTrigger,
	}
}

func (m *cronTaskTriggerManager) StartCronTaskTrigger(taskKey types.NamespacedName, ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler, crons []autoscalingv1alpha1.ReplicaCron) error {
	hash, err := hashstructure.Hash(crons, hashstructure.FormatV2, nil)
	if err != nil {
		return fmt.Errorf("failed to hash crons: %v", err)
	}

	taskV, ok := m.cronTaskMap.Load(taskKey)
	if ok {
		task := taskV.(*cronTask)
		if hash == task.Hash {
			return nil
		}
		task.Stop()
		m.cronTaskMap.Delete(taskKey)
	}

	task := &cronTask{
		Hash:     hash,
		Crontabs: make([]*cron.Cron, 0, len(crons)),
	}

	for _, cronData := range crons {
		location, err := time.LoadLocation(cronData.TimeZone)
		if err != nil {
			return fmt.Errorf("failed to load time zone of cron %q: %v", cronData.Name, err)
		}

		ihpaEvent := event.GenericEvent{Object: ihpa}

		crontab := cron.New(cron.WithLocation(location))
		if _, err := crontab.AddFunc(cronData.Start, func() {
			m.eventTrigger <- ihpaEvent
		}); err != nil {
			return fmt.Errorf("failed to add crontab start function of cron %q: %v", cronData.Name, err)
		}

		if _, err := crontab.AddFunc(cronData.End, func() {
			m.eventTrigger <- ihpaEvent
		}); err != nil {
			return fmt.Errorf("failed to add crontab end function of cron %q: %v", cronData.Name, err)
		}

		task.Crontabs = append(task.Crontabs, crontab)
	}

	m.cronTaskMap.Store(taskKey, task)
	task.Start()
	return nil
}

func (m *cronTaskTriggerManager) StopCronTaskTrigger(taskKey types.NamespacedName) {
	taskV, ok := m.cronTaskMap.Load(taskKey)
	if ok {
		task := taskV.(*cronTask)
		task.Stop()
		m.cronTaskMap.Delete(taskKey)
	}
}

func getActiveReplicaCron(crons []autoscalingv1alpha1.ReplicaCron) (*autoscalingv1alpha1.ReplicaCron, time.Time, error) {
	var (
		active     *autoscalingv1alpha1.ReplicaCron
		expireTime time.Time
	)
	now := time.Now()
	for i := range crons {
		rc := &crons[i]

		location, err := time.LoadLocation(rc.TimeZone)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("failed to load time zone of cron %q: %v", rc.Name, err)
		}
		t := now.In(location)

		isActive, cronExpireTime, err := util.IsCronActive(t, rc.Start, rc.End)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("failed to check active for cron %q: %v", rc.Name, err)
		}

		if isActive {
			if active != nil {
				return nil, time.Time{}, fmt.Errorf("found overlapping cron %q and %q", active.Name, rc.Name)
			}
			active = rc
			expireTime = cronExpireTime
		}
	}
	return active, expireTime, nil
}

type timerCanceller struct {
	T      time.Time
	Cancel context.CancelFunc
}

type timerTriggerManager struct {
	timerCancellerMap sync.Map // map[types.NamespacedName]*timerCanceller
	eventTrigger      chan event.GenericEvent
}

func newTimerTriggerManager(eventTrigger chan event.GenericEvent) *timerTriggerManager {
	return &timerTriggerManager{
		eventTrigger: eventTrigger,
	}
}

func (m *timerTriggerManager) StartTimerTrigger(ctx context.Context, timerKey types.NamespacedName, ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler, t time.Time) {
	cancellerV, ok := m.timerCancellerMap.Load(timerKey)
	if ok {
		canceller := cancellerV.(*timerCanceller)
		if t.Equal(canceller.T) {
			return
		}
		canceller.Cancel()
		m.timerCancellerMap.Delete(timerKey)
	}

	timerContext, cancel := context.WithCancel(ctx)
	m.timerCancellerMap.Store(timerKey, &timerCanceller{
		T:      t,
		Cancel: cancel,
	})

	go func() {
		d := time.Until(t)
		if d <= 0 {
			m.eventTrigger <- event.GenericEvent{Object: ihpa}
			return
		}
		timer := time.NewTimer(d)
		select {
		case <-timer.C:
			m.eventTrigger <- event.GenericEvent{Object: ihpa}
		case <-timerContext.Done():
			timer.Stop()
		}
	}()
}

func (m *timerTriggerManager) StopTimerTrigger(timerKey types.NamespacedName) {
	cancellerV, ok := m.timerCancellerMap.Load(timerKey)
	if ok {
		canceller := cancellerV.(*timerCanceller)
		canceller.Cancel()
		m.timerCancellerMap.Delete(timerKey)
	}
}
