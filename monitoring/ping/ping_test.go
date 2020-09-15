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
	"sort"
	"testing"
	"time"

	"github.com/gravitational/satellite/agent/health"
	"github.com/gravitational/satellite/lib/membership"
	"github.com/gravitational/satellite/lib/test"

	"github.com/gravitational/trace"
	"github.com/jonboulle/clockwork"
	"github.com/sirupsen/logrus"
	. "gopkg.in/check.v1"
)

func TestPing(t *testing.T) { TestingT(t) }

type PingSuite struct {
	clock  clockwork.Clock
	logger logrus.FieldLogger
}

var _ = Suite(&PingSuite{})

func (r *PingSuite) SetUpSuite(c *C) {
	r.clock = clockwork.NewFakeClock()
	r.logger = logrus.WithField(trace.Component, checkerID)
}

// TODO: update test cases to test entire Check functionality. Have not figured
// out how to simulate latency without slowing down the tests. For now just
// testing the verification step of the checker.

// TestVerifyPing verifies the checker can correctly label low or high ping.
func (r *PingSuite) TestVerifyPing(c *C) {
	tests := []struct {
		comment  string
		cache    *rttCache
		latest   int64
		expected health.Probes
	}{
		{
			comment: "All recorded values are at the threshold",
			cache:   r.newRTTCache(node2, rttThreshold.Nanoseconds()),
			latest:  rttThreshold.Nanoseconds(),
		},
		{
			comment: "Some recorded values are under the threshold",
			cache:   r.newRTTCache(node2, underThreshold.Nanoseconds()),
			latest:  aboveThreshold.Nanoseconds(),
		},
		{
			comment: "Latest recorded value is under the threshold",
			cache:   r.newRTTCache(node2, aboveThreshold.Nanoseconds()),
			latest:  underThreshold.Nanoseconds(),
		},
		{
			comment: "All recorded values are above the threshold",
			cache:   r.newRTTCache(node2, aboveThreshold.Nanoseconds()),
			latest:  aboveThreshold.Nanoseconds(),
			expected: health.Probes{
				failureProbe(node1, node2, aboveThreshold, rttThreshold),
			},
		},
	}
	for _, tc := range tests {
		comment := Commentf(tc.comment)
		checker := &checker{
			Config: &Config{
				NodeName:     node1,
				RTTThreshold: rttThreshold,
			},
			FieldLogger: r.logger,
			rttStats:    tc.cache,
		}

		var probes health.Probes
		c.Assert(checker.rttStats.Append(node2, tc.latest), IsNil, comment)
		c.Assert(checker.verifyRTTStats(&probes, []membership.Member{{Name: node2}}), IsNil, comment)
		sort.Sort(health.ByDetail(probes))
		c.Assert(probes, test.DeepCompare, tc.expected, comment)
	}
}

// newRTTCache initializes a new rttCache for the specified node. The entire
// window of RTTs will be filled with the specified rtt value.
func (r *PingSuite) newRTTCache(node string, rtt int64) *rttCache {
	cache := newRTTCache()
	stats := make([]int64, rttStatsSlidingWindowSize)
	for i := 0; i < len(stats); i++ {
		stats[i] = rtt
	}
	cache.Set(node, stats)
	return cache
}

const (
	// pair of nodes used for test cases
	node1 = "node-1"
	node2 = "node-2"

	// arbitrary values above and below the RTT threshold.
	aboveThreshold = rttThreshold + time.Millisecond
	underThreshold = rttThreshold - time.Millisecond
)
