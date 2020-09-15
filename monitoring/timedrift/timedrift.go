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

// Package timedrift implements a timedrift checker that verifies that the time
// drift between nodes in the cluster remain within a specified threshold.
package timedrift

import (
	"context"
	"fmt"
	"time"

	"github.com/gravitational/satellite/agent/health"
	pb "github.com/gravitational/satellite/agent/proto/agentpb"
	"github.com/gravitational/satellite/lib/membership"
	"github.com/gravitational/satellite/lib/rpc/client"

	"github.com/gravitational/trace"
	"github.com/jonboulle/clockwork"
	"github.com/sirupsen/logrus"
)

const (
	// checkerID is the time drift check name.
	checkerID = "time-drift"
	// timeDriftThreshold sets the default threshold of the acceptable time
	// difference between nodes.
	timeDriftThreshold = 300 * time.Millisecond
)

// Config stores configuration for the time drift check.
type Config struct {
	// NodeName specifies the name of the node that is running the check.
	NodeName string
	// Cluster specifies cluster membership interface.
	Cluster membership.Cluster
	// DialRPC specifies dial function used to create satellite RPC client.
	DialRPC client.DialRPC
	// Clock is used in tests to mock time.
	Clock clockwork.Clock
	// TimeDriftThreshold specifies the time drift threshold.
	TimeDriftThreshold time.Duration
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
	if r.TimeDriftThreshold == 0 {
		r.TimeDriftThreshold = timeDriftThreshold
	}
	return nil
}

// checker verifies that the time drift between nodes in the cluster remain
// within a specified threshold.
//
// Implements health.Checker
type checker struct {
	// Config contains checker configuration.
	*Config
	// FieldLogger is used for logging.
	logrus.FieldLogger
}

// NewChecker constructs a new timedrift checker.
func NewChecker(config *Config) (health.Checker, error) {
	if err := config.checkAndSetDefaults(); err != nil {
		return nil, trace.Wrap(err)
	}
	return &checker{
		Config:      config,
		FieldLogger: logrus.WithField(trace.Component, checkerID),
	}, nil
}

// Name returns the checker name.
func (r *checker) Name() string {
	return checkerID
}

// Check executes checks and reports results to the reporter.
func (r *checker) Check(ctx context.Context, reporter health.Reporter) {
	if err := r.check(ctx, reporter); err != nil {
		r.WithError(err).Debug("Failed to check time drift.")
		return
	}
	if reporter.NumProbes() == 0 {
		reporter.Add(successProbe(r.NodeName, r.TimeDriftThreshold))
	}
}

// check checks the time drift between this and other nodes in the cluster.
func (r *checker) check(ctx context.Context, reporter health.Reporter) error {
	nodes, err := r.Cluster.Members()
	if err != nil {
		return trace.Wrap(err, "failed to get cluster members")
	}

	for _, node := range nodes {
		if r.NodeName == node.Name {
			// Skip self
			continue
		}
		drift, err := r.getTimeDrift(ctx, node)
		if err != nil {
			r.WithError(err).WithField("node", node.Name).Debug("Failed to get time drift.")
			continue
		}
		if isDriftHigh(drift, r.TimeDriftThreshold) {
			reporter.Add(failureProbe(r.NodeName, node.Name, drift, r.TimeDriftThreshold))
		}
	}
	return nil
}

// getTimeDrift calculates the time drift value between this and the specified
// node using the following algorithm.
//
// Every coordinator node (Kubernetes masters) executes an instance
// of this algorithm.
//
// For each of the remaining cluster nodes (including other coordinator nodes):
//
// * Selected coordinator node records its local timestamp (in UTC). Let’s call
//   this timestamp T1Start.
//
// * Coordinator initiates a "ping" grpc request to the node.
//
// * The node responds to the ping request replying with node's local timestamp
//   (in UTC) in the payload. Let's call this timestamp T2.
//
// * After receiving the remote response, coordinator records the second local
//   timestamp. Let's call it T1End.
//
// * Coordinator calculates the latency between itself and the node:
//   (T1End-T1Start)/2. Let's call this value Latency.
//
// * Coordinator calculates the time drift between itself and the node:
//   T2-T1Start-Latency. Let's call this value Drift. Can be negative which would
//   mean the node time is falling behind.
//
// * Compare abs(Drift) with the threshold.
func (r *checker) getTimeDrift(ctx context.Context, node membership.Member) (time.Duration, error) {
	client, err := r.DialRPC(ctx, node.Addr)
	if err != nil {
		return 0, trace.Wrap(err, "failed to dial cluster member")
	}
	defer client.Close()

	// Obtain this node's local timestamp.
	t1Start := r.Clock.Now().UTC()

	// Send "time" request to the specified node.
	t2Response, err := client.Time(ctx, &pb.TimeRequest{})
	// If the agent we're making request to is of an older version,
	// it may not support Time() method yet. This can happen, e.g.,
	// during a rolling upgrade. In this case fallback to success.
	if trace.IsNotImplemented(err) {
		r.WithError(err).WithField("node", node.Name).Warn("node does not yet support Time() rpc")
		return 0, nil
	}
	if err != nil {
		return 0, trace.Wrap(err)
	}

	// Calculate how much time has elapsed since T1Start. This value will
	// roughly be the request roundtrip time, so the latency b/w the nodes
	// is half that.
	latency := r.Clock.Now().UTC().Sub(t1Start) / 2

	// Finally calculate the time drift between this and the specified node
	// using formula: T2 - T1Start - Latency.
	t2 := t2Response.GetTimestamp().ToTime()
	drift := t2.Sub(t1Start) - latency

	r.WithField("node", node.Name).Debugf("T1Start: %v; T2: %v; Latency: %v; Drift: %v.",
		t1Start, t2, latency, drift)
	return drift, nil
}

// successProbe constructs a probe that represents successful time drift check.
func successProbe(node string, threshold time.Duration) *pb.Probe {
	return &pb.Probe{
		Checker: checkerID,
		Detail: fmt.Sprintf("time drift between %s and other nodes is within the allowed threshold of %s",
			node, threshold),
		Status: pb.Probe_Running,
	}
}

// failureProbe constructs a probe that represents failed time drift check
// against the specified node.
func failureProbe(node1, node2 string, drift, threshold time.Duration) *pb.Probe {
	return &pb.Probe{
		Checker: checkerID,
		Detail:  fmt.Sprintf("time drift between %s and %s is %s", node1, node2, drift),
		Error:   fmt.Sprintf("time drift is higher than the allowed threshold of %s", threshold),
		Status:  pb.Probe_Failed,
	}
}

// isDriftHigh returns true if the provided drift value is over the threshold.
func isDriftHigh(drift time.Duration, threshold time.Duration) bool {
	return drift < 0 && -drift > threshold || drift > threshold
}
