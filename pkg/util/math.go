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

// MaxInt32 returns the biggest one between two int32 numbers.
func MaxInt32(a, b int32) int32 {
	if a >= b {
		return a
	}
	return b
}

// MinInt32 returns the smallest one between two int32 numbers.
func MinInt32(a, b int32) int32 {
	if a <= b {
		return a
	}
	return b
}

// AbsInt32 returns the absolute value of an int32 number.
func AbsInt32(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}
