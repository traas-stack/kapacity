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
	"testing"
	"time"

	"github.com/mitchellh/hashstructure/v2"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
)

const oneSecond = 1*time.Second + 50*time.Millisecond

var (
	ctx           = context.Background()
	namespaceName = types.NamespacedName{Namespace: "namespace", Name: "name"}
	ihpa          = &autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespaceName.Name,
			Namespace: namespaceName.Namespace,
		},
	}
	targetReplicas int32 = 1
	genericEvent         = make(chan event.GenericEvent, 1024)
	crons                = []autoscalingv1alpha1.ReplicaCron{
		{
			Name:     "cron-task-1",
			Replicas: targetReplicas,
			TimeZone: "UTC",
			Start:    "0 0 * * ?",
			End:      "* * * * ?",
		},
	}
)

func TestStart(t *testing.T) {
	ch := make(chan bool)
	crontabs := make([]*cron.Cron, 0)

	location, _ := time.LoadLocation("UTC")
	crontab := cron.New(cron.WithLocation(location), cron.WithSeconds())
	_, err := crontab.AddFunc("* * * * * ?", func() {
		ch <- true
	})
	assert.Nil(t, err)

	crontabs = append(crontabs, crontab)
	hash, _ := hashstructure.Hash("crons", hashstructure.FormatV2, nil)
	cronTask := &cronTask{
		Hash:     hash,
		Crontabs: crontabs,
	}

	cronTask.Start()
	defer cronTask.Stop()

	select {
	case <-time.After(oneSecond):
		t.Error("cron task timeout")
	case <-ch:
		// expected
	}
}

func TestStop(t *testing.T) {
	ch := make(chan bool)
	crontabs := make([]*cron.Cron, 0)

	location, _ := time.LoadLocation("UTC")
	crontab := cron.New(cron.WithLocation(location), cron.WithSeconds())
	_, err := crontab.AddFunc("* * * * * ?", func() {
		ch <- true
	})
	assert.Nil(t, err)

	crontabs = append(crontabs, crontab)
	hash, _ := hashstructure.Hash("crons", hashstructure.FormatV2, nil)
	cronTask := &cronTask{
		Hash:     hash,
		Crontabs: crontabs,
	}

	cronTask.Start()
	cronTask.Stop()

	select {
	case <-time.After(oneSecond):
		// expected
	case <-ch:
		t.Error("context was not done immediately")
	}
}

func TestStartCronTaskTrigger(t *testing.T) {
	cronTaskTriggerManage := newCronTaskTriggerManager(genericEvent)
	err := cronTaskTriggerManage.StartCronTaskTrigger(namespaceName, ihpa, crons)
	assert.Nil(t, err)
	defer cronTaskTriggerManage.StopCronTaskTrigger(namespaceName)

	taskV, ok := cronTaskTriggerManage.cronTaskMap.Load(namespaceName)
	assert.True(t, ok)

	hash, err := hashstructure.Hash(crons, hashstructure.FormatV2, nil)
	assert.Nil(t, err)

	task := taskV.(*cronTask)
	assert.True(t, task.Hash == hash)
}

func TestStopCronTaskTrigger(t *testing.T) {
	cronTaskTriggerManage := newCronTaskTriggerManager(genericEvent)
	err := cronTaskTriggerManage.StartCronTaskTrigger(namespaceName, ihpa, crons)
	assert.Nil(t, err)
	cronTaskTriggerManage.StopCronTaskTrigger(namespaceName)

	_, ok := cronTaskTriggerManage.cronTaskMap.Load(namespaceName)
	assert.False(t, ok, "the cron task is not stopped")
}

func TestStartTimerTrigger(t *testing.T) {
	ctx := context.Background()

	timerTriggerManager := newTimerTriggerManager(genericEvent)
	timerTriggerManager.StartTimerTrigger(ctx, namespaceName, ihpa, time.Now())
	defer timerTriggerManager.StopTimerTrigger(namespaceName)

	_, ok := timerTriggerManager.timerCancellerMap.Load(namespaceName)
	assert.True(t, ok)

	select {
	case ihpaEvent := <-genericEvent:
		if ihpaEvent.Object.GetName() != ihpa.Name {
			t.Error("wrong event")
		}
		// expected
	case <-time.After(oneSecond):
		t.Error("time trigger timeout")
	}
}

func TestStopTimerTrigger(t *testing.T) {
	ctx := context.Background()

	timerTriggerManager := newTimerTriggerManager(genericEvent)
	timerTriggerManager.StartTimerTrigger(ctx, namespaceName, ihpa, time.Now())
	timerTriggerManager.StopTimerTrigger(namespaceName)

	_, ok := timerTriggerManager.timerCancellerMap.Load(namespaceName)
	assert.False(t, ok, "the time trigger is not stopped")
}
