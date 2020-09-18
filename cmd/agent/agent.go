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
	"context"
	"io/ioutil"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gravitational/satellite/agent"
	"github.com/gravitational/satellite/agent/backend/influxdb"
	"github.com/gravitational/satellite/agent/backend/inmemory"
	"github.com/gravitational/satellite/agent/cache"
	"github.com/gravitational/satellite/agent/cache/multiplex"
	"github.com/gravitational/satellite/lib/history"
	"github.com/gravitational/satellite/lib/history/sqlite"
	"github.com/gravitational/satellite/lib/membership"
	kuberneteslib "github.com/gravitational/satellite/lib/membership/kubernetes"
	serflib "github.com/gravitational/satellite/lib/membership/serf"
	"github.com/gravitational/satellite/lib/rpc/client"

	"github.com/gravitational/trace"
	serf "github.com/hashicorp/serf/client"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"
)

type config struct {
	// name specifies the name used to identify this agent inside the cluster
	// membership service.
	name string
	// rpcAddrs is the list of listening addresses on RPC agents.
	rpcAddrs []string
	// caFile sets the file location for the Agent CA cert.
	caFile string
	// certFile sets the file location for the Agent cert.
	certFile string
	// keyFile sets the file location for the Agent cert key.
	keyFile string
	// metricsAddr specifies the address on which to expose Prometheus metrics.
	metricsAddr string
	// membershipConfig specifies the cluster membership configuration.
	membershipConfig *membershipConfig
	// sqliteConfig specifies sqlite configuration.
	sqliteConfig *sqlite.Config
	// influxConfig specifies influxDB configuration.
	influxdbConfig *influxdb.Config
}

// func (r *config) checkAndSetDefaults() error {
// 	return trace.NotImplemented("not yet implemented")
// }

type membershipConfig struct {
	// membership specifies the membership service type.
	membership string
	// serfRPCAddr specifies the local serf RPC address. Must be specified if
	// using membership `serf`.
	serfRPCAddr string
}

func runAgent(conf *config) error {
	cluster, err := initMembership(conf.membershipConfig)
	if err != nil {
		return trace.Wrap(err, "failed to initialize cluster membership")
	}
	cache, err := initCache(conf.influxdbConfig)
	if err != nil {
		return trace.Wrap(err, "failed to initialize cache")
	}
	// clusterTimeline, err := initTimeline(*conf.sqliteConfig, "cluster.db")
	// if err != nil {
	// 	return trace.Wrap(err, "failed to initialize cluster timeline")
	// }
	// localTimeline, err := initTimeline(*conf.sqliteConfig, "local.db")
	// if err != nil {
	// 	return trace.Wrap(err, "failed to initialize local timeline")
	// }

	agentConfig := &agent.Config{
		Name:        conf.name,
		RPCAddrs:    conf.rpcAddrs,
		CAFile:      conf.caFile,
		CertFile:    conf.certFile,
		KeyFile:     conf.keyFile,
		MetricsAddr: conf.metricsAddr,
		Cluster:     cluster,
		Cache:       cache,
		DialRPC:     client.DefaultDialRPC(conf.caFile, conf.certFile, conf.keyFile),
		// ClusterTimeline: clusterTimeline,
		// LocalTimeline:   localTimeline,
	}

	monitoringAgent, err := agent.New(agentConfig)
	if err != nil {
		return trace.Wrap(err, "failed to create agent")
	}
	defer monitoringAgent.Close()

	if err := addCheckers(monitoringAgent); err != nil {
		return trace.Wrap(err, "failed to add checkers")
	}

	if err := monitoringAgent.Start(); err != nil {
		return trace.Wrap(err, "failed to start agent")
	}

	// defer deleteCredentials(satelliteDir)
	if err := storeCredentials(satelliteDir, conf.caFile, conf.certFile, conf.keyFile); err != nil {
		return trace.Wrap(err, "failed to store satellite credentials")
	}

	signalc := make(chan os.Signal, 2)
	signal.Notify(signalc, os.Interrupt, syscall.SIGTERM)

	select {
	case <-signalc:
	}

	log.Info("shutting down agent")
	return nil
}

// initMembership initializes the cluster membership interface.
func initMembership(config *membershipConfig) (membership.Cluster, error) {
	switch config.membership {
	case membershipK8s:
		// Assuming satellite is running in a Pod.
		kubeconfig, err := rest.InClusterConfig()
		if err == rest.ErrNotInCluster {
			return nil, trace.Wrap(err, "satellite is not running inside a kubernetes cluster")
		}
		if err != nil {
			return nil, trace.Wrap(err, "failed to retrieve kubeconfig")
		}
		cluster, err := kuberneteslib.NewCluster(&kuberneteslib.Config{
			Kubeconfig:    kubeconfig,
			Namespace:     "satellite",
			LabelSelector: "app=satellite",
		})
		if err != nil {
			return nil, trace.Wrap(err, "failed to initialize kubernetes cluster membership")
		}
		return cluster, nil
	case membershipSerf:
		cluster, err := serflib.NewCluster(&serf.Config{
			Addr: config.serfRPCAddr,
		})
		if err != nil {
			return nil, trace.Wrap(err, "failed to initialize serf cluster membership")
		}
		return cluster, nil
	case "":
		return nil, trace.BadParameter("cluster membership must be specified")
	default:
		return nil, trace.BadParameter("unsupported cluster membership: %s", config.membership)
	}
}

// initCache initializes the agent cache.
func initCache(config *influxdb.Config) (cache.Cache, error) {
	cache := inmemory.New()
	if config.Database == "" {
		return multiplex.New(cache), nil
	}

	log.Infof("connecting to influxdb database `%v` on %v", config.Database, config.URL)
	influxdb, err := influxdb.New(config)
	if err != nil {
		return nil, trace.Wrap(err, "failed to create influxdb backend")
	}
	return multiplex.New(cache, influxdb), nil
}

// initTimeline initializes a new sqlite timeline. fileName specifies the
// SQLite database file name.
func initTimeline(config sqlite.Config, fileName string) (history.Timeline, error) {
	const timelineInitTimeout = time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), timelineInitTimeout)
	defer cancel()
	config.DBPath = path.Join(config.DBPath, fileName)
	return sqlite.NewTimeline(ctx, config)
}

// storeCredentials stores the credentials to the specified directory.
func storeCredentials(dir, ca, cert, key string) error {
	if err := makeIfNotExists(dir, mode); err != nil {
		return trace.Wrap(err, "failed to make directory %s with mode %v", dir, mode)
	}

	for _, src := range []string{ca, cert, key} {
		dst := filepath.Join(dir, filepath.Base(src))
		if err := copyFile(src, dst, mode); err != nil {
			return trace.Wrap(err, "failed to copy %s to %s", src, dst)
		}
	}

	return nil
}

// makeIfNotExist makes the directory if it does not exist.
func makeIfNotExists(dir string, perm os.FileMode) error {
	err := os.Mkdir(dir, perm)
	if err == nil || os.IsExist(err) {
		return nil
	}
	return trace.Wrap(err)
}

// copyFile copies src to dst.
func copyFile(src, dst string, perm os.FileMode) error {
	input, err := ioutil.ReadFile(src)
	if err != nil {
		return trace.Wrap(err, "failed to read src %s", src)
	}
	if err := ioutil.WriteFile(dst, input, perm); err != nil {
		return trace.Wrap(err, "failed to write src %s to dst %s", src, dst)
	}
	return nil
}

// deleteCredentials deletes the specified directory.
func deleteCredentials(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		log.WithError(err).Debug("Failed to remove satellite directory.")
	}
}

const satelliteDir = "/var/lib/satellite"
const mode = 0644
