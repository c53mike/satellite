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
	"sync"

	"github.com/gravitational/trace"
	"github.com/gravitational/ttlmap/v2"
)

const (
	// rttStatsCapacity sets the number of TTLMaps that can be stored; this will be the size of the cluster -1
	rttStatsCapacity = 1000
	// rttStatsTTLSeconds specifies how long check results will be kept before being dropped
	rttStatsTTLSeconds = 300 // 5 minutes
	// rttStatsSlidingWindowSize specifies the number of retained check results
	rttStatsSlidingWindowSize = 20
)

// rttCache caches the rtt stats for nodes in the cluster.
// Exported methods are safe for concurrent use.
type rttCache struct {
	sync.Mutex
	rttStats *ttlmap.TTLMap
}

// newRTTCache constructs a new rttCache.
func newRTTCache() *rttCache {
	return &rttCache{
		rttStats: ttlmap.NewTTLMap(rttStatsCapacity),
	}
}

// Get gets the cached RTT stats for the node specified by the provided name.
func (r *rttCache) Get(name string) (rtts []int64, err error) {
	r.Lock()
	defer r.Unlock()
	return r.get(name)
}

func (r *rttCache) get(name string) (rtts []int64, err error) {
	value, exists := r.rttStats.Get(name)
	if !exists {
		return rtts, trace.NotFound("no RTT stats found for node %s", name)
	}

	rtts, ok := value.([]int64)
	if !ok {
		return rtts, trace.BadParameter("couldn't parse node RTT as []int64 on %s", name)
	}

	return rtts, nil
}

// Set sets the RTT stats for the node specified by the provided name.
func (r *rttCache) Set(name string, rtts []int64) error {
	r.Lock()
	defer r.Unlock()
	return r.set(name, rtts)
}

func (r *rttCache) set(name string, rtts []int64) error {
	if err := r.rttStats.Set(name, rtts, rttStatsTTLSeconds); err != nil {
		return trace.Wrap(err, "failed to set RTTs stats")
	}
	return nil
}

// Append appends the RTT to the RTT stats of the node specified by
// the provided name.
func (r *rttCache) Append(name string, rtt int64) error {
	r.Lock()
	defer r.Unlock()

	rtts, err := r.get(name)
	if err != nil && !trace.IsNotFound(err) {
		return trace.Wrap(err, "failed to get RTT stats for %s", name)
	}
	if trace.IsNotFound(err) {
		rtts = make([]int64, rttStatsSlidingWindowSize)
	}

	if len(rtts) >= rttStatsSlidingWindowSize {
		// keep the slice within the sliding window size
		// slidingWindowSize is -1 because another element will be added a few lines below
		rtts = rtts[1:rttStatsSlidingWindowSize]
	}

	rtts = append(rtts, rtt)

	err = r.set(name, rtts)
	if err != nil {
		return trace.Wrap(err, "failed to set RTT stats for %s", name)
	}

	return nil
}
