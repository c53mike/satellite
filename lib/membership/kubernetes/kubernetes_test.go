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
	"testing"

	"github.com/gravitational/satellite/lib/membership"
	"github.com/gravitational/satellite/lib/test"

	. "gopkg.in/check.v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestPing(t *testing.T) { TestingT(t) }

type KubernetesSuite struct{}

var _ = Suite(&KubernetesSuite{})

// TestMembers verifies members can be queried.
func (r *KubernetesSuite) TestMembers(c *C) {
	tests := []struct {
		comment  string
		pods     []v1.Pod
		expected []membership.Member
	}{
		{
			comment: "List all pods",
			pods: []v1.Pod{
				r.newPod("satellite-1", "satellite", "192.168.1.101",
					map[string]string{"app": "satellite"},
					map[string]string{"role": "master"}),
				r.newPod("satellite-2", "satellite", "192.168.1.102",
					map[string]string{"app": "satellite"},
					map[string]string{"role": "master"}),
			},
			expected: []membership.Member{
				membership.NewMember("satellite-1", "192.168.1.101", map[string]string{"role": "master"}),
				membership.NewMember("satellite-2", "192.168.1.102", map[string]string{"role": "master"}),
			},
		},
		{
			comment: "Filter pods that are not in the satellite namespace",
			pods: []v1.Pod{
				r.newPod("satellite-1", "satellite", "192.168.1.101",
					map[string]string{"app": "satellite"},
					map[string]string{"role": "master"}),
				r.newPod("satellite-2", "not-satellite", "192.168.1.102",
					map[string]string{"app": "satellite"},
					map[string]string{"role": "master"}),
			},
			expected: []membership.Member{
				membership.NewMember("satellite-1", "192.168.1.101", map[string]string{"role": "master"}),
			},
		},
		{
			comment: "Filter pods that do not have the app=satellite label",
			pods: []v1.Pod{
				r.newPod("satellite-1", "satellite", "192.168.1.101",
					map[string]string{"app": "satellite"},
					map[string]string{"role": "master"}),
				r.newPod("satellite-2", "satellite", "192.168.1.102",
					map[string]string{"app": "not-satellite"},
					map[string]string{"role": "master"}),
			},
			expected: []membership.Member{
				membership.NewMember("satellite-1", "192.168.1.101", map[string]string{"role": "master"}),
			},
		},
	}

	for _, tc := range tests {
		comment := Commentf(tc.comment)

		cluster := &Cluster{
			config: &Config{
				Namespace:     testNamespace,
				LabelSelector: testLabelSelector,
			},
			client: fake.NewSimpleClientset(
				&v1.PodList{Items: tc.pods},
			),
		}

		members, err := cluster.Members()
		c.Assert(err, IsNil, comment)
		c.Assert(members, test.DeepCompare, tc.expected, comment)
	}
}

// TestMember verifies single member can be queried.
func (r *KubernetesSuite) TestMember(c *C) {
	tests := []struct {
		comment  string
		name     string
		pods     []v1.Pod
		expected membership.Member
	}{
		{
			comment: "",
			name:    "satellite-1",
			pods: []v1.Pod{
				r.newPod("satellite-1", "satellite", "192.168.1.101",
					map[string]string{"app": "satellite"},
					map[string]string{"role": "master"}),
			},
			expected: membership.NewMember("satellite-1", "192.168.1.101", map[string]string{"role": "master"}),
		},
	}
	for _, tc := range tests {
		comment := Commentf(tc.comment)

		cluster := &Cluster{
			config: &Config{
				Namespace:     testNamespace,
				LabelSelector: testLabelSelector,
			},
			client: fake.NewSimpleClientset(
				&v1.PodList{Items: tc.pods},
			),
		}

		member, err := cluster.Member(tc.name)
		c.Assert(err, IsNil, comment)
		c.Assert(member, test.DeepCompare, tc.expected, comment)
	}
}

// newPod constructs a new pod.
func (r *KubernetesSuite) newPod(name, namespace, addr string, labels, annotations map[string]string) v1.Pod {
	return v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Status: v1.PodStatus{
			PodIP: addr,
		},
	}
}

const (
	testNamespace     = "satellite"
	testLabelSelector = "app=satellite"
)
