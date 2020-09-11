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

// Package kubernetes provides an implementation of membership.Cluster that
// relies on a kubernetes cluster.
package kubernetes

import (
	"github.com/gravitational/satellite/lib/membership"
	"github.com/gravitational/satellite/utils"

	"github.com/gravitational/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Config contains information needed to identify other members deployed in the
// kubernetes cluster.
type Config struct {
	// Kubeconfig specifies the kubeconfig
	Kubeconfig *rest.Config
	// Namespace specifies the operating kubernetes namespace.
	Namespace string
	// LabelSelector specifies the kubernetes labels that identify cluster
	// members.
	LabelSelector string
}

// checkAndSetDefaults validates configuration and sets default values.
func (r *Config) checkAndSetDefaults() error {
	var errors []error
	if r.Kubeconfig == nil {
		errors = append(errors, trace.BadParameter("Kubeconfig must be provided"))
	}
	if r.Namespace == "" {
		errors = append(errors, trace.BadParameter("Namespace must be provided"))
	}
	if r.LabelSelector == "" {
		errors = append(errors, trace.BadParameter("LabelSelector must be provided"))
	}
	return trace.NewAggregate(errors...)
}

// Cluster can poll the members that are deployed in the kubernetes cluster.
//
// Implementes membership.Cluster
type Cluster struct {
	config Config
	client kubernetes.Interface
}

// NewCluster returns a new kubernetes cluster.
func NewCluster(config Config) (*Cluster, error) {
	if err := config.checkAndSetDefaults(); err != nil {
		return nil, trace.Wrap(err, "invalid configuration")
	}

	client, err := kubernetes.NewForConfig(config.Kubeconfig)
	if err != nil {
		return nil, trace.Wrap(err, "failed to create clientset")
	}

	return &Cluster{
		config: config,
		client: client,
	}, nil
}

// Members lists the members deployed in the kubernetes cluster.
// Returns NotFound if no members are found.
func (r *Cluster) Members() (members []membership.Member, err error) {
	return r.members()
}

// members returns the list of cluster members matching the configured
// namespace and label.
// Returns NotFound if there are no pods found for the configured namespace and
// label.
func (r *Cluster) members() (members []membership.Member, err error) {
	list, err := r.client.CoreV1().Pods(r.config.Namespace).List(metav1.ListOptions{
		LabelSelector: r.config.LabelSelector,
	})
	if err != nil {
		return members, utils.ConvertError(err)
	}

	members = make([]membership.Member, len(list.Items))
	for i, pod := range list.Items {
		members[i] = membership.Member{
			Name: pod.Name,
			Addr: pod.Status.PodIP,
			Tags: pod.Annotations,
		}
	}

	return members, nil
}

// Member returns the member with the specified name.
// Returns NotFound if the specified member is not deployed in the kubernetes
// cluster.
func (r *Cluster) Member(name string) (member membership.Member, err error) {
	members, err := r.members()
	if err != nil {
		return member, trace.Wrap(err, "failed to get cluster members")
	}

	for _, member := range members {
		if member.Name == name {
			return member, nil
		}
	}

	return member, trace.NotFound("member %q is not an active member of the cluster", name)
}
