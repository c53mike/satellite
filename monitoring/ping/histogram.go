/*
Copyright 2019-2020 Gravitational, Inc.

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

package ping

import (
	"time"

	"github.com/codahale/hdrhistogram"
	"github.com/gravitational/trace"
)

const (
	// pingMinimum sets the minimum value that can be recorded
	pingMinimum = 0 * time.Second
	// pingMaximum sets the maximum value that can be recorded
	pingMaximum = 10 * time.Second
	// pingSignificantFigures specifies how many decimals should be recorded
	pingSignificantFigures = 3
)

// buildLatencyHistogram maps latencies to a HDRHistrogram
func buildLatencyHistogram(node string, latencies []int64) (latencyHDR *hdrhistogram.Histogram, err error) {
	latencyHDR = hdrhistogram.New(pingMinimum.Nanoseconds(),
		pingMaximum.Nanoseconds(), pingSignificantFigures)

	for _, v := range latencies {
		if err := latencyHDR.RecordValue(v); err != nil {
			return nil, trace.Wrap(err)
		}
	}

	return latencyHDR, nil
}
