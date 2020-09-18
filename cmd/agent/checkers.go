/*
Copyright 2016 Gravitational, Inc.

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

package main

import (
	"github.com/gravitational/satellite/agent"
	"github.com/gravitational/satellite/monitoring"
	"github.com/gravitational/satellite/monitoring/ping"
	"github.com/gravitational/satellite/monitoring/timedrift"

	"github.com/gravitational/trace"
	log "github.com/sirupsen/logrus"
)

// addCheckers adds checkers to the agent.
func addCheckers(node agent.Agent) (err error) {
	log.Debugf("Monitoring Agent started.")

	local, err := node.GetConfig().Cluster.Member(node.GetConfig().Name)
	if err != nil {
		return trace.Wrap(err, "failed to get local member")
	}

	if local.IsMaster() {
		if err = addToMaster(node); err != nil {
			return trace.Wrap(err, "failed to add checkers")
		}
		return nil
	}

	if err := addToNode(node); err != nil {
		return trace.Wrap(err, "failed to add checkers")
	}
	return nil
}

func addToMaster(node agent.Agent) error {
	// TODO:
	config := node.GetConfig()
	pingChecker, err := ping.NewChecker(&ping.Config{
		NodeName: config.Name,
		Cluster:  config.Cluster,
		DialRPC:  config.DialRPC,
	})
	if err != nil {
		return trace.Wrap(err)
	}

	timeDriftChecker, err := timedrift.NewChecker(&timedrift.Config{
		NodeName: config.Name,
		Cluster:  config.Cluster,
		DialRPC:  config.DialRPC,
	})
	if err != nil {
		return trace.Wrap(err)
	}

	node.AddChecker(monitoring.SystemdHealth())
	node.AddChecker(pingChecker)
	node.AddChecker(timeDriftChecker)
	return nil
}

func addToNode(node agent.Agent) error {
	// TODO:
	node.AddChecker(monitoring.SystemdHealth())
	return nil
}
