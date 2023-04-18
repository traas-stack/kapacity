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
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
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
	portraitprovider "github.com/traas-stack/kapacity/pkg/portrait/provider"
	"github.com/traas-stack/kapacity/pkg/util"
)

const (
	IHPAControllerName = "ihpa_controller"
)

// IntelligentHorizontalPodAutoscalerReconciler reconciles a IntelligentHorizontalPodAutoscaler object.
type IntelligentHorizontalPodAutoscalerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	record.EventRecorder

	PortraitProviders map[autoscalingv1alpha1.HorizontalPortraitProviderType]portraitprovider.Horizontal
	EventTrigger      chan event.GenericEvent

	// portraitIdentifiersMap stores the last seen portrait identifiers of IHPAs.
	// This is used for portrait id diff to find the removed portraits.
	// The portrait identifiers stored are strings with format "providerType/portraitIdentifier".
	portraitIdentifiersMap sync.Map // map[types.NamespacedName]sets.String
}

type replicaData struct {
	OnlineReplicas  int32
	CutoffReplicas  int32
	StandbyReplicas int32
}

type replicaCap int

const (
	noneCap replicaCap = iota
	minCap
	maxCap
)

//+kubebuilder:rbac:groups=autoscaling.kapacity.traas.io,resources=intelligenthorizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=autoscaling.kapacity.traas.io,resources=intelligenthorizontalpodautoscalers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=autoscaling.kapacity.traas.io,resources=intelligenthorizontalpodautoscalers/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=autoscaling.kapacity.traas.io,resources=replicaprofiles,verbs=get;list;watch;create;patch
//+kubebuilder:rbac:groups=autoscaling.kapacity.traas.io,resources=horizontalportraits,verbs=get;list;watch;create;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *IntelligentHorizontalPodAutoscalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	l.V(2).Info("start to reconcile")

	ihpa := &autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler{}
	if err := r.Get(ctx, req.NamespacedName, ihpa); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		l.Error(err, "failed to get IntelligentHorizontalPodAutoscaler")
		return ctrl.Result{}, err
	}

	if !ihpa.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(ihpa, controllers.Finalizer) {
			// FIXME(zqzten): do actual portrait cleanup
			r.portraitIdentifiersMap.Delete(req.NamespacedName)

			if err := r.removeFinalizer(ctx, ihpa); err != nil {
				l.Error(err, "failed to remove finalizer")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if err := r.ensureFinalizer(ctx, ihpa); err != nil {
		l.Error(err, "failed to ensure finalizer")
		return ctrl.Result{}, err
	}

	ihpaStatusOriginal := ihpa.Status.DeepCopy()

	if ihpa.Spec.Paused {
		rp, err := r.getReplicaProfile(ctx, ihpa.Namespace, ihpa.Name)
		if err != nil {
			l.Error(err, "failed to get ReplicaProfile")
			r.errorOut(ctx, ihpa, ihpaStatusOriginal, autoscalingv1alpha1.ScalingActive, metav1.ConditionFalse,
				"FailedGetReplicaProfile", fmt.Sprintf("failed to get ReplicaProfile: %v", err))
			return ctrl.Result{}, err
		}

		if rp != nil && !rp.Spec.Paused {
			patch := client.MergeFrom(rp.DeepCopy())
			rp.Spec.Paused = true
			if err := r.Patch(ctx, rp, patch); err != nil {
				l.Error(err, "failed to patch ReplicaProfile")
				r.errorOut(ctx, ihpa, ihpaStatusOriginal, autoscalingv1alpha1.ScalingActive, metav1.ConditionFalse,
					"FailedUpdateReplicaProfile", fmt.Sprintf("failed to patch ReplicaProfile: %v", err))
				return ctrl.Result{}, err
			}
		}

		setIHPACondition(ihpa, autoscalingv1alpha1.ScalingActive, metav1.ConditionFalse, "ScalingPaused", "scaling is paused")
		if err := r.updateStatusIfNeeded(ctx, ihpa, ihpaStatusOriginal); err != nil {
			l.Error(err, "failed to update status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	portraitValue, err := r.updateAndFetchPortraitValue(ctx, ihpa)
	l.V(2).Info("fetched portrait value", "horizontalPortraitValue", portraitValue)
	if err != nil {
		l.Error(err, "failed to update and fetch portrait value")
		r.errorOut(ctx, ihpa, ihpaStatusOriginal, autoscalingv1alpha1.ScalingActive, metav1.ConditionFalse,
			"FailedUpdateAndFetchPortraitValue", fmt.Sprintf("failed to update and fetch portrait value: %v", err))
		return ctrl.Result{}, err
	}
	if portraitValue == nil {
		l.Info("no valid portrait value")
		r.errorOut(ctx, ihpa, ihpaStatusOriginal, autoscalingv1alpha1.ScalingActive, metav1.ConditionFalse,
			"NoValidPortraitValue", "no valid portrait value for now")
		return ctrl.Result{}, nil
	}

	if !apiequality.Semantic.DeepEqual(ihpa.Status.CurrentPortraitValue, portraitValue) {
		if ihpa.Status.CurrentPortraitValue != nil {
			ihpa.Status.PreviousPortraitValue = ihpa.Status.CurrentPortraitValue.DeepCopy()
		}
		ihpa.Status.CurrentPortraitValue = portraitValue
		if isIHPAGrayActive(ihpa) {
			ihpa.Status.Gray = &autoscalingv1alpha1.GrayStatus{LastUpdateTime: metav1.Now()}
		}
		if err := r.updateStatusIfNeeded(ctx, ihpa, ihpaStatusOriginal); err != nil {
			l.Error(err, "failed to update status")
			return ctrl.Result{}, err
		}
	}

	rp, err := r.getReplicaProfile(ctx, ihpa.Namespace, ihpa.Name)
	if err != nil {
		l.Error(err, "failed to get ReplicaProfile")
		r.errorOut(ctx, ihpa, ihpaStatusOriginal, autoscalingv1alpha1.ScalingActive, metav1.ConditionFalse,
			"FailedGetReplicaProfile", fmt.Sprintf("failed to get ReplicaProfile: %v", err))
		return ctrl.Result{}, err
	}

	var grayRemainTime time.Duration

	switch ihpa.Spec.ScaleMode {
	case autoscalingv1alpha1.ScaleModeAuto:
		var (
			replicaData *replicaData
			replicaCap  replicaCap
		)
		replicaData, replicaCap, grayRemainTime = generateIHPAReplicaData(ihpa)
		switch replicaCap {
		case noneCap:
			setIHPACondition(ihpa, autoscalingv1alpha1.ScalingLimited, metav1.ConditionFalse, "DesiredWithinRange",
				"the desired replica count of current portrait value is within the acceptable range")
		case minCap:
			setIHPACondition(ihpa, autoscalingv1alpha1.ScalingLimited, metav1.ConditionTrue, "TooFewReplicas",
				"the desired replica count of current portrait value is less than the minimum replica count")
		case maxCap:
			setIHPACondition(ihpa, autoscalingv1alpha1.ScalingLimited, metav1.ConditionTrue, "TooManyReplicas",
				"the desired replica count of current portrait value is more than the maximum replica count")
		}

		if rp == nil {
			rp = newReplicaProfile(ihpa, replicaData)

			l.Info("create ReplicaProfile", "onlineReplicas", rp.Spec.OnlineReplicas,
				"cutoffReplicas", rp.Spec.CutoffReplicas, "standbyReplicas", rp.Spec.StandbyReplicas)
			r.Event(ihpa, corev1.EventTypeNormal, "CreateReplicaProfile",
				fmt.Sprintf("create ReplicaProfile with onlineReplcas: %d, cutoffReplicas: %d, standbyReplicas: %d",
					rp.Spec.OnlineReplicas, rp.Spec.CutoffReplicas, rp.Spec.StandbyReplicas))

			if err := r.Create(ctx, rp); err != nil {
				l.Error(err, "failed to create ReplicaProfile")
				r.errorOut(ctx, ihpa, ihpaStatusOriginal, autoscalingv1alpha1.ScalingActive, metav1.ConditionFalse,
					"FailedCreateReplicaProfile", fmt.Sprintf("failed to create ReplicaProfile: %v", err))
				return ctrl.Result{}, err
			}
		} else {
			if replicaData.OnlineReplicas != rp.Spec.OnlineReplicas ||
				replicaData.CutoffReplicas != rp.Spec.CutoffReplicas ||
				replicaData.StandbyReplicas != rp.Spec.StandbyReplicas {
				l.Info("update ReplicaProfile",
					"onlineReplicas", fmt.Sprintf("%d -> %d", rp.Spec.OnlineReplicas, replicaData.OnlineReplicas),
					"cutoffReplicas", fmt.Sprintf("%d -> %d", rp.Spec.CutoffReplicas, replicaData.CutoffReplicas),
					"standbyReplicas", fmt.Sprintf("%d -> %d", rp.Spec.StandbyReplicas, replicaData.StandbyReplicas))
				r.Event(ihpa, corev1.EventTypeNormal, "UpdateReplicaProfile",
					fmt.Sprintf("update ReplicaProfile with onlineReplcas: %d -> %d, cutoffReplicas: %d -> %d, standbyReplicas: %d -> %d",
						rp.Spec.OnlineReplicas, replicaData.OnlineReplicas,
						rp.Spec.CutoffReplicas, replicaData.CutoffReplicas,
						rp.Spec.StandbyReplicas, replicaData.StandbyReplicas))
			}

			originalRP := rp.DeepCopy()
			rp.Spec.OnlineReplicas = replicaData.OnlineReplicas
			rp.Spec.CutoffReplicas = replicaData.CutoffReplicas
			rp.Spec.StandbyReplicas = replicaData.StandbyReplicas
			rp.Spec.Paused = ihpa.Spec.Paused
			if ihpa.Spec.Behavior.ReplicaProfile != nil {
				rp.Spec.Behavior = *ihpa.Spec.Behavior.ReplicaProfile
			} else {
				rp.Spec.Behavior = defaultReplicaProfileBehavior()
			}
			if !apiequality.Semantic.DeepEqual(rp.Spec, originalRP.Spec) {
				patch := client.MergeFrom(originalRP)
				if err := r.Patch(ctx, rp, patch); err != nil {
					l.Error(err, "failed to patch ReplicaProfile")
					r.errorOut(ctx, ihpa, ihpaStatusOriginal, autoscalingv1alpha1.ScalingActive, metav1.ConditionFalse,
						"FailedUpdateReplicaProfile", fmt.Sprintf("failed to patch ReplicaProfile: %v", err))
					return ctrl.Result{}, err
				}
			}
		}

		if ihpa.Status.CurrentPortraitValue.Replicas == rp.Spec.OnlineReplicas {
			setIHPACondition(ihpa, autoscalingv1alpha1.GrayProgressing, metav1.ConditionFalse, "ReplicasMatchPortrait",
				"online replica count matches replica count of current portrait value")
		} else {
			setIHPACondition(ihpa, autoscalingv1alpha1.GrayProgressing, metav1.ConditionTrue, "ReplicasNotMatchPortrait",
				"online replica count does not match replica count of current portrait value")
		}
	case autoscalingv1alpha1.ScaleModePreview:
		if rp != nil {
			if err := client.IgnoreNotFound(r.Delete(ctx, rp)); err != nil {
				l.Error(err, "failed to delete ReplicaProfile")
				r.errorOut(ctx, ihpa, ihpaStatusOriginal, autoscalingv1alpha1.ScalingActive, metav1.ConditionFalse,
					"FailedDeleteReplicaProfile", fmt.Sprintf("failed to delete ReplicaProfile: %v", err))
				return ctrl.Result{}, err
			}
		}
	}

	setIHPACondition(ihpa, autoscalingv1alpha1.ScalingActive, metav1.ConditionTrue, "SucceededAutoscaling", "the autoscaling process is functional")
	if err := r.updateStatusIfNeeded(ctx, ihpa, ihpaStatusOriginal); err != nil {
		l.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: grayRemainTime}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *IntelligentHorizontalPodAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(IHPAControllerName).
		For(&autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&autoscalingv1alpha1.HorizontalPortrait{}).
		Owns(&autoscalingv1alpha1.ReplicaProfile{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&source.Channel{Source: r.EventTrigger}, &handler.EnqueueRequestForObject{}).
		WithOptions(controller.Options{
			RecoverPanic: true,
		}).
		Complete(r)
}

func newReplicaProfile(ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler, replicaData *replicaData) *autoscalingv1alpha1.ReplicaProfile {
	behavior := defaultReplicaProfileBehavior()
	if ihpa.Spec.Behavior.ReplicaProfile != nil {
		behavior = *ihpa.Spec.Behavior.ReplicaProfile
	}

	return &autoscalingv1alpha1.ReplicaProfile{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ihpa.Namespace,
			Name:      ihpa.Name,
			OwnerReferences: []metav1.OwnerReference{
				util.BuildControllerOwnerRef(ihpa),
			},
		},
		Spec: autoscalingv1alpha1.ReplicaProfileSpec{
			ScaleTargetRef:  ihpa.Spec.ScaleTargetRef,
			OnlineReplicas:  replicaData.OnlineReplicas,
			CutoffReplicas:  replicaData.CutoffReplicas,
			StandbyReplicas: replicaData.StandbyReplicas,
			Paused:          ihpa.Spec.Paused,
			Behavior:        behavior,
		},
	}
}

func defaultReplicaProfileBehavior() autoscalingv1alpha1.ReplicaProfileBehavior {
	return autoscalingv1alpha1.ReplicaProfileBehavior{
		PodSorter: autoscalingv1alpha1.PodSorter{
			Type: autoscalingv1alpha1.WorkloadDefaultPodSorterType,
		},
		PodTrafficController: autoscalingv1alpha1.PodTrafficController{
			Type: autoscalingv1alpha1.ReadinessGatePodTrafficControllerType,
		},
	}
}

func (r *IntelligentHorizontalPodAutoscalerReconciler) updateAndFetchPortraitValue(ctx context.Context, ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler) (*autoscalingv1alpha1.HorizontalPortraitValue, error) {
	currentPortraitIdentifiers := sets.String{}
	for _, providerCfg := range ihpa.Spec.PortraitProviders {
		provider, ok := r.PortraitProviders[providerCfg.Type]
		if !ok {
			return nil, fmt.Errorf("unknown portrait provider type %q", providerCfg.Type)
		}
		currentPortraitIdentifiers.Insert(fmt.Sprintf("%s/%s", providerCfg.Type, provider.GetPortraitIdentifier(ihpa, &providerCfg)))
	}
	n := types.NamespacedName{Namespace: ihpa.Namespace, Name: ihpa.Name}
	previousPortraitIdentifiers, ok := r.portraitIdentifiersMap.Load(n)
	if ok {
		if err := r.cleanupRemovedPortraits(ctx, ihpa, previousPortraitIdentifiers.(sets.String), currentPortraitIdentifiers); err != nil {
			return nil, fmt.Errorf("failed to clean up removed portraits: %v", err)
		}
	}
	r.portraitIdentifiersMap.Store(n, currentPortraitIdentifiers)

	var (
		candidatePortraitValue    *autoscalingv1alpha1.HorizontalPortraitValue
		candidatePortraitPriority int32
	)
	now := metav1.Now()
	for _, providerCfg := range ihpa.Spec.PortraitProviders {
		provider, ok := r.PortraitProviders[providerCfg.Type]
		if !ok {
			return nil, fmt.Errorf("unknown portrait provider type %q", providerCfg.Type)
		}

		if err := provider.UpdatePortraitSpec(ctx, ihpa, &providerCfg); err != nil {
			return nil, fmt.Errorf("failed to update portrait spec: %v", err)
		}

		value, err := provider.FetchPortraitValue(ctx, ihpa, &providerCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch portrait value: %v", err)
		}
		if value == nil {
			continue
		}

		priority := providerCfg.Priority

		// The provided portrait value would be chosen as candidate only if (1 && (2 || 3 || 4)):
		// 1. It is not expired
		// 2. It is the first candidate
		// 3. Its priority is higher than the previous candidate
		// 4. Its priority is the same as the previous candidate, but it desires more replicas than the previous one
		if (value.ExpireTime.IsZero() || now.Before(&value.ExpireTime)) &&
			(candidatePortraitValue == nil ||
				(priority > candidatePortraitPriority ||
					(priority == candidatePortraitPriority && value.Replicas > candidatePortraitValue.Replicas))) {
			candidatePortraitValue = value
			candidatePortraitPriority = priority
		}
	}
	return candidatePortraitValue, nil
}

func (r *IntelligentHorizontalPodAutoscalerReconciler) cleanupRemovedPortraits(ctx context.Context, ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler, previousPortraitIdentifiers, currentPortraitIdentifiers sets.String) error {
	portraitsToCleanup := previousPortraitIdentifiers.Difference(currentPortraitIdentifiers)
	for portraitToCleanup := range portraitsToCleanup {
		typeAndIdentifier := strings.Split(portraitToCleanup, "/")
		if len(typeAndIdentifier) != 2 {
			return fmt.Errorf("invalid portrait identifier key %q", typeAndIdentifier)
		}
		providerType, portraitIdentifier := typeAndIdentifier[0], typeAndIdentifier[1]
		provider, ok := r.PortraitProviders[autoscalingv1alpha1.HorizontalPortraitProviderType(providerType)]
		if !ok {
			return fmt.Errorf("unknown portrait provider type %q", providerType)
		}
		if err := provider.CleanupPortrait(ctx, ihpa, portraitIdentifier); err != nil {
			return fmt.Errorf("failed to clean up portrait %q of provider type %q: %v", portraitIdentifier, providerType, err)
		}
	}
	return nil
}

func generateIHPAReplicaData(ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler) (*replicaData, replicaCap, time.Duration) {
	currentPortraitValue := ihpa.Status.CurrentPortraitValue

	replicaCap := noneCap
	if currentPortraitValue.Replicas < ihpa.Spec.MinReplicas {
		currentPortraitValue.Replicas = ihpa.Spec.MinReplicas
		replicaCap = minCap
	}
	if currentPortraitValue.Replicas > ihpa.Spec.MaxReplicas {
		currentPortraitValue.Replicas = ihpa.Spec.MaxReplicas
		replicaCap = maxCap
	}

	if !isIHPAGrayActive(ihpa) {
		return &replicaData{
			OnlineReplicas: currentPortraitValue.Replicas,
		}, replicaCap, 0
	}

	remainTime := calIHPAGrayRemainTime(ihpa)
	grayPercent := ihpa.Status.Gray.GrayPercent

	if grayPercent == 100 && remainTime == 0 {
		// the gray change has ended, the online replicas should match that provided by the current portrait
		return &replicaData{
			OnlineReplicas: currentPortraitValue.Replicas,
		}, replicaCap, 0
	}

	// calculate replicas of different states based on gray strategy and the process of the gray change
	grayStrategy := getCurrentGrayStrategy(ihpa)
	previousPortraitValue := ihpa.Status.PreviousPortraitValue
	changePercent := grayStrategy.ChangePercent
	newGrayPercent := grayPercent
	if remainTime == 0 {
		newGrayPercent = util.MinInt32(100, grayPercent+changePercent)
	}
	diff := util.AbsInt32(currentPortraitValue.Replicas - previousPortraitValue.Replicas)
	grayStateReplicas := util.MaxInt32(1, diff*newGrayPercent/100)

	var newOnlineReplicas, newCutoffReplicas, newStandbyReplicas int32
	if currentPortraitValue.Replicas > previousPortraitValue.Replicas {
		// scale up
		newOnlineReplicas = previousPortraitValue.Replicas + grayStateReplicas
	} else {
		// scale down
		newOnlineReplicas = previousPortraitValue.Replicas - grayStateReplicas
		switch grayStrategy.GrayState {
		case autoscalingv1alpha1.PodStateCutoff:
			newCutoffReplicas = grayStateReplicas
		case autoscalingv1alpha1.PodStateStandby:
			newStandbyReplicas = grayStateReplicas
		}
	}

	if remainTime == 0 {
		ihpa.Status.Gray = &autoscalingv1alpha1.GrayStatus{
			GrayPercent:    newGrayPercent,
			LastUpdateTime: metav1.Now(),
		}
	}
	return &replicaData{
		OnlineReplicas:  newOnlineReplicas,
		CutoffReplicas:  newCutoffReplicas,
		StandbyReplicas: newStandbyReplicas,
	}, replicaCap, remainTime
}

func isIHPAGrayActive(ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler) bool {
	if ihpa.Spec.ScaleMode == autoscalingv1alpha1.ScaleModePreview {
		return false
	}

	previousPortraitValue := ihpa.Status.PreviousPortraitValue
	currentPortraitValue := ihpa.Status.CurrentPortraitValue
	if previousPortraitValue == nil || currentPortraitValue == nil {
		return false
	}

	behavior := ihpa.Spec.Behavior
	return (behavior.ScaleUp.GrayStrategy != nil && currentPortraitValue.Replicas > previousPortraitValue.Replicas) ||
		(behavior.ScaleDown.GrayStrategy != nil && currentPortraitValue.Replicas < previousPortraitValue.Replicas)
}

func calIHPAGrayRemainTime(ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler) time.Duration {
	grayStrategy := getCurrentGrayStrategy(ihpa)
	grayPercent := ihpa.Status.Gray.GrayPercent
	lastUpdateTime := ihpa.Status.Gray.LastUpdateTime
	waitSeconds := grayStrategy.ChangeIntervalSeconds
	if grayPercent == 0 {
		// start immediately for the first change
		waitSeconds = 0
	} else if grayPercent == 100 {
		waitSeconds = grayStrategy.ObservationSeconds
	}

	remainingTime := time.Duration(waitSeconds)*time.Second - time.Since(lastUpdateTime.Time)
	if remainingTime < 0 {
		remainingTime = 0
	}
	return remainingTime
}

func getCurrentGrayStrategy(ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler) *autoscalingv1alpha1.GrayStrategy {
	previousPortraitValue := ihpa.Status.PreviousPortraitValue
	currentPortraitValue := ihpa.Status.CurrentPortraitValue
	if previousPortraitValue == nil || currentPortraitValue == nil {
		return nil
	}
	if currentPortraitValue.Replicas > previousPortraitValue.Replicas {
		return ihpa.Spec.Behavior.ScaleUp.GrayStrategy
	}
	if currentPortraitValue.Replicas < previousPortraitValue.Replicas {
		return ihpa.Spec.Behavior.ScaleDown.GrayStrategy
	}
	return nil
}

func (r *IntelligentHorizontalPodAutoscalerReconciler) getReplicaProfile(ctx context.Context, namespace, name string) (*autoscalingv1alpha1.ReplicaProfile, error) {
	rp := &autoscalingv1alpha1.ReplicaProfile{}
	err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, rp)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return rp, nil
}

func (r *IntelligentHorizontalPodAutoscalerReconciler) ensureFinalizer(ctx context.Context, obj *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler) error {
	if controllerutil.ContainsFinalizer(obj, controllers.Finalizer) {
		return nil
	}
	patch := client.MergeFrom(obj.DeepCopy())
	controllerutil.AddFinalizer(obj, controllers.Finalizer)
	return r.Patch(ctx, obj, patch)
}

func (r *IntelligentHorizontalPodAutoscalerReconciler) removeFinalizer(ctx context.Context, obj *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler) error {
	if !controllerutil.ContainsFinalizer(obj, controllers.Finalizer) {
		return nil
	}
	patch := client.MergeFrom(obj.DeepCopy())
	controllerutil.RemoveFinalizer(obj, controllers.Finalizer)
	return r.Patch(ctx, obj, patch)
}

func (r *IntelligentHorizontalPodAutoscalerReconciler) errorOut(ctx context.Context, ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler, oldStatus *autoscalingv1alpha1.IntelligentHorizontalPodAutoscalerStatus,
	conditionType autoscalingv1alpha1.IntelligentHorizontalPodAutoscalerConditionType, conditionStatus metav1.ConditionStatus, reason, message string) {
	r.Event(ihpa, corev1.EventTypeWarning, reason, message)
	setIHPACondition(ihpa, conditionType, conditionStatus, reason, message)
	if err := r.updateStatusIfNeeded(ctx, ihpa, oldStatus); err != nil {
		log.FromContext(ctx).Error(err, "failed to update status")
	}
}

func (r *IntelligentHorizontalPodAutoscalerReconciler) updateStatusIfNeeded(ctx context.Context, ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler, oldStatus *autoscalingv1alpha1.IntelligentHorizontalPodAutoscalerStatus) error {
	if apiequality.Semantic.DeepEqual(oldStatus, &ihpa.Status) {
		return nil
	}
	if err := r.Status().Update(ctx, ihpa); err != nil {
		return err
	}
	ihpa.Status.DeepCopyInto(oldStatus)
	return nil
}

func setIHPACondition(ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler, conditionType autoscalingv1alpha1.IntelligentHorizontalPodAutoscalerConditionType, status metav1.ConditionStatus, reason, message string) {
	ihpa.Status.Conditions = util.SetConditionInList(ihpa.Status.Conditions, string(conditionType), status, ihpa.Generation, reason, message)
}
