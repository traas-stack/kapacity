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

package provider

import (
	"context"
	"time"

	"github.com/traas-stack/kapacity/pkg/metric"
)

// Interface provide methods to query metrics.
type Interface interface {
	// QueryLatest metrics.
	QueryLatest(ctx context.Context, query *metric.Query) ([]*metric.Sample, error)
	// Query historical metrics with arbitrary range and step.
	Query(ctx context.Context, query *metric.Query, start, end time.Time, step time.Duration) ([]*metric.Series, error)
}
