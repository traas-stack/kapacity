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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsMapValueChanged(t *testing.T) {
	oldValues := map[string]string{"key": "a"}

	// The new map is empty
	newValues := map[string]string{}
	assert.False(t, IsMapValueChanged(oldValues, newValues))

	// The new map value is changed
	newValues["key"] = "b"
	assert.True(t, IsMapValueChanged(oldValues, newValues))

	// The new map has new key-value pairs
	newValues = map[string]string{"new_key": "b"}
	assert.True(t, IsMapValueChanged(oldValues, newValues))
}

func TestCopyMapValues(t *testing.T) {
	src := map[string]string{"key_1": "a", "key_2": "c"}
	dst := map[string]string{"key_1": "b"}
	CopyMapValues(dst, src)
	for k, v := range src {
		assert.Equal(t, v, dst[k])
	}
}
