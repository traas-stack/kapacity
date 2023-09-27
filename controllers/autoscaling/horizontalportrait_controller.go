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
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
	"github.com/traas-stack/kapacity/controllers"
	"github.com/traas-stack/kapacity/pkg/portrait/algorithm/externaljob/jobcontroller"
	"github.com/traas-stack/kapacity/pkg/portrait/algorithm/externaljob/resultfetcher"
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
	EventTrigger chan event.GenericEvent

	PortraitGenerators map[autoscalingv1alpha1.PortraitType]portraitgenerator.Interface

	ExternalAlgorithmJobControllers    map[autoscalingv1alpha1.PortraitAlgorithmJobType]jobcontroller.Horizontal
	ExternalAlgorithmJobResultFetchers map[autoscalingv1alpha1.PortraitAlgorithmResultSourceType]resultfetcher.Horizontal

	// externalAlgorithmJobTypesMap stores the last seen external algorithm job types of HorizontalPortraits.
	// This is used to determine the job that shall be cleaned up.
	externalAlgorithmJobTypesMap sync.Map // map[types.NamespacedName]autoscalingv1alpha1.PortraitAlgorithmJobType
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
//+kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch

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

	if !hp.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(hp, controllers.Finalizer) {
			if err := r.cleanupLastExternalAlgorithmJob(ctx, hp); err != nil {
				l.Error(err, "failed to clean up external algorithm job")
				r.Event(hp, corev1.EventTypeWarning, "FailedCleanupExternalAlgorithmJob",
					fmt.Sprintf("failed to clean up external algorithm job: %v", err))
				return ctrl.Result{}, err
			}

			if err := r.removeFinalizer(ctx, hp); err != nil {
				l.Error(err, "failed to remove finalizer")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if err := r.ensureFinalizer(ctx, hp); err != nil {
		l.Error(err, "failed to ensure finalizer")
		return ctrl.Result{}, err
	}

	hpStatusOriginal := hp.Status.DeepCopy()

	var (
		data         *autoscalingv1alpha1.HorizontalPortraitData
		requeueAfter time.Duration
		err          error
	)
	if hp.Spec.Algorithm.Type == autoscalingv1alpha1.ExternalJobPortraitAlgorithmType {
		if err := r.reconcileExternalAlgorithmJob(ctx, hp); err != nil {
			l.Error(err, "failed to reconcile external algorithm job")
			r.errorOut(ctx, hp, hpStatusOriginal, autoscalingv1alpha1.PortraitGenerated, metav1.ConditionFalse,
				"FailedReconcileExternalAlgorithmJob", fmt.Sprintf("failed to reconcile external algorithm job: %v", err))
			return ctrl.Result{}, err
		}

		data, err = r.fetchExternalAlgorithmJobResult(ctx, hp)
		if err != nil {
			l.Error(err, "failed to fetch external algorithm job result")
			r.errorOut(ctx, hp, hpStatusOriginal, autoscalingv1alpha1.PortraitGenerated, metav1.ConditionFalse,
				"FailedFetchExternalAlgorithmJobResult", fmt.Sprintf("failed to fetch external algorithm job result: %v", err))
			return ctrl.Result{}, err
		}
	} else {
		if err := r.cleanupLastExternalAlgorithmJob(ctx, hp); err != nil {
			l.Error(err, "failed to clean up external algorithm job")
			r.errorOut(ctx, hp, hpStatusOriginal, autoscalingv1alpha1.PortraitGenerated, metav1.ConditionFalse,
				"FailedCleanupExternalAlgorithmJob", fmt.Sprintf("failed to clean up external algorithm job: %v", err))
			return ctrl.Result{}, err
		}

		portraitGenerator, ok := r.PortraitGenerators[hp.Spec.PortraitType]
		if !ok {
			// this should not happen because we have watch predicates
			l.Info("unknown portrait type, ignore", "portraitType", hp.Spec.PortraitType)
			return ctrl.Result{}, nil
		}

		data, requeueAfter, err = portraitGenerator.GenerateHorizontal(ctx, hp.Namespace, hp.Spec.ScaleTargetRef, hp.Spec.Metrics, hp.Spec.Algorithm)
		if err != nil {
			l.Error(err, "failed to generate portrait")
			r.errorOut(ctx, hp, hpStatusOriginal, autoscalingv1alpha1.PortraitGenerated, metav1.ConditionFalse,
				"FailedGeneratePortrait", fmt.Sprintf("failed to generate portrait: %v", err))
			return ctrl.Result{}, err
		}
	}

	if data != nil {
		hp.Status.PortraitData = data
		setHorizontalPortraitCondition(hp, autoscalingv1alpha1.PortraitGenerated, metav1.ConditionTrue,
			"SucceededGeneratePortrait", "portrait has been successfully generated")
		if err := r.updateStatusIfNeeded(ctx, hp, hpStatusOriginal); err != nil {
			l.Error(err, "failed to update status")
			return ctrl.Result{}, err
		}
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
		Watches(&source.Channel{Source: r.EventTrigger}, &handler.EnqueueRequestForObject{}).
		WithOptions(controller.Options{
			RecoverPanic: true,
		}).
		Complete(r)
}

func (r *HorizontalPortraitReconciler) reconcileExternalAlgorithmJob(ctx context.Context, hp *autoscalingv1alpha1.HorizontalPortrait) error {
	jobCfg := hp.Spec.Algorithm.ExternalJob.Job
	jobType := jobCfg.Type
	jobController, ok := r.ExternalAlgorithmJobControllers[jobType]
	if !ok {
		return fmt.Errorf("unknown external algorithm job type %q", jobType)
	}

	n := types.NamespacedName{Namespace: hp.Namespace, Name: hp.Name}
	previousJobType, ok := r.externalAlgorithmJobTypesMap.Load(n)
	if ok && jobType != previousJobType.(autoscalingv1alpha1.PortraitAlgorithmJobType) {
		if err := r.cleanupExternalAlgorithmJob(ctx, hp, previousJobType.(autoscalingv1alpha1.PortraitAlgorithmJobType)); err != nil {
			return fmt.Errorf("failed to clean up previous external algorithm job: %v", err)
		}
	}
	r.externalAlgorithmJobTypesMap.Store(n, jobType)

	if err := jobController.UpdateJob(ctx, hp, &jobCfg); err != nil {
		return fmt.Errorf("failed to update external algorithm job: %v", err)
	}
	return nil
}

func (r *HorizontalPortraitReconciler) cleanupLastExternalAlgorithmJob(ctx context.Context, hp *autoscalingv1alpha1.HorizontalPortrait) error {
	n := types.NamespacedName{Namespace: hp.Namespace, Name: hp.Name}
	jobType, ok := r.externalAlgorithmJobTypesMap.Load(n)
	if !ok {
		return nil
	}
	if err := r.cleanupExternalAlgorithmJob(ctx, hp, jobType.(autoscalingv1alpha1.PortraitAlgorithmJobType)); err != nil {
		return err
	}
	r.externalAlgorithmJobTypesMap.Delete(n)
	return nil
}

func (r *HorizontalPortraitReconciler) cleanupExternalAlgorithmJob(ctx context.Context, hp *autoscalingv1alpha1.HorizontalPortrait, jobType autoscalingv1alpha1.PortraitAlgorithmJobType) error {
	jobController, ok := r.ExternalAlgorithmJobControllers[jobType]
	if !ok {
		return fmt.Errorf("unknown external algorithm job type %q", jobType)
	}
	if err := jobController.CleanupJob(ctx, hp); err != nil {
		return fmt.Errorf("failed to clean up external algorithm job of type %q: %v", jobType, err)
	}
	return nil
}

func (r *HorizontalPortraitReconciler) fetchExternalAlgorithmJobResult(ctx context.Context, hp *autoscalingv1alpha1.HorizontalPortrait) (*autoscalingv1alpha1.HorizontalPortraitData, error) {
	sourceCfg := hp.Spec.Algorithm.ExternalJob.ResultSource
	fetcher, ok := r.ExternalAlgorithmJobResultFetchers[sourceCfg.Type]
	if !ok {
		return nil, fmt.Errorf("unknown external algorithm job result source type %q", sourceCfg.Type)
	}
	return fetcher.FetchResult(ctx, hp, &sourceCfg)
}

func (r *HorizontalPortraitReconciler) ensureFinalizer(ctx context.Context, obj *autoscalingv1alpha1.HorizontalPortrait) error {
	if controllerutil.ContainsFinalizer(obj, controllers.Finalizer) {
		return nil
	}
	patch := client.MergeFrom(obj.DeepCopy())
	controllerutil.AddFinalizer(obj, controllers.Finalizer)
	return r.Patch(ctx, obj, patch)
}

func (r *HorizontalPortraitReconciler) removeFinalizer(ctx context.Context, obj *autoscalingv1alpha1.HorizontalPortrait) error {
	if !controllerutil.ContainsFinalizer(obj, controllers.Finalizer) {
		return nil
	}
	patch := client.MergeFrom(obj.DeepCopy())
	controllerutil.RemoveFinalizer(obj, controllers.Finalizer)
	return r.Patch(ctx, obj, patch)
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
