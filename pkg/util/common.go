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
	prommodel "github.com/prometheus/common/model"
)

const (
	UserAgent = "kapacity-manager"
)

// IsMapValueChanged compares two maps' values key by key, if any value in oldValues map differs
// from the one in newValues map, it returns true, otherwise returns false.
func IsMapValueChanged(oldValues, newValues map[string]string) bool {
	if len(newValues) == 0 {
		return false
	}

	for k, newV := range newValues {
		if oldV, ok := oldValues[k]; !ok || oldV != newV {
			return true
		}
	}

	return false
}

// CopyMapValues copies all the values from src map to dst map, overwriting any existing one.
func CopyMapValues(dst, src map[string]string) {
	for k, v := range src {
		dst[k] = v
	}
}

func ConvertPromLabelSetToMap(in prommodel.LabelSet) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[string(k)] = string(v)
	}
	return out
}
