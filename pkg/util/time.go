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

package util

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// IsCronActive returns if the given time is in the range specified by the start cron and end cron
// as well as the next time of the end cron.
func IsCronActive(t time.Time, startCron, endCron string) (bool, time.Time, error) {
	start, err := cron.ParseStandard(startCron)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("failed to parse start cron %q: %v", startCron, err)
	}
	nextStart := start.Next(t)

	end, err := cron.ParseStandard(endCron)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("failed to parse end cron %q: %v", endCron, err)
	}
	nextEnd := end.Next(t)

	return nextStart.After(nextEnd), nextEnd, nil
}
