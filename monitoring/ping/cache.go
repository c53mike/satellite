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
	// latencyStatsCapacity sets the number of TTLMaps that can be stored; this will be the size of the cluster -1
	latencyStatsCapacity = 1000
	// latencyStatsTTLSeconds specifies how long check results will be kept before being dropped
	latencyStatsTTLSeconds = 3600 // 1 hour
	// latencyStatsSlidingWindowSize specifies the number of retained check results
	latencyStatsSlidingWindowSize = 20
)

// latencyCache caches the latency stats for nodes in the cluster.
type latencyCache struct {
	sync.Mutex
	latencyStats *ttlmap.TTLMap
}

// newLatencyCache constructs a new latencyCache.
func newLatencyCache() *latencyCache {
	return &latencyCache{
		latencyStats: ttlmap.NewTTLMap(latencyStatsCapacity),
	}
}

// Get gets the cached latency stats for the node specified by the provided name.
func (r *latencyCache) Get(name string) (latencies []int64, err error) {
	r.Lock()
	defer r.Unlock()
	return r.get(name)
}

func (r *latencyCache) get(name string) (latencies []int64, err error) {
	value, exists := r.latencyStats.Get(name)
	if !exists {
		return latencies, trace.NotFound("no latency stats found for node %q", name)
	}

	latencies, ok := value.([]int64)
	if !ok {
		return latencies, trace.BadParameter("couldn't parse node latency as []int64 on %q", name)
	}

	return latencies, nil
}

// Set sets the latency stats for the node specified by the provided name.
func (r *latencyCache) Set(name string, latencies []int64) error {
	r.Lock()
	defer r.Unlock()
	return r.set(name, latencies)
}

func (r *latencyCache) set(name string, latencies []int64) error {
	if err := r.latencyStats.Set(name, latencies, latencyStatsTTLSeconds); err != nil {
		return trace.Wrap(err, "failed to set latencies stats")
	}
	return nil
}

// Append appends the latency to the latency status of the node specified by
// the provided name.
func (r *latencyCache) Append(name string, latency int64) error {
	r.Lock()
	defer r.Unlock()

	latencies, err := r.get(name)
	if err != nil {
		return trace.Wrap(err, "failed to get latency stats for %q", name)
	}

	if len(latencies) >= latencyStatsSlidingWindowSize {
		// keep the slice within the sliding window size
		// slidingWindowSize is -1 because another element will be added a few lines below
		latencies = latencies[1:latencyStatsSlidingWindowSize]
	}

	latencies = append(latencies, latency)

	err = r.set(name, latencies)
	if err != nil {
		return trace.Wrap(err, "failed to set latency stats for %q", name)
	}

	return nil
}
