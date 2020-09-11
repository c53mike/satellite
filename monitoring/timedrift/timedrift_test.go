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

package timedrift

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/gravitational/satellite/agent/health"
	"github.com/gravitational/satellite/agent/proto/agentpb"
	"github.com/gravitational/satellite/lib/membership"
	"github.com/gravitational/satellite/lib/rpc/client"
	"github.com/gravitational/satellite/lib/test"

	"github.com/gravitational/trace"
	"github.com/jonboulle/clockwork"
	. "gopkg.in/check.v1"
)

func TestTimeDrift(t *testing.T) { TestingT(t) }

type TimeDriftSuite struct {
	clock clockwork.Clock
}

var _ = Suite(&TimeDriftSuite{})

func (r *TimeDriftSuite) SetUpSuite(c *C) {
	r.clock = clockwork.NewFakeClock()
}

func (s *TimeDriftSuite) TestTimeDriftChecker(c *C) {
	tests := []struct {
		comment  string
		cluster  mockCluster
		expected health.Probes
	}{
		{
			comment: "Acceptable time drift",
			cluster: mockCluster{
				clients: map[string]*mockedTimeAgentClient{
					node2: newMockedTimeAgentClient(node2, s.clock.Now().Add(driftUnderThreshold())),
					node3: newMockedTimeAgentClient(node3, s.clock.Now().Add(driftUnderThreshold())),
				},
			},
			expected: health.Probes{
				successProbe(node1, timeDriftThreshold),
			},
		},
		{
			comment: "Acceptable time drift, one node is lagging behind",
			cluster: mockCluster{
				clients: map[string]*mockedTimeAgentClient{
					node2: newMockedTimeAgentClient(node2, s.clock.Now().Add(driftUnderThreshold())),
					node3: newMockedTimeAgentClient(node3, s.clock.Now().Add(-driftUnderThreshold())),
				},
			},
			expected: health.Probes{
				successProbe(node1, timeDriftThreshold),
			},
		},
		{
			comment: "Time drift to node-3 exceeds threshold",
			cluster: mockCluster{
				clients: map[string]*mockedTimeAgentClient{
					node2: newMockedTimeAgentClient(node2, s.clock.Now().Add(driftUnderThreshold())),
					node3: newMockedTimeAgentClient(node3, s.clock.Now().Add(driftOverThreshold())),
				},
			},
			expected: health.Probes{
				failureProbe(node1, node3, driftOverThreshold(), timeDriftThreshold),
			},
		},
		{
			comment: "Time drift to node-2 exceeds threshold",
			cluster: mockCluster{
				clients: map[string]*mockedTimeAgentClient{
					node2: newMockedTimeAgentClient(node2, s.clock.Now().Add(-driftOverThreshold())),
					node3: newMockedTimeAgentClient(node3, s.clock.Now().Add(driftUnderThreshold())),
				},
			},
			expected: health.Probes{
				failureProbe(node1, node2, -driftOverThreshold(), timeDriftThreshold),
			},
		},
		{
			comment: "Time drift to both nodes exceeds threshold",
			cluster: mockCluster{
				clients: map[string]*mockedTimeAgentClient{
					node2: newMockedTimeAgentClient(node2, s.clock.Now().Add(-driftOverThreshold())),
					node3: newMockedTimeAgentClient(node3, s.clock.Now().Add(driftOverThreshold())),
				},
			},
			expected: health.Probes{
				failureProbe(node1, node2, -driftOverThreshold(), timeDriftThreshold),
				failureProbe(node1, node3, driftOverThreshold(), timeDriftThreshold),
			},
		},
	}

	for _, tc := range tests {
		comment := Commentf(tc.comment)
		checker, err := NewChecker(Config{
			NodeName: node1,
			Cluster:  tc.cluster,
			DialRPC:  tc.cluster.dial,
			Clock:    s.clock,
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
	clients map[string]*mockedTimeAgentClient
}

func (r mockCluster) Members() ([]membership.Member, error) {
	members := make([]membership.Member, 0, len(r.clients))
	for _, client := range r.clients {
		members = append(members, memberFromMockClient(client))
	}
	return members, nil
}

func (r mockCluster) dial(_ context.Context, name string) (client.Client, error) {
	client, exists := r.clients[name]
	if !exists {
		return nil, trace.NotFound("member %s does not exist in this cluster", name)
	}
	return client, nil
}

// memberFromMockClient constructs a new ClusterMember from the provided client.
func memberFromMockClient(client *mockedTimeAgentClient) membership.Member {
	return membership.Member{
		Name: client.name,
		Addr: client.name, // mock dial function will use name to dial node
	}
}

type mockedTimeAgentClient struct {
	client.Client
	name string
	time time.Time
}

func newMockedTimeAgentClient(name string, time time.Time) *mockedTimeAgentClient {
	return &mockedTimeAgentClient{
		name: name,
		time: time,
	}
}

func (r *mockedTimeAgentClient) Time(ctx context.Context, req *agentpb.TimeRequest) (*agentpb.TimeResponse, error) {
	return &agentpb.TimeResponse{
		Timestamp: agentpb.NewTimeToProto(r.time),
	}, nil
}

func (r *mockedTimeAgentClient) Close() error {
	return nil
}

// driftOverThreshold returns time drift value that exceeds configured threshold.
func driftOverThreshold() time.Duration {
	return timeDriftThreshold * 2
}

// driftUnderThreshold returns time drift value that is within configured threshold.
func driftUnderThreshold() time.Duration {
	return timeDriftThreshold / 2
}

const (
	// Test nodes
	node1 = "node-1"
	node2 = "node-2"
	node3 = "node-3"
)
