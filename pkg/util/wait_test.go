/*
 Copyright 2023 The Kapacity Authors.
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
	"errors"
	"math"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

func TestExponentialBackoffWithContext(t *testing.T) {
	defaultCtx := func() (context.Context, context.CancelFunc) {
		return context.WithCancel(context.Background())
	}

	defaultCallback := func(_ int) (bool, error) {
		return false, nil
	}

	conditionErr := errors.New("condition failed")

	tests := []struct {
		name             string
		cap              time.Duration
		ctxGetter        func() (context.Context, context.CancelFunc)
		callback         func(calls int) (bool, error)
		attemptsExpected int
		errExpected      error
	}{
		{
			name:      "condition returns true with single backoff step",
			ctxGetter: defaultCtx,
			callback: func(_ int) (bool, error) {
				return true, nil
			},
			attemptsExpected: 1,
			errExpected:      nil,
		},
		{
			name:             "condition always returns false with multiple backoff steps",
			ctxGetter:        defaultCtx,
			callback:         defaultCallback,
			attemptsExpected: 5,
			errExpected:      nil,
		},
		{
			name:      "condition returns true after certain attempts with multiple backoff steps",
			ctxGetter: defaultCtx,
			callback: func(attempts int) (bool, error) {
				if attempts == 3 {
					return true, nil
				}
				return false, nil
			},
			attemptsExpected: 3,
			errExpected:      nil,
		},
		{
			name:      "condition returns error no further attempts expected",
			ctxGetter: defaultCtx,
			callback: func(_ int) (bool, error) {
				return true, conditionErr
			},
			attemptsExpected: 1,
			errExpected:      conditionErr,
		},
		{
			name: "context already canceled no attempts expected",
			ctxGetter: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				return ctx, cancel
			},
			callback:         defaultCallback,
			attemptsExpected: 0,
			errExpected:      nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backoff := wait.Backoff{
				Duration: 10 * time.Millisecond,
				Factor:   2,
				Steps:    math.MaxInt32,
				Cap:      40 * time.Millisecond,
			}

			ctx, cancel := test.ctxGetter()

			go func() {
				time.Sleep(120 * time.Millisecond)
				cancel()
			}()

			attempts := 0
			err := ExponentialBackoffWithContext(ctx, backoff, func(context.Context) (bool, error) {
				attempts++
				return test.callback(attempts)
			})

			if !errors.Is(err, test.errExpected) {
				t.Errorf("expected error: %v but got: %v", test.errExpected, err)
			}

			if test.attemptsExpected != attempts {
				t.Errorf("expected attempts count: %d but got: %d", test.attemptsExpected, attempts)
			}
		})
	}
}
