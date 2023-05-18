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

package mutating

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/traas-stack/kapacity/internal/webhook/common"
)

// +kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,admissionReviewVersions=v1;v1beta1,groups="",resources=pods;pods/status,verbs=create;update,versions=v1,name=mpod.kb.io

var HandlerRegister = &common.HandlerRegister{
	Path: "/mutate-v1-pod",
	HandlerSetupFunc: func(mgr ctrl.Manager) (admission.Handler, error) {
		decoder, err := admission.NewDecoder(mgr.GetScheme())
		if err != nil {
			return nil, fmt.Errorf("failed to new admission decoder: %v", err)
		}
		return &Handler{
			Client:  mgr.GetClient(),
			Decoder: decoder,
		}, nil
	},
}

type Handler struct {
	client.Client
	*admission.Decoder
}

func (h *Handler) Handle(_ context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	if err := h.Decode(req, pod); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("failed to decode obj: %v", err))
	}

	var needMutate bool

	if changed := injectReadinessGate(req, pod); changed {
		needMutate = true
	}
	if changed := defaultReadinessGateStatus(req, pod); changed {
		needMutate = true
	}

	if !needMutate {
		return admission.Allowed("")
	}
	marshaled, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to marshal mutated pod: %v", err))
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaled)
}
