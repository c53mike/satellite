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
	"github.com/gravitational/satellite/cmd"
	serfmembership "github.com/gravitational/satellite/lib/membership/serf"
	"github.com/gravitational/satellite/lib/nethealth"
	"github.com/gravitational/satellite/lib/rpc/client"
	"github.com/gravitational/satellite/monitoring"
	"github.com/gravitational/satellite/monitoring/latency"
	"github.com/gravitational/satellite/monitoring/timedrift"

	"github.com/gravitational/trace"
	serf "github.com/hashicorp/serf/client"
	log "github.com/sirupsen/logrus"
)

// config represents configuration for setting up monitoring checkers.
type config struct {
	// rpcAddrs is the list of listening addresses on RPC agents
	rpcAddrs []string
	// agentCAFile sets the file location for the Agent CA cert
	agentCAFile string
	// agentCertFile sets the file location for the Agent cert
	agentCertFile string
	// agentKeyFile sets the file location for the Agent cert key
	agentKeyFile string
	// serfRPCAddr is the Serf RPC endpoint address
	serfRPCAddr string
	// kubeconfigPath is the path to the kubeconfig file
	kubeconfigPath string
	// kubeletAddr is the address of the kubelet
	kubeletAddr string
	// dockerAddr is the endpoint of the docker daemon
	dockerAddr string
	// nettestContainerImage is the image name to use for networking test
	nettestContainerImage string
	// disableInterPodCheck disables inter-pod communication tests
	disableInterPodCheck bool
	// etcd defines etcd-specific configuration
	etcd *monitoring.ETCDConfig
}

// addCheckers adds checkers to the agent.
func addCheckers(node agent.Agent, config *config) (err error) {
	log.Debugf("Monitoring Agent started with config %#v", config)

	local, err := node.GetConfig().Cluster.Member(node.GetConfig().Name)
	if err != nil {
		return trace.Wrap(err, "failed to get local member")
	}

	role, exists := local.Tags["role"]
	if !exists {
		return trace.NotFound("local node does not have a role")
	}

	switch agent.Role(role) {
	case agent.RoleMaster:
		client, err := cmd.GetKubeClientFromPath(config.kubeconfigPath)
		if err != nil {
			return trace.Wrap(err)
		}
		kubeConfig := monitoring.KubeConfig{Client: client}
		err = addToMaster(node, config, kubeConfig)
	case agent.RoleNode:
		err = addToNode(node, config)
	}
	return trace.Wrap(err)
}

func addToMaster(node agent.Agent, config *config, kubeConfig monitoring.KubeConfig) error {
	cluster, err := serfmembership.NewCluster(&serf.Config{
		Addr: config.serfRPCAddr,
	})
	if err != nil {
		return trace.Wrap(err)
	}

	etcdChecker, err := monitoring.EtcdHealth(config.etcd)
	if err != nil {
		return trace.Wrap(err)
	}

	timeDriftChecker, err := timedrift.NewChecker(&timedrift.Config{
		NodeName: node.GetConfig().Name,
		Cluster:  cluster,
		DialRPC:  client.DefaultDialRPC(node.GetConfig().CAFile, node.GetConfig().CertFile, node.GetConfig().KeyFile),
	})
	if err != nil {
		return trace.Wrap(err)
	}

	latencyChecker, err := latency.NewChecker(&latency.Config{
		NodeName:      node.GetConfig().Name,
		Cluster:       cluster,
		LatencyClient: nethealth.NewClient(nethealth.DefaultNethealthSocket),
	})
	if err != nil {
		return trace.Wrap(err)
	}

	node.AddChecker(monitoring.KubeAPIServerHealth(kubeConfig))
	node.AddChecker(monitoring.DockerHealth(config.dockerAddr))
	node.AddChecker(etcdChecker)
	node.AddChecker(monitoring.SystemdHealth())
	node.AddChecker(timeDriftChecker)
	node.AddChecker(latencyChecker)

	if !config.disableInterPodCheck {
		node.AddChecker(monitoring.InterPodCommunication(kubeConfig, config.nettestContainerImage))
	}
	return nil
}

func addToNode(node agent.Agent, config *config) error {
	etcdChecker, err := monitoring.EtcdHealth(config.etcd)
	if err != nil {
		return trace.Wrap(err)
	}
	node.AddChecker(monitoring.KubeletHealth(config.kubeletAddr))
	node.AddChecker(monitoring.DockerHealth(config.dockerAddr))
	node.AddChecker(etcdChecker)
	node.AddChecker(monitoring.SystemdHealth())
	return nil
}
