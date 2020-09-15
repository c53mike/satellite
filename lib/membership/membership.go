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

// Package membership provides an interface for polling cluster membership
package membership

// Cluster interface allows cluster membership polling.
type Cluster interface {
	// Members returns the list of cluster members.
	Members() ([]Member, error)
	// Member returns the member with the specified name.
	Member(name string) (Member, error)
}

// Member contains information to identify and access a member.
type Member struct {
	// Name is the member's name.
	Name string
	// Addr is the member's IP address.
	Addr string
	// Tags is the list of tags held by the member.
	Tags map[string]string
}

// NewMember constructs a new Member.
func NewMember(name, addr string, tags map[string]string) Member {
	return Member{
		Name: name,
		Addr: addr,
		Tags: tags,
	}
}

// IsMaster returns true if member has role 'master'
func (r Member) IsMaster() bool {
	role, exists := r.Tags["role"]
	return exists && Role(role) == RoleMaster
}

// Role describes the agent's server role.
type Role string

const (
	RoleMaster Role = "master"
	RoleNode        = "node"
)
