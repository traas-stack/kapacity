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

/*
 This file contains code derived from and/or modified from Kubernetes
 which is licensed under below license:

 Copyright 2015 The Kubernetes Authors.

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

package scale

import (
	"context"
	"fmt"

	k8sautoscalingv1 "k8s.io/api/autoscaling/v1"
	k8sautoscalingv2 "k8s.io/api/autoscaling/v2"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	scaleclient "k8s.io/client-go/scale"
)

// Scaler is a convenient wrapper of ScalesGetter which provides a helper method
// to get the scale of any scale target.
type Scaler struct {
	scaleclient.ScalesGetter
	restMapper apimeta.RESTMapper
}

// NewScaler creates a new Scaler with the given scales getter and rest mapper.
func NewScaler(scalesGetter scaleclient.ScalesGetter, restMapper apimeta.RESTMapper) *Scaler {
	return &Scaler{
		ScalesGetter: scalesGetter,
		restMapper:   restMapper,
	}
}

// GetScale return the scale as well as the group-resource of the specified scale target.
func (s *Scaler) GetScale(ctx context.Context, namespace string, scaleTargetRef k8sautoscalingv2.CrossVersionObjectReference) (*k8sautoscalingv1.Scale, schema.GroupResource, error) {
	targetGV, err := schema.ParseGroupVersion(scaleTargetRef.APIVersion)
	if err != nil {
		return nil, schema.GroupResource{}, fmt.Errorf("invalid API version in scale target reference: %v", err)
	}
	targetGK := schema.GroupKind{
		Group: targetGV.Group,
		Kind:  scaleTargetRef.Kind,
	}
	mappings, err := s.restMapper.RESTMappings(targetGK)
	if err != nil {
		return nil, schema.GroupResource{}, fmt.Errorf("unable to determine resource for scale target reference: %v", err)
	}
	scale, targetGR, err := s.scaleForResourceMappings(ctx, namespace, scaleTargetRef.Name, mappings)
	if err != nil {
		return nil, schema.GroupResource{}, fmt.Errorf("failed to query scale subresource: %v", err)
	}
	return scale, targetGR, nil
}

// scaleForResourceMappings attempts to fetch the scale for the resource with the given name and namespace,
// trying each RESTMapping in turn until a working one is found.
// If none work, the first error is returned.
// It returns both the scale, as well as the group-resource from the working mapping.
func (s *Scaler) scaleForResourceMappings(ctx context.Context, namespace, name string, mappings []*apimeta.RESTMapping) (*k8sautoscalingv1.Scale, schema.GroupResource, error) {
	var firstErr error
	for i, mapping := range mappings {
		targetGR := mapping.Resource.GroupResource()
		scale, err := s.Scales(namespace).Get(ctx, targetGR, name, metav1.GetOptions{})
		if err == nil {
			return scale, targetGR, nil
		}
		// if this is the first error, remember it,
		// then go on and try other mappings until we find a good one
		if i == 0 {
			firstErr = err
		}
	}
	// make sure we handle an empty set of mappings
	if firstErr == nil {
		firstErr = fmt.Errorf("unrecognized resource")
	}
	return nil, schema.GroupResource{}, firstErr
}
