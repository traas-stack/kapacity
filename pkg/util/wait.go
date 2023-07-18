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

 Copyright 2014 The Kubernetes Authors.

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

package util

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
)

// ExponentialBackoffWithContext works similar like wait.ExponentialBackoffWithContext but with below differences:
// * It does not stop when the cap of backoff is reached.
// * It does not return the error of ctx when done.
func ExponentialBackoffWithContext(ctx context.Context, backoff wait.Backoff, condition wait.ConditionWithContextFunc) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if ok, err := runConditionWithCrashProtectionWithContext(ctx, condition); err != nil || ok {
			return err
		}

		t := time.NewTimer(backoff.Step())
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case <-t.C:
		}
	}
}

// runConditionWithCrashProtectionWithContext is copied from k8s.io/apimachinery/pkg/util/wait.
func runConditionWithCrashProtectionWithContext(ctx context.Context, condition wait.ConditionWithContextFunc) (bool, error) {
	defer runtime.HandleCrash()
	return condition(ctx)
}
