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

package autoscaling

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
	portraitgenerator "github.com/traas-stack/kapacity/pkg/portrait/generator"
	"github.com/traas-stack/kapacity/pkg/util"
)

const (
	HorizontalPortraitControllerName = "horizontal_portrait_controller"
)

// HorizontalPortraitReconciler reconciles a HorizontalPortrait object.
type HorizontalPortraitReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	record.EventRecorder

	PortraitGenerators map[autoscalingv1alpha1.PortraitType]portraitgenerator.Interface
}

//+kubebuilder:rbac:groups=autoscaling.kapacitystack.io,resources=horizontalportraits,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=autoscaling.kapacitystack.io,resources=horizontalportraits/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=autoscaling.kapacitystack.io,resources=horizontalportraits/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=metrics.k8s.io,resources=*,verbs=get;list;watch
//+kubebuilder:rbac:groups=custom.metrics.k8s.io,resources=*,verbs=get;list;watch
//+kubebuilder:rbac:groups=external.metrics.k8s.io,resources=*,verbs=get;list;watch
//+kubebuilder:rbac:groups=*,resources=*/scale,verbs=get
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *HorizontalPortraitReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	l.V(2).Info("start to reconcile")

	hp := &autoscalingv1alpha1.HorizontalPortrait{}
	if err := r.Get(ctx, req.NamespacedName, hp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		l.Error(err, "failed to get HorizontalPortrait")
		return ctrl.Result{}, err
	}

	hpStatusOriginal := hp.Status.DeepCopy()

	portraitGenerator, ok := r.PortraitGenerators[hp.Spec.PortraitType]
	if !ok {
		// this should not happen because we have watch predicates
		l.Info("unknown portrait type, ignore", "portraitType", hp.Spec.PortraitType)
		return ctrl.Result{}, nil
	}

	data, requeueAfter, err := portraitGenerator.GenerateHorizontal(ctx, hp.Namespace, hp.Spec.ScaleTargetRef, hp.Spec.Metrics, hp.Spec.Algorithm)
	if err != nil {
		l.Error(err, "failed to generate portrait")
		r.errorOut(ctx, hp, hpStatusOriginal, autoscalingv1alpha1.PortraitGenerated, metav1.ConditionFalse,
			"FailedGeneratePortrait", fmt.Sprintf("failed to generate portrait: %v", err))
		return ctrl.Result{}, err
	}

	hp.Status.PortraitData = data
	setHorizontalPortraitCondition(hp, autoscalingv1alpha1.PortraitGenerated, metav1.ConditionTrue,
		"SucceededGeneratePortrait", "portrait has been successfully generated")
	if err := r.updateStatusIfNeeded(ctx, hp, hpStatusOriginal); err != nil {
		l.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HorizontalPortraitReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(HorizontalPortraitControllerName).
		For(&autoscalingv1alpha1.HorizontalPortrait{},
			builder.WithPredicates(
				predicate.GenerationChangedPredicate{},
				predicate.NewPredicateFuncs(func(object client.Object) bool {
					hp, ok := object.(*autoscalingv1alpha1.HorizontalPortrait)
					if !ok {
						return false
					}
					switch hp.Spec.PortraitType {
					case autoscalingv1alpha1.ReactivePortraitType:
					case autoscalingv1alpha1.PredictivePortraitType:
					case autoscalingv1alpha1.BurstPortraitType:
					default:
						return false
					}
					return true
				}),
			)).
		WithOptions(controller.Options{
			RecoverPanic: true,
		}).
		Complete(r)
}

func (r *HorizontalPortraitReconciler) errorOut(ctx context.Context, hp *autoscalingv1alpha1.HorizontalPortrait, oldStatus *autoscalingv1alpha1.HorizontalPortraitStatus,
	conditionType autoscalingv1alpha1.HorizontalPortraitConditionType, conditionStatus metav1.ConditionStatus, reason, message string) {
	r.Event(hp, corev1.EventTypeWarning, reason, message)
	setHorizontalPortraitCondition(hp, conditionType, conditionStatus, reason, message)
	if err := r.updateStatusIfNeeded(ctx, hp, oldStatus); err != nil {
		log.FromContext(ctx).Error(err, "failed to update status")
	}
}

func (r *HorizontalPortraitReconciler) updateStatusIfNeeded(ctx context.Context, hp *autoscalingv1alpha1.HorizontalPortrait, oldStatus *autoscalingv1alpha1.HorizontalPortraitStatus) error {
	if apiequality.Semantic.DeepEqual(oldStatus, &hp.Status) {
		return nil
	}
	if err := r.Status().Update(ctx, hp); err != nil {
		return err
	}
	hp.Status.DeepCopyInto(oldStatus)
	return nil
}

func setHorizontalPortraitCondition(hp *autoscalingv1alpha1.HorizontalPortrait, conditionType autoscalingv1alpha1.HorizontalPortraitConditionType, status metav1.ConditionStatus, reason, message string) {
	hp.Status.Conditions = util.SetConditionInList(hp.Status.Conditions, string(conditionType), status, hp.Generation, reason, message)
}
