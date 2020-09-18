/*
Copyright 2016-2020 Gravitational, Inc.

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
	"os"

	"github.com/gravitational/satellite/agent/backend/influxdb"
	"github.com/gravitational/satellite/lib/history/sqlite"
	"github.com/gravitational/trace"
	"github.com/gravitational/version"

	log "github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

func init() {
	version.Init("v0.0.1-master+$Format:%h$")
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var (
		app   = kingpin.New("satellite", "Cluster health monitoring agent")
		debug = app.Flag("debug", "Enable verbose mode").Bool()

		// Launch agent service
		cagent            = app.Command("agent", "Start monitoring agent")
		cagentRPCAddrs    = ListFlag(cagent.Flag("rpc-addr", "List of addresses to bind the RPC listener to (host:port), comma-separated").Default("127.0.0.1:7575"))
		cagentName        = cagent.Flag("name", "Agent name that is used identify this node in the cluster membership service").OverrideDefaultFromEnvar(EnvAgentName).String()
		cagentMetricsAddr = cagent.Flag("metrics-addr", "Address to listen on for web interface and telemetry for Prometheus metrics").Default("127.0.0.1:7580").String()
		cagentCAFile      = cagent.Flag("ca-file", "SSL CA certificate for verifying server certificates").ExistingFile()
		cagentCertFile    = cagent.Flag("cert-file", "SSL certificate for server RPC").ExistingFile()
		cagentKeyFile     = cagent.Flag("key-file", "SSL certificate key for server RPC").ExistingFile()

		// Membership configuration
		cagentMembership  = cagent.Flag("membership", "Specifies the cluster membership interface to be used").Default(membershipK8s).Enum(membershipK8s, membershipSerf)
		cagentSerfRPCAddr = cagent.Flag("serf-rpc-addr", "RPC address of the local serf node").Default("127.0.0.1:7373").String()

		// InfluxDB backend configuration
		cagentInfluxDatabase = cagent.Flag("influxdb-database", "Database to connect to").OverrideDefaultFromEnvar(EnvInfluxDatabase).String()
		cagentInfluxUsername = cagent.Flag("influxdb-user", "Username to use for connection").OverrideDefaultFromEnvar(EnvInfluxUser).String()
		cagentInfluxPassword = cagent.Flag("influxdb-password", "Password to use for connection").OverrideDefaultFromEnvar(EnvInfluxPassword).String()
		cagentInfluxURL      = cagent.Flag("influxdb-url", "URL of the InfluxDB endpoint").OverrideDefaultFromEnvar(EnvInfluxURL).String()

		// SQLite configuration
		cagentTimelineDir = cagent.Flag("timeline", "Directory to be used for timeline storage").Default("/tmp/timeline").String()
		cagentRetention   = cagent.Flag("retention", "Window to retain timeline as a Go duration").Duration()

		// Display cluster status information
		cstatus         = app.Command("status", "Query cluster status")
		cstatusRPCPort  = cstatus.Flag("rpc-port", "Local agent RPC port").Default("7575").Int()
		cstatusCAFile   = cstatus.Flag("ca-file", "CA certificate for verifying server certificates").Default("/var/lib/satellite/ca.pem").ExistingFile()
		cstatusCertFile = cstatus.Flag("cert-file", "mTLS client certificate file").Default("/var/lib/satellite/cert.pem").ExistingFile()
		cstatusKeyFile  = cstatus.Flag("key-file", "mTLS client key file").Default("/var/lib/satellite/cert.key").ExistingFile()

		// Display cluster status
		cstatusCluster       = cstatus.Command("cluster", "Display cluster status").Default()
		cstatusClusterPretty = cstatusCluster.Flag("pretty", "Pretty-print the output").Bool()
		cstatusClusterLocal  = cstatusCluster.Flag("local", "Query the status of the local node").Bool()

		// Display cluster status history
		cstatusHistory = cstatus.Command("history", "Display cluster status history")

		// Run local compatibility checks
		cchecks = app.Command("checks", "Run local compatibility checks")

		// Display satellite version
		cversion = app.Command("version", "Display version")
	)

	var cmd string
	var err error

	cmd, err = app.Parse(os.Args[1:])
	if err != nil {
		return trace.Errorf("unable to parse command line.\nUse agent --help for help.")
	}

	log.SetOutput(os.Stderr)
	if *debug == true {
		log.SetLevel(log.InfoLevel)
	} else {
		log.SetLevel(log.WarnLevel)
	}

	switch cmd {
	case cagent.FullCommand():
		err = runAgent(&config{
			name:        *cagentName,
			rpcAddrs:    *cagentRPCAddrs,
			caFile:      *cagentCAFile,
			certFile:    *cagentCertFile,
			keyFile:     *cagentKeyFile,
			metricsAddr: *cagentMetricsAddr,
			membershipConfig: &membershipConfig{
				membership:  *cagentMembership,
				serfRPCAddr: *cagentSerfRPCAddr,
			},
			sqliteConfig: &sqlite.Config{
				DBPath:            *cagentTimelineDir,
				RetentionDuration: *cagentRetention,
			},
			influxdbConfig: &influxdb.Config{
				Database: *cagentInfluxDatabase,
				Username: *cagentInfluxUsername,
				Password: *cagentInfluxPassword,
				URL:      *cagentInfluxURL,
			},
		})
	case cstatusCluster.FullCommand():
		config := statusConfig{
			rpcConfig: rpcConfig{
				rpcPort:  *cstatusRPCPort,
				caFile:   *cstatusCAFile,
				certFile: *cstatusCertFile,
				keyFile:  *cstatusKeyFile,
			},
			local:       *cstatusClusterLocal,
			prettyPrint: *cstatusClusterPretty,
		}
		_, err = status(config)
	case cstatusHistory.FullCommand():
		config := rpcConfig{
			rpcPort:  *cstatusRPCPort,
			caFile:   *cstatusCAFile,
			certFile: *cstatusCertFile,
			keyFile:  *cstatusKeyFile,
		}
		_, err = statusHistory(config)
	case cchecks.FullCommand():
		err = localChecks()
	case cversion.FullCommand():
		version.Print()
		err = nil
	}

	return trace.Wrap(err)
}
