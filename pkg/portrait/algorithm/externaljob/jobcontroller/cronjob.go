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
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
	"github.com/traas-stack/kapacity/pkg/util"
)

type CronJobHorizontal struct {
	client client.Client
}

func NewCronJobHorizontal(client client.Client) Horizontal {
	return &CronJobHorizontal{
		client: client,
	}
}

func (h *CronJobHorizontal) UpdateJob(ctx context.Context, hp *autoscalingv1alpha1.HorizontalPortrait, cfg *autoscalingv1alpha1.PortraitAlgorithmJob) error {
	cronJobNamespacedName := types.NamespacedName{
		Namespace: hp.Namespace,
		Name:      hp.Name,
	}
	cronJob := &batchv1.CronJob{}
	if err := h.client.Get(ctx, cronJobNamespacedName, cronJob); err != nil {
		if apierrors.IsNotFound(err) {
			cronJob = &batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   cronJobNamespacedName.Namespace,
					Name:        cronJobNamespacedName.Name,
					Annotations: cfg.CronJob.Template.Annotations,
					Labels:      cfg.CronJob.Template.Labels,
					OwnerReferences: []metav1.OwnerReference{
						*util.NewControllerRef(hp),
					},
				},
				Spec: cfg.CronJob.Template.Spec,
			}

			if err := h.client.Create(ctx, cronJob); err != nil {
				return fmt.Errorf("failed to create CronJob %q: %v", cronJobNamespacedName, err)
			}

			return nil
		}

		return fmt.Errorf("failed to get CronJob %q: %v", cronJobNamespacedName, err)
	}

	if apiequality.Semantic.DeepEqual(cronJob.Spec, cfg.CronJob.Template.Spec) &&
		!util.IsMapValueChanged(cronJob.Labels, cfg.CronJob.Template.Labels) &&
		!util.IsMapValueChanged(cronJob.Annotations, cfg.CronJob.Template.Annotations) {
		return nil
	}

	patch := client.MergeFrom(cronJob.DeepCopy())
	cronJob.Spec = cfg.CronJob.Template.Spec
	if len(cfg.CronJob.Template.Labels) > 0 {
		if cronJob.Labels == nil {
			cronJob.Labels = make(map[string]string, len(cfg.CronJob.Template.Labels))
		}
		util.CopyMapValues(cronJob.Labels, cfg.CronJob.Template.Labels)
	}
	if len(cfg.CronJob.Template.Annotations) > 0 {
		if cronJob.Annotations == nil {
			cronJob.Annotations = make(map[string]string, len(cfg.CronJob.Template.Annotations))
		}
		util.CopyMapValues(cronJob.Annotations, cfg.CronJob.Template.Annotations)
	}
	if err := h.client.Patch(ctx, cronJob, patch); err != nil {
		return fmt.Errorf("failed to patch CronJob %q: %v", cronJobNamespacedName, err)
	}

	return nil
}

func (h *CronJobHorizontal) CleanupJob(ctx context.Context, hp *autoscalingv1alpha1.HorizontalPortrait) error {
	cronJobNamespacedName := types.NamespacedName{
		Namespace: hp.Namespace,
		Name:      hp.Name,
	}

	cronJob := &batchv1.CronJob{}
	if err := h.client.Get(ctx, cronJobNamespacedName, cronJob); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get CronJob %q: %v", cronJobNamespacedName, err)
	}

	if err := client.IgnoreNotFound(h.client.Delete(ctx, cronJob)); err != nil {
		return fmt.Errorf("failed to delete CronJob %q: %v", cronJobNamespacedName, err)
	}
	return nil
}
