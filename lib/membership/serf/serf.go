/*
Copyright 2020 Gravitational, Inc.

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

// Package serf provides an implementation of membership.Cluster that relies on
// a serf cluster.
package serf

import (
	pb "github.com/gravitational/satellite/agent/proto/agentpb"
	"github.com/sirupsen/logrus"

	"github.com/gravitational/trace"
	serf "github.com/hashicorp/serf/client"
)

// Cluster can poll the members of the Serf cluster.
//
// Implements membership.Cluster
type Cluster struct {
	// config specifies the information needed to create a client connection
	// to the local serf agent.
	config *serf.Config
}

// NewCluster returns a new Serf cluster.
func NewCluster(config *serf.Config) (*Cluster, error) {
	if config == nil {
		return nil, trace.BadParameter("serf config must be provided")
	}
	if config.Addr == "" {
		return nil, trace.BadParameter("serf addr must be provided")
	}
	return &Cluster{
		config: config,
	}, nil
}

// Members lists the members of the Serf cluster.
// Inactive members will be filtered out.
func (r *Cluster) Members() ([]*pb.MemberStatus, error) {
	return r.members()
}

// members lists the members of the Serf cluster.
// Inactive members will be filtered out.
func (r *Cluster) members() (clusterMembers []*pb.MemberStatus, err error) {
	client, err := serf.ClientFromConfig(r.config)
	if err != nil {
		return clusterMembers, trace.Wrap(err, "failed to create serf client")
	}
	defer client.Close()

	serfMembers, err := client.Members()
	if err != nil {
		return nil, trace.Wrap(err, "failed to fetch serf members")
	}
	serfMembers = filterInactive(serfMembers)

	for _, serfMember := range serfMembers {
		status := pb.NewMemberStatus(serfMember.Name, serfMember.Addr.String(), serfMember.Tags)
		clusterMembers = append(clusterMembers, status)
	}

	return clusterMembers, nil
}

// Member returns the member with the specified name.
// Returns NotFound if the specified member is not an active member of the
// Serf cluster.
func (r *Cluster) Member(name string) (member *pb.MemberStatus, err error) {
	members, err := r.members()
	if err != nil {
		return member, trace.Wrap(err, "failed to get cluster members")
	}

	for _, member := range members {
		if member.Name == name {
			return member, nil
		}
	}

	return member, trace.NotFound("member %s is not an active member of the cluster", name)
}

// filterInactive filters out serf members that are not "alive".
func filterInactive(members []serf.Member) (result []serf.Member) {
	for _, member := range members {
		if memberStatus(member.Status) != memberAlive {
			logrus.WithField("member", member.Name).Debug("Inactive member has been filtered.")
			continue
		}
		result = append(result, member)
	}
	return result
}

// memberStatus describes the state of a serf node.
type memberStatus string

const (
	// memberAlive indicates serf member is active.
	memberAlive memberStatus = "alive"
	// memberLeaving indicates serf member is in the process of leaving the cluster.
	memberLeaving memberStatus = "leaving"
	// memberLeft indicates serf member has left the cluster.
	memberLeft memberStatus = "left"
	// memberFailed indicates failure has been detected on serf member.
	memberFailed memberStatus = "failed"
)
