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

func TestMaxInt32(t *testing.T) {
	var min, max int32 = 1, 2
	value := MaxInt32(min, max)
	assert.Equal(t, max, value)
}

func TestMinInt32(t *testing.T) {
	var min, max int32 = 1, 2
	value := MinInt32(min, max)
	assert.Equal(t, min, value)
}

func TestAbsInt32(t *testing.T) {
	var positiveNum, negativeNum, zero int32 = 1, -1, 0

	value := AbsInt32(positiveNum)
	assert.Equal(t, positiveNum, value)

	value = AbsInt32(negativeNum)
	assert.Equal(t, -negativeNum, value)

	value = AbsInt32(zero)
	assert.Equal(t, zero, value)
}
