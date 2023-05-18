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

package common

import (
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// HandlerSetupFunc sets up an admission handler with the manager.
type HandlerSetupFunc func(ctrl.Manager) (admission.Handler, error)

// HandlerRegister contains information about how to register an admission handler
// to a specific path of the webhook server.
type HandlerRegister struct {
	Path string
	HandlerSetupFunc
}

// RegisterToServerWithManager sets up an admission handler and
// registers it to the specified path of the webhook server.
func (r *HandlerRegister) RegisterToServerWithManager(srv *webhook.Server, mgr ctrl.Manager) error {
	handler, err := r.HandlerSetupFunc(mgr)
	if err != nil {
		return fmt.Errorf("failed to set up handler: %v", err)
	}
	srv.Register(r.Path, &webhook.Admission{Handler: handler})
	return nil
}
