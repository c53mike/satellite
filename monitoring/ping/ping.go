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

// Package ping implements a ping checker that verifies that ping times
// (RTT) between nodes in the cluster remain within a specified threshold.
package ping

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gravitational/satellite/agent/health"
	pb "github.com/gravitational/satellite/agent/proto/agentpb"
	pingpb "github.com/gravitational/satellite/agent/proto/ping"
	"github.com/gravitational/satellite/lib/membership"
	"github.com/gravitational/satellite/lib/rpc/client"
	"github.com/gravitational/satellite/monitoring"

	"github.com/gravitational/trace"
	"github.com/jonboulle/clockwork"
	"github.com/sirupsen/logrus"
)

// TODO: RTT stats should be sent to metrics
const (
	// checkerID specifies the check name
	checkerID = "ping"
	// rttThreshold sets the RTT threshold
	rttThreshold = 15 * time.Millisecond
)

// Config specifies ping checker config.
type Config struct {
	// NodeName specifies the name of the node that is running the check.
	NodeName string
	// Cluster specifies cluster membership interface.
	Cluster membership.Cluster
	// DialRPC specifies dial function used to create satellite RPC client.
	DialRPC client.DialRPC
	// Clock is used in tests to mock time.
	Clock clockwork.Clock
	// RTTThreshold specifies the RTT threshold.
	RTTThreshold time.Duration
}

// checkAndSetDefaults validates the config and sets default values.
func (r *Config) checkAndSetDefaults() error {
	var errors []error
	if r.NodeName == "" {
		errors = append(errors, trace.BadParameter("NodeName must be provided"))
	}
	if r.Cluster == nil {
		errors = append(errors, trace.BadParameter("Cluster must be provided"))
	}
	if r.DialRPC == nil {
		errors = append(errors, trace.BadParameter("DialRPC must be provided"))
	}
	if len(errors) > 0 {
		return trace.NewAggregate(errors...)
	}
	if r.Clock == nil {
		r.Clock = clockwork.NewRealClock()
	}
	if r.RTTThreshold == 0 {
		r.RTTThreshold = rttThreshold
	}
	return nil
}

// checker verifies that ping times (RTT) between nodes in the cluster
// remain within a specified threshold.
//
// Implements health.Checker
type checker struct {
	// Config contains checker configuration.
	*Config
	// FieldLogger is used for logging.
	logrus.FieldLogger

	// rttStats caches the RTT stats for the cluster members.
	rttStats *rttCache
}

// NewChecker constructs a new ping checker.
func NewChecker(config *Config) (health.Checker, error) {
	if err := config.checkAndSetDefaults(); err != nil {
		return nil, trace.Wrap(err)
	}

	return &checker{
		Config:      config,
		FieldLogger: logrus.WithField(trace.Component, checkerID),
		rttStats:    newRTTCache(),
	}, nil
}

// Name returns the checker name
func (r *checker) Name() string {
	return checkerID
}

// Check executes checks and reports results to the reporter.
func (r *checker) Check(ctx context.Context, reporter health.Reporter) {
	if err := r.check(ctx, reporter); err != nil {
		r.WithError(err).Debug("Failed to verify ping RTT.")
		return
	}
	if reporter.NumProbes() == 0 {
		reporter.Add(monitoring.NewSuccessProbe(r.Name()))
	}
}

// check checks the ping between this and other nodes in the cluster.
func (r *checker) check(ctx context.Context, reporter health.Reporter) error {
	nodes, err := r.Cluster.Members()
	if err != nil {
		return trace.Wrap(err, "failed to get cluster members")
	}
	if err := r.collectRTTs(ctx, nodes); err != nil {
		return trace.Wrap(err, "failed to collect RTT stats")
	}
	if err := r.verifyRTTStats(reporter, nodes); err != nil {
		return trace.Wrap(err, "failed to verify RTT stats")
	}
	return nil
}

// collectRTTs collects the RTTs for the specified nodes.
func (r *checker) collectRTTs(ctx context.Context, nodes []membership.Member) error {
	waitCh := make(chan struct{}, 1)
	wg := sync.WaitGroup{}
	wg.Add(len(nodes))
	for _, n := range nodes {
		go func(node membership.Member) {
			defer wg.Done()
			if err := r.collectRTT(ctx, node); err != nil {
				r.WithError(err).WithField("node", node.Name).Warn("Failed to collect RTT.")
			}
		}(n)
	}

	go func() {
		wg.Wait()
		waitCh <- struct{}{}
	}()

	select {
	case <-waitCh:
		return nil
	case <-ctx.Done():
		return trace.Wrap(ctx.Err())
	}
}

// collectRTT caclucates the RTT For the specified node and records the
// RTT for later use.
func (r *checker) collectRTT(ctx context.Context, node membership.Member) error {
	rttNanoSec, err := r.calculateRTT(ctx, node)
	if err != nil {
		return trace.Wrap(err, "failed to calculate RTT for node")
	}
	if err := r.rttStats.Append(node.Name, rttNanoSec); err != nil {
		return trace.Wrap(err, "failed to append RTT stats")
	}
	r.Debugf("%s <-ping-> %s = %s", r.NodeName, node.Name, time.Duration(rttNanoSec))
	return nil
}

// calculateRTT calculates and returns the RTT time (in nanoseconds)
// between this node and a member of the cluster.
func (r *checker) calculateRTT(ctx context.Context, node membership.Member) (rttNanos int64, err error) {
	// return zero RTT if specified node is local node.
	if r.NodeName == node.Name {
		return 0, nil
	}

	client, err := r.DialRPC(ctx, node.Addr)
	if err != nil {
		return rttNanos, trace.Wrap(err, "failed to dial cluster member at %s", node.Addr)
	}
	defer client.Close()

	// Obtain this node's local timestamp.
	t1Start := r.Clock.Now().UTC()

	// Send "ping" request to the specified node.
	_, err = client.Ping(ctx, &pingpb.PingRequest{})
	// If the agent we're making request to is of an older version,
	// it may not support Ping() method yet. This can happen, e.g.,
	// during a rolling upgrade. In this case fallback to success.
	if trace.IsNotImplemented(err) {
		r.WithError(err).WithField("node", node.Name).Warn("Node does not yet support Ping() rpc.")
		return rttNanos, nil
	}
	if err != nil {
		return rttNanos, trace.Wrap(err)
	}

	// Calculate how much time has elapsed since T1Start. This value will
	// roughly be the request roundtrip time.
	rtt := r.Clock.Now().UTC().Sub(t1Start)
	return rtt.Nanoseconds(), nil
}

// verifyRTTStats reports a failure probe for nodes that have high ping.
func (r *checker) verifyRTTStats(reporter health.Reporter, nodes []membership.Member) error {
	for _, node := range nodes {
		rtts, err := r.rttStats.Get(node.Name)
		if err != nil {
			return trace.Wrap(err, "failed to get RTT stats for %s", node.Name)
		}

		if isPingHigh(rtts, r.RTTThreshold) {
			latest := rtts[len(rtts)-1]
			reporter.Add(failureProbe(r.NodeName, node.Name, time.Duration(latest), r.RTTThreshold))
		}
	}
	return nil
}

// isPingHigh returns true if all recorded ping RTTs are above the
// specified threshold.
func isPingHigh(rtts []int64, threshold time.Duration) bool {
	for _, rtt := range rtts {
		if time.Duration(rtt) <= threshold {
			return false
		}
	}
	return true
}

// failureProbe constructs a new probe that represents a failed ping check
// between the two nodes.
func failureProbe(node1, node2 string, rtt, threshold time.Duration) *pb.Probe {
	return &pb.Probe{
		Checker:  checkerID,
		Detail:   fmt.Sprintf("ping between %s and %s is at %s", node1, node2, rtt),
		Error:    fmt.Sprintf("ping is higher than the allowed threshold of %s", threshold),
		Status:   pb.Probe_Failed,
		Severity: pb.Probe_Warning,
	}
}
