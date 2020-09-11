/*
Copyright 2019 Gravitational, Inc.

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
	"context"
	"sort"
	"testing"

	"github.com/gravitational/satellite/agent/health"
	"github.com/gravitational/satellite/lib/membership"
	"github.com/gravitational/satellite/lib/test"

	. "gopkg.in/check.v1"
)

func TestPing(t *testing.T) { TestingT(t) }

type PingSuite struct{}

var _ = Suite(&PingSuite{})

func (*PingSuite) TestPingCHecker(c *C) {
	tests := []struct {
		comment  string
		cluster  mockCluster
		expected health.Probes
	}{
		// TODO:
	}
	for _, tc := range tests {
		comment := Commentf(tc.comment)
		checker, err := NewChecker(Config{
			NodeName: node1,
			Cluster:  tc.cluster,
		})
		c.Assert(err, IsNil, comment)

		test.WithTimeout(func(ctx context.Context) {
			var probes health.Probes
			checker.Check(ctx, &probes)
			sort.Sort(health.ByDetail(probes))
			c.Assert(probes, test.DeepCompare, tc.expected, comment)
		})
	}
}

type mockCluster struct {
	membership.Cluster
}

const (
	// Test nodes
	node1 = "node-1"
	node2 = "node-2"
	node3 = "node-3"
)
