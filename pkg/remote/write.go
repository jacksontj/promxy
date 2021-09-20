// Copyright 2017 The Prometheus Authors
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

package remote

import (
	"fmt"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/exemplar"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
)

// Appender implements scrape.Appendable.
func (s *Storage) Appender() (storage.Appender, error) {
	return s, nil
}

// Add implements storage.Appender.
func (s *Storage) Append(ref uint64, l labels.Labels, t int64, v float64) (uint64, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	for _, q := range s.queues {
		if err := q.Append(&model.Sample{
			Metric:    labelsToMetric(l),
			Timestamp: model.Time(t),
			Value:     model.SampleValue(v),
		}); err != nil {
			panic(err) // QueueManager.Append() should always return nil as per doc string.
		}
	}
	return 0, nil
}

func (s *Storage) AppendExemplar(ref uint64, l labels.Labels, e exemplar.Exemplar) (uint64, error) {
	return 0, fmt.Errorf("not supported")
}

// Commit implements storage.Appender.
func (*Storage) Commit() error {
	return nil
}

// Rollback implements storage.Appender.
func (*Storage) Rollback() error {
	return nil
}
