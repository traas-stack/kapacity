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

package jobcontroller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
)

const (
	cronJobNamespace = "cron-job-test"

	hpNamespace = "test"
	hpName      = "test-predictive"
	cronJobName = hpNamespace + "-" + hpName
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = autoscalingv1alpha1.AddToScheme(scheme)
}

func TestCronJobHorizontal_Create(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects().Build()
	ctx := context.Background()
	horizontalPortrait := &autoscalingv1alpha1.HorizontalPortrait{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: hpNamespace,
			Name:      hpName,
		},
	}

	cfg := &autoscalingv1alpha1.PortraitAlgorithmJob{
		Type: autoscalingv1alpha1.CronJobPortraitAlgorithmJobType,
		CronJob: &autoscalingv1alpha1.CronJobPortraitAlgorithmJob{
			Template: autoscalingv1alpha1.CronJobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"cronjob": "test"},
				},
				Spec: batchv1.CronJobSpec{
					Schedule:    "* * * * *",
					JobTemplate: batchv1.JobTemplateSpec{},
				},
			},
		},
	}

	cronJobHorizontal := NewCronJobHorizontal(fakeClient, cronJobNamespace, "", nil)
	err := cronJobHorizontal.UpdateJob(ctx, horizontalPortrait, cfg)
	assert.Nil(t, err)

	cronJob := &batchv1.CronJob{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Namespace: cronJobNamespace, Name: cronJobName}, cronJob)
	assert.NotNil(t, cronJob)

	err = cronJobHorizontal.CleanupJob(ctx, horizontalPortrait)
	assert.Nil(t, err)

	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: cronJobNamespace, Name: cronJobName}, cronJob)
	assert.True(t, apierrors.IsNotFound(err))
}

func TestCronJobHorizontal_Update(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects().Build()
	ctx := context.Background()
	horizontalPortrait := &autoscalingv1alpha1.HorizontalPortrait{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: hpNamespace,
			Name:      hpName,
		},
	}

	cfg := &autoscalingv1alpha1.PortraitAlgorithmJob{
		Type: autoscalingv1alpha1.CronJobPortraitAlgorithmJobType,
		CronJob: &autoscalingv1alpha1.CronJobPortraitAlgorithmJob{
			Template: autoscalingv1alpha1.CronJobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"cronjob": "test"},
					Annotations: map[string]string{"a": "b"},
				},
				Spec: batchv1.CronJobSpec{
					Schedule:    "* * * * *",
					JobTemplate: batchv1.JobTemplateSpec{},
				},
			},
		},
	}

	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   cronJobNamespace,
			Name:        cronJobName,
			Annotations: map[string]string{"c": "d"},
		},
		Spec: batchv1.CronJobSpec{},
	}
	_ = fakeClient.Create(ctx, cronJob)

	cronJobHorizontal := NewCronJobHorizontal(fakeClient, cronJobNamespace, "", nil)
	err := cronJobHorizontal.UpdateJob(ctx, horizontalPortrait, cfg)
	assert.Nil(t, err)

	_ = fakeClient.Get(ctx, types.NamespacedName{Namespace: cronJobNamespace, Name: cronJobName}, cronJob)
	assert.NotNil(t, cronJob)
	assert.True(t, len(cronJob.Labels) > 0)

	_ = cronJobHorizontal.CleanupJob(ctx, horizontalPortrait)
}
