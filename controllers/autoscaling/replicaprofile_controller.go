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

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
	"github.com/traas-stack/kapacity/controllers"
	"github.com/traas-stack/kapacity/pkg/pod"
	podsorter "github.com/traas-stack/kapacity/pkg/pod/sorter"
	podtraffic "github.com/traas-stack/kapacity/pkg/pod/traffic"
	pkgscale "github.com/traas-stack/kapacity/pkg/scale"
	"github.com/traas-stack/kapacity/pkg/util"
	"github.com/traas-stack/kapacity/pkg/workload"
)

const (
	ReplicaProfileControllerName = "replica_profile_controller"
)

// ReplicaProfileReconciler reconciles a ReplicaProfile object.
type ReplicaProfileReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	record.EventRecorder
	*pkgscale.Scaler

	// podSelectorCache is used to cache pod selector by namespaced name of ReplicaProfile
	// with assumption that the selector of any workload is immutable.
	// This is for pod watch handler usage only.
	podSelectorCache sync.Map // map[types.NamespacedName]labels.Selector
}

//+kubebuilder:rbac:groups=autoscaling.kapacitystack.io,resources=replicaprofiles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=autoscaling.kapacitystack.io,resources=replicaprofiles/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=autoscaling.kapacitystack.io,resources=replicaprofiles/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=*,resources=*/scale,verbs=get;update
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;patch
//+kubebuilder:rbac:groups="",resources=pods/status,verbs=get;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ReplicaProfileReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	l.V(2).Info("start to reconcile")

	rp := &autoscalingv1alpha1.ReplicaProfile{}
	if err := r.Get(ctx, req.NamespacedName, rp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		l.Error(err, "failed to get ReplicaProfile")
		return ctrl.Result{}, err
	}

	if !rp.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(rp, controllers.Finalizer) {
			// cleanup the cache
			r.podSelectorCache.Delete(req.NamespacedName)

			if err := r.removeFinalizer(ctx, rp); err != nil {
				l.Error(err, "failed to remove finalizer")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if err := r.ensureFinalizer(ctx, rp); err != nil {
		l.Error(err, "failed to ensure finalizer")
		return ctrl.Result{}, err
	}

	rpStatusOriginal := rp.Status.DeepCopy()

	if rp.Spec.Paused {
		setReplicaProfileCondition(rp, autoscalingv1alpha1.ReplicaProfileApplied, metav1.ConditionFalse,
			"ReplicaProfilePaused", "the replica profile is paused")
		setReplicaProfileCondition(rp, autoscalingv1alpha1.ReplicaProfileEnsured, metav1.ConditionFalse,
			"ReplicaProfilePaused", "the replica profile is paused")
		if err := r.updateStatusIfNeeded(ctx, rp, rpStatusOriginal); err != nil {
			l.Error(err, "failed to update status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Get target's scale
	scale, targetGR, err := r.GetScale(ctx, rp.Namespace, rp.Spec.ScaleTargetRef)
	if err != nil {
		l.Error(err, "failed to get the target's current scale")
		r.errorOut(ctx, rp, rpStatusOriginal, autoscalingv1alpha1.ReplicaProfileEnsured, metav1.ConditionFalse,
			"FailedGetScale", fmt.Sprintf("failed to get the target's current scale: %v", err))
		return ctrl.Result{}, err
	}
	selector, err := util.ParseScaleSelector(scale.Status.Selector)
	if err != nil {
		l.Error(err, "failed to parse label selector of scale", "selector", scale.Status.Selector)
		r.errorOut(ctx, rp, rpStatusOriginal, autoscalingv1alpha1.ReplicaProfileEnsured, metav1.ConditionFalse,
			"InvalidSelector", fmt.Sprintf("failed to parse label selector %q of scale: %v", scale.Status.Selector, err))
		return ctrl.Result{}, err
	}
	// Cache selector for pod watch handler, the reconciler itself won't use this cache
	// because it has to fetch the current scale everytime.
	r.podSelectorCache.LoadOrStore(req.NamespacedName, selector)

	// Get target's pod states
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.InNamespace(rp.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		l.Error(err, "failed to list the target's Pods")
		r.errorOut(ctx, rp, rpStatusOriginal, autoscalingv1alpha1.ReplicaProfileEnsured, metav1.ConditionFalse,
			"FailedListPods", fmt.Sprintf("failed to list the target's Pods: %v", err))
		return ctrl.Result{}, err
	}
	currentRunningPods, currentRunningPodCount := pod.FilterAndClassifyByRunningState(podList.Items)

	// Reflect status & check if ensured
	rp.Status.OnlineReplicas = int32(len(currentRunningPods[autoscalingv1alpha1.PodStateOnline]))
	rp.Status.CutoffReplicas = int32(len(currentRunningPods[autoscalingv1alpha1.PodStateCutoff]))
	rp.Status.StandbyReplicas = int32(len(currentRunningPods[autoscalingv1alpha1.PodStateStandby]))

	ensured := rp.Status.OnlineReplicas == rp.Spec.OnlineReplicas &&
		rp.Status.CutoffReplicas == rp.Spec.CutoffReplicas &&
		rp.Status.StandbyReplicas == rp.Spec.StandbyReplicas
	if ensured {
		setReplicaProfileCondition(rp, autoscalingv1alpha1.ReplicaProfileEnsured, metav1.ConditionTrue,
			"ReplicaProfileEnsured", "current replica profile is ensured")
	} else {
		setReplicaProfileCondition(rp, autoscalingv1alpha1.ReplicaProfileEnsured, metav1.ConditionFalse,
			"ReplicaNumberNotSatisfied", "the number of replicas of some states do not meet the expectation")
	}

	if err := r.updateStatusIfNeeded(ctx, rp, rpStatusOriginal); err != nil {
		l.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}
	if ensured {
		return ctrl.Result{}, nil
	}

	// We need state change only if we want some pods to be not online, or there exists pods whose state is not online.
	needStateChange := rp.Spec.CutoffReplicas > 0 || rp.Spec.StandbyReplicas > 0 ||
		rp.Status.CutoffReplicas > 0 || rp.Status.StandbyReplicas > 0
	// Do state change only if the number of current running pods match the desired replicas of the target workload,
	// this can prevent unexpected behaviors when the target workload is updating/self-healing.
	canDoStateChange := currentRunningPodCount == int(scale.Spec.Replicas)
	if needStateChange && canDoStateChange {
		w, err := r.getWorkload(rp, selector)
		if err != nil {
			l.Error(err, "failed to get target workload")
			r.errorOut(ctx, rp, rpStatusOriginal, autoscalingv1alpha1.ReplicaProfileApplied, metav1.ConditionFalse,
				"FailedGetWorkload", fmt.Sprintf("failed to get the target workload: %v", err))
			return ctrl.Result{}, err
		}

		var podSorter podsorter.Interface = w
		if rp.Spec.Behavior.PodSorter.Type != autoscalingv1alpha1.WorkloadDefaultPodSorterType {
			if !w.CanSelectPodsToScaleDown(ctx) {
				l.V(1).Info("the target workload does not support non default pod sorter")
				r.errorOut(ctx, rp, rpStatusOriginal, autoscalingv1alpha1.ReplicaProfileApplied, metav1.ConditionFalse,
					"PodSorterNotSupportedByWorkload", "the target workload does not support non default pod sorter")
				return ctrl.Result{}, nil
			}
			podSorter, err = r.getPodSorter(rp)
			if err != nil {
				l.Error(err, "failed to get pod sorter")
				r.errorOut(ctx, rp, rpStatusOriginal, autoscalingv1alpha1.ReplicaProfileApplied, metav1.ConditionFalse,
					"FailedGetPodSorter", fmt.Sprintf("failed to get pod sorter: %v", err))
				return ctrl.Result{}, err
			}
		}

		podTrafficController, err := r.getPodTrafficController(rp)
		if err != nil {
			l.Error(err, "failed to get pod traffic controller")
			r.errorOut(ctx, rp, rpStatusOriginal, autoscalingv1alpha1.ReplicaProfileApplied, metav1.ConditionFalse,
				"FailedGetPodTrafficController", fmt.Sprintf("failed to get pod traffic controller: %v", err))
			return ctrl.Result{}, err
		}

		sm := pod.NewStateManager(rp, podSorter, currentRunningPods)
		change, err := sm.CalculateStateChange(ctx)
		if err != nil {
			l.Error(err, "failed to calculate state change")
			r.errorOut(ctx, rp, rpStatusOriginal, autoscalingv1alpha1.ReplicaProfileApplied, metav1.ConditionFalse,
				"FailedCalculateStateChange", fmt.Sprintf("failed to calculate state change: %v", err))
			return ctrl.Result{}, err
		}

		if len(change.Online) > 0 {
			podNames := util.GetPodNames(change.Online)
			l.Info("online pods", "pods", podNames)
			r.Eventf(rp, corev1.EventTypeNormal, "OnlinePods", "online pods: %v", podNames)

			if err := podTrafficController.On(ctx, change.Online); err != nil {
				l.Error(err, "failed to online pods")
				r.errorOut(ctx, rp, rpStatusOriginal, autoscalingv1alpha1.ReplicaProfileApplied, metav1.ConditionFalse,
					"FailedOnlinePods", fmt.Sprintf("failed to online pods: %v", err))
				return ctrl.Result{}, err
			}

			// TODO(zqzten): use a concurrent way to set pod state
			for _, p := range change.Online {
				if err := r.setPodState(ctx, p, autoscalingv1alpha1.PodStateOnline); err != nil {
					l.Error(err, "failed to mark pod online", "pod", p.Name)
					r.errorOut(ctx, rp, rpStatusOriginal, autoscalingv1alpha1.ReplicaProfileApplied, metav1.ConditionFalse,
						"FailedSetPodState", fmt.Sprintf("failed to mark pod %q online: %v", p.Name, err))
					return ctrl.Result{}, err
				}
			}
		}

		if len(change.Cutoff) > 0 {
			podNames := util.GetPodNames(change.Cutoff)
			l.Info("cutoff pods", "pods", podNames)
			r.Eventf(rp, corev1.EventTypeNormal, "CutoffPods", "cutoff pods: %v", podNames)

			if err := podTrafficController.Off(ctx, change.Cutoff); err != nil {
				l.Error(err, "failed to cutoff pods")
				r.errorOut(ctx, rp, rpStatusOriginal, autoscalingv1alpha1.ReplicaProfileApplied, metav1.ConditionFalse,
					"FailedCutoffPods", fmt.Sprintf("failed to cutoff pods: %v", err))
				return ctrl.Result{}, err
			}

			// TODO(zqzten): use a concurrent way to set pod state
			for _, p := range change.Cutoff {
				if err := r.setPodState(ctx, p, autoscalingv1alpha1.PodStateCutoff); err != nil {
					l.Error(err, "failed to mark pod cutoff", "pod", p.Name)
					r.errorOut(ctx, rp, rpStatusOriginal, autoscalingv1alpha1.ReplicaProfileApplied, metav1.ConditionFalse,
						"FailedSetPodState", fmt.Sprintf("failed to mark pod %q cutoff: %v", p.Name, err))
					return ctrl.Result{}, err
				}
			}
		}

		if len(change.Standby) > 0 {
			podNames := util.GetPodNames(change.Standby)
			l.Info("standby pods", "pods", podNames)
			r.Eventf(rp, corev1.EventTypeNormal, "StandbyPods", "standby pods: %v", podNames)

			// TODO(zqzten) support standby

			// TODO(zqzten): use a concurrent way to set pod state
			for _, p := range change.Standby {
				if err := r.setPodState(ctx, p, autoscalingv1alpha1.PodStateStandby); err != nil {
					l.Error(err, "failed to mark pod standby", "pod", p.Name)
					r.errorOut(ctx, rp, rpStatusOriginal, autoscalingv1alpha1.ReplicaProfileApplied, metav1.ConditionFalse,
						"FailedSetPodState", fmt.Sprintf("failed to mark pod %q standby: %v", p.Name, err))
					return ctrl.Result{}, err
				}
			}
		}

		if len(change.Delete) > 0 && w.CanSelectPodsToScaleDown(ctx) {
			podNames := util.GetPodNames(change.Delete)
			l.Info("select pods to delete", "pods", podNames)
			r.Eventf(rp, corev1.EventTypeNormal, "SelectPodsToDelete", "select pods to delete: %v", podNames)

			if err := w.SelectPodsToScaleDown(ctx, change.Delete); err != nil {
				l.Error(err, "failed to select pods to delete")
				r.errorOut(ctx, rp, rpStatusOriginal, autoscalingv1alpha1.ReplicaProfileApplied, metav1.ConditionFalse,
					"FailedSelectPodsToDelete", fmt.Sprintf("failed to select pods to delete: %v", err))
				return ctrl.Result{}, err
			}
		}
	} else {
		l.Info("state change skipped as the number of current running pods observed mismatch the desired replicas of the target workload",
			"currentRunningPodsCount", currentRunningPodCount, "workloadDesiredReplicas", scale.Spec.Replicas)
	}

	// Scale replicas if needed
	desiredReplicas := rp.Spec.OnlineReplicas + rp.Spec.CutoffReplicas + rp.Spec.StandbyReplicas
	if desiredReplicas != scale.Spec.Replicas {
		l.Info("rescale target workload", "oldReplicas", scale.Spec.Replicas, "newReplicas", desiredReplicas)
		r.Eventf(rp, corev1.EventTypeNormal, "UpdateScale", "rescale target workload from %d to %d replicas", scale.Spec.Replicas, desiredReplicas)

		scale.Spec.Replicas = desiredReplicas
		_, err = r.Scales(rp.Namespace).Update(ctx, targetGR, scale, metav1.UpdateOptions{})
		if err != nil {
			l.Error(err, "failed to update the target's scale")
			r.errorOut(ctx, rp, rpStatusOriginal, autoscalingv1alpha1.ReplicaProfileApplied, metav1.ConditionFalse,
				"FailedUpdateScale", fmt.Sprintf("failed to update the target's scale: %v", err))
			return ctrl.Result{}, err
		}
	}

	if !needStateChange || canDoStateChange {
		setReplicaProfileCondition(rp, autoscalingv1alpha1.ReplicaProfileApplied, metav1.ConditionTrue,
			"ReplicaProfileApplied", "all the operations for ensuring the replica profile are applied")
	} else {
		setReplicaProfileCondition(rp, autoscalingv1alpha1.ReplicaProfileApplied, metav1.ConditionFalse,
			"StateChangeSkipped", "state change skipped as the number of current running pods observed mismatch the desired replicas of the target workload")
	}
	if err := r.updateStatusIfNeeded(ctx, rp, rpStatusOriginal); err != nil {
		l.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ReplicaProfileReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	ctx = log.IntoContext(ctx, mgr.GetLogger().WithValues("controller", ReplicaProfileControllerName))
	return ctrl.NewControllerManagedBy(mgr).
		Named(ReplicaProfileControllerName).
		For(&autoscalingv1alpha1.ReplicaProfile{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&source.Kind{Type: &corev1.Pod{}},
			handler.EnqueueRequestsFromMapFunc(func(object client.Object) []reconcile.Request {
				if n := r.findReplicaProfileToEnqueueForPod(ctx, object); n != nil {
					return []reconcile.Request{{NamespacedName: *n}}
				}
				return []reconcile.Request{}
			}),
			builder.WithPredicates(podStateChangePredicate{}),
		).
		WithOptions(controller.Options{
			RecoverPanic: true,
		}).
		Complete(r)
}

type podStateChangePredicate struct {
	predicate.ResourceVersionChangedPredicate
}

func (p podStateChangePredicate) Update(e event.UpdateEvent) bool {
	if !p.ResourceVersionChangedPredicate.Update(e) {
		return false
	}
	var (
		podOld, podNew *corev1.Pod
		ok             bool
	)
	if podOld, ok = e.ObjectOld.(*corev1.Pod); !ok {
		return false
	}
	if podNew, ok = e.ObjectNew.(*corev1.Pod); !ok {
		return false
	}
	return pod.StateChanged(podOld, podNew)
}

func (r *ReplicaProfileReconciler) findReplicaProfileToEnqueueForPod(ctx context.Context, pod client.Object) *types.NamespacedName {
	l := log.FromContext(ctx).WithValues("namespace", pod.GetNamespace())

	rps := &autoscalingv1alpha1.ReplicaProfileList{}
	if err := r.List(ctx, rps, client.InNamespace(pod.GetNamespace())); err != nil {
		l.Error(err, "failed to list ReplicaProfile")
		return nil
	}

	for _, rp := range rps.Items {
		n := types.NamespacedName{Namespace: rp.Namespace, Name: rp.Name}
		selectorObj, ok := r.podSelectorCache.Load(n)
		if !ok {
			// the selector has not been cached, which means this ReplicaProfile has not been reconciled
			// for the first time, so it's ok to ignore here since every ReplicaProfile
			// would be reconciled at least once eventually
			continue
		}
		selector := selectorObj.(labels.Selector)
		if selector.Matches(labels.Set(pod.GetLabels())) {
			// return the first found as we assume a pod would not be managed by multiple ReplicaProfiles
			// if the ReplicaProfile is being deleted or paused, there's no need to enqueue
			if !rp.DeletionTimestamp.IsZero() || rp.Spec.Paused {
				return nil
			}
			return &n
		}
	}

	return nil
}

func (r *ReplicaProfileReconciler) getWorkload(rp *autoscalingv1alpha1.ReplicaProfile, selector labels.Selector) (workload.Interface, error) {
	gvk, err := util.ParseGVK(rp.Spec.ScaleTargetRef.APIVersion, rp.Spec.ScaleTargetRef.Kind)
	if err != nil {
		return nil, fmt.Errorf("failed to parse scale target's gvk: %v", err)
	}
	switch {
	case gvk.Group == "apps" && gvk.Kind == "ReplicaSet":
		return &workload.ReplicaSet{
			Client:    r.Client,
			Namespace: rp.Namespace,
			Selector:  selector,
		}, nil
	case gvk.Group == "apps" && gvk.Kind == "Deployment":
		return &workload.Deployment{
			Client:    r.Client,
			Namespace: rp.Namespace,
			Selector:  selector,
		}, nil
	case gvk.Group == "apps" && gvk.Kind == "StatefulSet":
		return &workload.StatefulSet{}, nil
	// TODO(zqzten): support external workload
	default:
		return nil, fmt.Errorf("unknown workload with gvk \"%v\"", gvk)
	}
}

func (r *ReplicaProfileReconciler) getPodSorter(rp *autoscalingv1alpha1.ReplicaProfile) (podsorter.Interface, error) {
	cfg := rp.Spec.Behavior.PodSorter
	switch cfg.Type {
	// TODO(zqzten): support more pod sorters
	case autoscalingv1alpha1.ExternalPodSorterType:
		// TODO(zqzten): support this
		return nil, fmt.Errorf("todo")
	default:
		return nil, fmt.Errorf("unknown pod sorter type %q", cfg.Type)
	}
}

func (r *ReplicaProfileReconciler) getPodTrafficController(rp *autoscalingv1alpha1.ReplicaProfile) (podtraffic.Controller, error) {
	cfg := rp.Spec.Behavior.PodTrafficController
	switch cfg.Type {
	case autoscalingv1alpha1.ReadinessGatePodTrafficControllerType:
		return &podtraffic.ReadinessGate{
			Client: r.Client,
		}, nil
	case autoscalingv1alpha1.ExternalPodTrafficControllerType:
		// TODO(zqzten): support this
		return nil, fmt.Errorf("todo")
	default:
		return nil, fmt.Errorf("unknown pod traffic controller type %q", cfg.Type)
	}
}

func (r *ReplicaProfileReconciler) ensureFinalizer(ctx context.Context, obj *autoscalingv1alpha1.ReplicaProfile) error {
	if controllerutil.ContainsFinalizer(obj, controllers.Finalizer) {
		return nil
	}
	patch := client.MergeFrom(obj.DeepCopy())
	controllerutil.AddFinalizer(obj, controllers.Finalizer)
	return r.Patch(ctx, obj, patch)
}

func (r *ReplicaProfileReconciler) removeFinalizer(ctx context.Context, obj *autoscalingv1alpha1.ReplicaProfile) error {
	if !controllerutil.ContainsFinalizer(obj, controllers.Finalizer) {
		return nil
	}
	patch := client.MergeFrom(obj.DeepCopy())
	controllerutil.RemoveFinalizer(obj, controllers.Finalizer)
	return r.Patch(ctx, obj, patch)
}

func (r *ReplicaProfileReconciler) errorOut(ctx context.Context, rp *autoscalingv1alpha1.ReplicaProfile, oldStatus *autoscalingv1alpha1.ReplicaProfileStatus,
	conditionType autoscalingv1alpha1.ReplicaProfileConditionType, conditionStatus metav1.ConditionStatus, reason, message string) {
	r.Event(rp, corev1.EventTypeWarning, reason, message)
	setReplicaProfileCondition(rp, conditionType, conditionStatus, reason, message)
	if err := r.updateStatusIfNeeded(ctx, rp, oldStatus); err != nil {
		log.FromContext(ctx).Error(err, "failed to update status")
	}
}

func (r *ReplicaProfileReconciler) updateStatusIfNeeded(ctx context.Context, rp *autoscalingv1alpha1.ReplicaProfile, oldStatus *autoscalingv1alpha1.ReplicaProfileStatus) error {
	if apiequality.Semantic.DeepEqual(oldStatus, &rp.Status) {
		return nil
	}
	if err := r.Status().Update(ctx, rp); err != nil {
		return err
	}
	rp.Status.DeepCopyInto(oldStatus)
	return nil
}

func (r *ReplicaProfileReconciler) setPodState(ctx context.Context, p *corev1.Pod, state autoscalingv1alpha1.PodState) error {
	patch := client.MergeFrom(p.DeepCopy())
	pod.SetState(p, state)
	return r.Patch(ctx, p, patch)
}

func setReplicaProfileCondition(rp *autoscalingv1alpha1.ReplicaProfile, conditionType autoscalingv1alpha1.ReplicaProfileConditionType, status metav1.ConditionStatus, reason, message string) {
	rp.Status.Conditions = util.SetConditionInList(rp.Status.Conditions, string(conditionType), status, rp.Generation, reason, message)
}
