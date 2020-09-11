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

// Package ping implements a ping checker that verifies that ping times (RTT)
// between nodes in the cluster remain within a specified threshold.
package ping

import (
	"context"
	"fmt"
	"time"

	"github.com/gravitational/satellite/agent/health"
	pb "github.com/gravitational/satellite/agent/proto/agentpb"
	"github.com/gravitational/satellite/lib/membership"
	"github.com/gravitational/satellite/monitoring"

	"github.com/gravitational/trace"
	"github.com/sirupsen/logrus"
)

// TODO: latency stats should be sent to metrics
const (
	// checkerID specifies the check name
	checkerID = "ping"
	// latencyQuantile sets the quantile used while checking Histograms against RTT results
	latencyQuantile = 95.0
	// latencyThreshold sets the RTT threshold
	latencyThreshold = 15 * time.Millisecond
)

// Config specifies ping checker config.
type Config struct {
	// NodeName specifies the name of the node that is running the check.
	NodeName string
	// Cluster specifies cluster membership interface.
	Cluster membership.Cluster
	// LatencyThreshold specifies the RTT threshold.
	LatencyThreshold time.Duration
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
	if len(errors) > 0 {
		return trace.NewAggregate(errors...)
	}
	if r.LatencyThreshold == 0 {
		r.LatencyThreshold = latencyThreshold
	}
	return nil
}

// checker verifies that ping times (RTT) between nodes in the cluster remain
// within a specified threshold.
//
// Implements health.Checker
type checker struct {
	// Config contains checker configuration.
	Config
	// FieldLogger is used for logging.
	logrus.FieldLogger

	// latencyStatus caches the latency stats for the cluster members.
	latencyStats *latencyCache
}

// NewChecker constructs a new ping checker.
func NewChecker(config Config) (health.Checker, error) {
	if err := config.checkAndSetDefaults(); err != nil {
		return nil, trace.Wrap(err)
	}

	return &checker{
		Config:       config,
		FieldLogger:  logrus.WithField(trace.Component, checkerID),
		latencyStats: newLatencyCache(),
	}, nil
}

// Name returns the checker name
func (r *checker) Name() string {
	return checkerID
}

// Check executes checks and reports results to the reporter.
func (r *checker) Check(ctx context.Context, reporter health.Reporter) {
	if err := r.check(ctx, reporter); err != nil {
		r.WithError(err).Debug("Failed to verify ping latency.")
		return
	}
	if reporter.NumProbes() == 0 {
		reporter.Add(monitoring.NewSuccessProbe(r.Name()))
	}
}

// check checks the ping between this and other nodes in the cluster.
func (r *checker) check(_ context.Context, reporter health.Reporter) (err error) {
	nodes, err := r.Cluster.Members()
	if err != nil {
		return trace.Wrap(err, "failed to get cluster members")
	}

	if err = r.checkNodesRTT(nodes, reporter); err != nil {
		return trace.Wrap(err, "failed to check node RTTs")
	}

	return nil
}

// checkNodesRTT checks the ping time between this node and the members of the
// cluster.
func (r *checker) checkNodesRTT(nodes []membership.Member, reporter health.Reporter) (err error) {
	// TODO:
	self, err := r.Cluster.Member(r.NodeName)
	if err != nil {
		return trace.Wrap(err, "failed to get cluster member")
	}
	for _, node := range nodes {
		if self.Name == node.Name {
			// Skip self
			continue
		}

		rttNanoSec, err := r.calculateRTT(self, node)
		if err != nil {
			return trace.Wrap(err, "failed to calculate RTT")
		}

		if err := r.latencyStats.Append(node.Name, rttNanoSec); err != nil {
			return trace.Wrap(err, "failed to update latency stats")
		}

		latencies, err := r.latencyStats.Get(node.Name)
		if err != nil {
			return trace.Wrap(err, "failed to get latency stats")
		}

		latencyHistogram, err := buildLatencyHistogram(node.Name, latencies)
		if err != nil {
			return trace.Wrap(err, "failed to build latency histogram")
		}

		latency95 := time.Duration(latencyHistogram.ValueAtQuantile(latencyQuantile))

		r.Debugf("%s <-ping-> %s = %s [latest]", self.Name, node.Name, time.Duration(rttNanoSec))
		r.Debugf("%s <-ping-> %s = %s [%.2f percentile]", self.Name, node.Name, latency95, latencyQuantile)

		if latency95 >= r.LatencyThreshold {
			r.Warningf("%s <-ping-> %s = slow ping detected. Value %s over threshold %s",
				self.Name, node.Name, latency95, r.LatencyThreshold)
			reporter.Add(failureProbe(self.Name, node.Name, latency95, r.LatencyThreshold))
		} else {
			r.Debugf("%s <-ping-> %s = ping okay. Value %s within threshold %s",
				self.Name, node.Name, latency95, r.LatencyThreshold)
		}
	}

	return nil
}

// calculateRTT calculates and returns the latency time (in nanoseconds) between
// two members of the cluster.
func (r *checker) calculateRTT(node1, node2 membership.Member) (rttNanos int64, err error) {
	// TODO: can no longer rely on serf coordinates to calcuate RTT.
	return rttNanos, trace.NotImplemented("not yet implemented")
}

// failureProbe constructs a new probe that represents a failed ping check
// between the two nodes.
func failureProbe(node1, node2 string, latency, threshold time.Duration) *pb.Probe {
	return &pb.Probe{
		Checker:  checkerID,
		Detail:   fmt.Sprintf("ping between %s and %s is at %s", node1, node2, latency),
		Error:    fmt.Sprintf("ping is higher than the allowed threshold of %s", threshold),
		Status:   pb.Probe_Failed,
		Severity: pb.Probe_Warning,
	}
}
