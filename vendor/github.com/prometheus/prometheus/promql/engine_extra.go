// Copyright 2023 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package promql

import (
	"time"

	"github.com/prometheus/prometheus/promql/parser"
)

func findPathRange(path []parser.Node, eRanges []evalRange) time.Duration {
	var (
		evalRange time.Duration
		depth     = -1
	)
	for _, r := range eRanges {
		// If the prefix is longer then it can't be the parent of `child`
		if len(r.Prefix) > len(path) {
			continue
		}

		// Check if we are a child
		child := true
		for i, p := range r.Prefix {
			if p != path[i] {
				child = false
				break
			}
		}
		if child && len(r.Prefix) > depth {
			evalRange = r.Range
			depth = len(r.Prefix)
		}
	}

	return evalRange
}

// evalRange summarizes a defined evalRange (from a MatrixSelector) within the ast
//
// Prefix identifies the ancestor chain by node pointer rather than by
// PositionRange: parser.Walk fans siblings out when a NodeReplacer is
// installed, and PositionRange() recurses into a node's children, so reading
// it here would race with the SetChild calls a sibling subtree performs.
type evalRange struct {
	Prefix []parser.Node
	Range  time.Duration
}
