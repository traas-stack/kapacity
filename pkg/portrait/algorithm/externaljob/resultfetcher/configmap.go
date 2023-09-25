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

package resultfetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
)

const (
	configMapNameSuffix = "-result"

	configMapKeyType       = "type"
	configMapKeyExpireTime = "expireTime"
	configMapKeyTimeSeries = "timeSeries"
)

var configMapHorizontalHandlerLog = ctrl.Log.WithName("configmap_horizontal_result_fetcher_handler")

type ConfigMapHorizontal struct {
	client       client.Client
	eventTrigger chan event.GenericEvent
}

func NewConfigMapHorizontal(client client.Client, eventTrigger chan event.GenericEvent, configMapInformer cache.Informer) Horizontal {
	h := &ConfigMapHorizontal{
		client:       client,
		eventTrigger: eventTrigger,
	}
	configMapInformer.AddEventHandler(h)
	return h
}

func (h *ConfigMapHorizontal) FetchResult(ctx context.Context, hp *autoscalingv1alpha1.HorizontalPortrait, _ *autoscalingv1alpha1.PortraitAlgorithmResultSource) (*autoscalingv1alpha1.HorizontalPortraitData, error) {
	cmNamespacedName := types.NamespacedName{
		Namespace: hp.Namespace,
		Name:      hp.Name + configMapNameSuffix,
	}
	cm := &corev1.ConfigMap{}
	if err := h.client.Get(ctx, cmNamespacedName, cm); err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %q: %v", cmNamespacedName, err)
	}

	resultType := autoscalingv1alpha1.HorizontalPortraitDataType(cm.Data[configMapKeyType])
	if resultType == "" {
		return nil, fmt.Errorf("invalid result data: missing %q", configMapKeyType)
	}
	var expireTime *metav1.Time
	expireTimeStr := cm.Data[configMapKeyExpireTime]
	if expireTimeStr != "" {
		expireTimeRaw, err := time.Parse(time.RFC3339, expireTimeStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %q: %v", configMapKeyExpireTime, err)
		}
		expireTime = &metav1.Time{Time: expireTimeRaw}
	}
	data := &autoscalingv1alpha1.HorizontalPortraitData{
		Type:       resultType,
		ExpireTime: expireTime,
	}

	switch resultType {
	// TODO(zqzten): support other data types
	case autoscalingv1alpha1.TimeSeriesHorizontalPortraitDataType:
		timeSeriesMapStr := cm.Data[configMapKeyTimeSeries]
		if timeSeriesMapStr == "" {
			return nil, fmt.Errorf("invalid result data: missing %q", configMapKeyTimeSeries)
		}
		timeSeriesMap := make(map[string]int32)
		if err := json.Unmarshal([]byte(timeSeriesMapStr), &timeSeriesMap); err != nil {
			return nil, fmt.Errorf("failed to unmarshal %q: %v", configMapKeyTimeSeries, err)
		}

		timeSeries := make([]autoscalingv1alpha1.ReplicaTimeSeriesPoint, 0, len(timeSeriesMap))
		for k, v := range timeSeriesMap {
			ts, err := strconv.ParseInt(k, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse %q to int64 timestamp: %v", k, err)
			}
			timeSeries = append(timeSeries, autoscalingv1alpha1.ReplicaTimeSeriesPoint{
				Timestamp: ts,
				Replicas:  v,
			})
		}
		sort.Slice(timeSeries, func(i, j int) bool {
			return timeSeries[i].Timestamp < timeSeries[j].Timestamp
		})
		data.TimeSeries = &autoscalingv1alpha1.TimeSeriesHorizontalPortraitData{
			TimeSeries: timeSeries,
		}
	default:
		return nil, fmt.Errorf("unsupported result type %q", resultType)
	}

	return data, nil
}

func (h *ConfigMapHorizontal) OnAdd(obj interface{}) {
	var (
		cm *corev1.ConfigMap
		ok bool
	)
	if cm, ok = obj.(*corev1.ConfigMap); !ok {
		return
	}
	hp, err := h.getHorizontalPortraitForConfigMap(context.TODO(), cm)
	if err != nil {
		configMapHorizontalHandlerLog.Error(err, "failed to get HorizontalPortrait for ConfigMap",
			"configMapNamespace", cm.Namespace, "configMapName", cm.Name)
		return
	}
	if hp == nil {
		return
	}
	h.eventTrigger <- event.GenericEvent{Object: hp}
}

func (h *ConfigMapHorizontal) OnUpdate(oldObj, newObj interface{}) {
	var (
		cmOld, cmNew *corev1.ConfigMap
		ok           bool
	)
	if cmOld, ok = oldObj.(*corev1.ConfigMap); !ok {
		return
	}
	if cmNew, ok = newObj.(*corev1.ConfigMap); !ok {
		return
	}
	hp, err := h.getHorizontalPortraitForConfigMap(context.TODO(), cmNew)
	if err != nil {
		configMapHorizontalHandlerLog.Error(err, "failed to get HorizontalPortrait for ConfigMap",
			"configMapNamespace", cmNew.Namespace, "configMapName", cmNew.Name)
		return
	}
	if hp == nil || apiequality.Semantic.DeepEqual(cmOld.Data, cmNew.Data) {
		return
	}
	h.eventTrigger <- event.GenericEvent{Object: hp}
}

func (h *ConfigMapHorizontal) OnDelete(interface{}) {}

func (h *ConfigMapHorizontal) getHorizontalPortraitForConfigMap(ctx context.Context, cm *corev1.ConfigMap) (*autoscalingv1alpha1.HorizontalPortrait, error) {
	if !strings.Contains(cm.Name, configMapNameSuffix) {
		return nil, nil
	}
	hpNamespacedName := types.NamespacedName{
		Namespace: cm.Namespace,
		Name:      strings.TrimSuffix(cm.Name, configMapNameSuffix),
	}
	hp := &autoscalingv1alpha1.HorizontalPortrait{}
	if err := h.client.Get(ctx, hpNamespacedName, hp); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get HorizontalPortrait %q: %v", hpNamespacedName, err)
	}
	return hp, nil
}
