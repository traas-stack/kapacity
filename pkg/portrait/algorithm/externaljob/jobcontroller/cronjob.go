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
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
	"github.com/traas-stack/kapacity/pkg/util"
)

const (
	defaultAlgorithmContainerName = "algorithm"

	envMetricsServerAddr = "METRICS_SERVER_ADDR"
	envHPNamespace       = "HP_NAMESPACE"
	envHPName            = "HP_NAME"
)

type CronJobHorizontal struct {
	client                   client.Client
	namespace                string
	defaultServiceAccount    string
	defaultMetricsServerAddr string
	defaultImages            map[autoscalingv1alpha1.PortraitType]string
}

func NewCronJobHorizontal(client client.Client, namespace, defaultServiceAccount, defaultMetricsServerAddr string, defaultImages map[autoscalingv1alpha1.PortraitType]string) Horizontal {
	return &CronJobHorizontal{
		client:                   client,
		namespace:                namespace,
		defaultServiceAccount:    defaultServiceAccount,
		defaultMetricsServerAddr: defaultMetricsServerAddr,
		defaultImages:            defaultImages,
	}
}

func (h *CronJobHorizontal) UpdateJob(ctx context.Context, hp *autoscalingv1alpha1.HorizontalPortrait, cfg *autoscalingv1alpha1.PortraitAlgorithmJob) error {
	cronJobNamespacedName := h.buildCronJobNamespacedName(hp)
	h.defaultingTemplatePodSpec(&cfg.CronJob.Template.Spec.JobTemplate.Spec.Template.Spec, hp)

	cronJob := &batchv1.CronJob{}
	if err := h.client.Get(ctx, cronJobNamespacedName, cronJob); err != nil {
		if apierrors.IsNotFound(err) {
			cronJob = &batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   cronJobNamespacedName.Namespace,
					Name:        cronJobNamespacedName.Name,
					Annotations: cfg.CronJob.Template.Annotations,
					Labels:      cfg.CronJob.Template.Labels,
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
	cronJobNamespacedName := h.buildCronJobNamespacedName(hp)

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

func (h *CronJobHorizontal) buildCronJobNamespacedName(hp *autoscalingv1alpha1.HorizontalPortrait) types.NamespacedName {
	return types.NamespacedName{
		Namespace: h.namespace,
		Name:      fmt.Sprintf("%s-%s", hp.Namespace, hp.Name),
	}
}

func (h *CronJobHorizontal) defaultingTemplatePodSpec(spec *corev1.PodSpec, hp *autoscalingv1alpha1.HorizontalPortrait) {
	if h.defaultServiceAccount != "" && spec.ServiceAccountName == "" && spec.DeprecatedServiceAccount == "" {
		spec.ServiceAccountName = h.defaultServiceAccount
		spec.DeprecatedServiceAccount = h.defaultServiceAccount
	}
	for i := range spec.Containers {
		container := &spec.Containers[i]
		if container.Name != defaultAlgorithmContainerName {
			continue
		}
		if container.Image == "" {
			container.Image = h.defaultImages[hp.Spec.PortraitType]
		}
		if container.Env == nil {
			container.Env = make([]corev1.EnvVar, 0)
		}
		metricsServerAddrSet := false
		for _, env := range container.Env {
			if env.Name == envMetricsServerAddr {
				metricsServerAddrSet = true
				break
			}
		}
		if !metricsServerAddrSet {
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  envMetricsServerAddr,
				Value: h.defaultMetricsServerAddr,
			})
		}
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  envHPNamespace,
			Value: hp.Namespace,
		})
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  envHPName,
			Value: hp.Name,
		})
		break
	}
}
