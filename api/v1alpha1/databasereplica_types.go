/*
Copyright 2022 DigitalOcean.

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

package v1alpha1

import (
	"github.com/digitalocean/godo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DatabaseReplicaSpec defines the desired state of DatabaseReplica
type DatabaseReplicaSpec struct {
	// Name is the name of the database cluster.
	Name string `json:"name"`
	// Size is the slug of the node size to use.
	Size string `json:"size"`
	// PrimaryClusterUUID is the UUID of the primary cluster to create the read-only replica from
	PrimaryClusterUUID string `json:"primaryClusterUUID"`
	// Region is the slug of the DO region for the cluster. If it is not set, the replica will be placed in the same region as the cluster
	Region string `json:"region,omitempty"`
}

// ToGodoCreateRequest returns a create request for a database that will fulfill
// the DatabaseClusterSpec.
func (spec *DatabaseReplicaSpec) ToGodoCreateRequest() *godo.DatabaseCreateReplicaRequest {
	return &godo.DatabaseCreateReplicaRequest{
		Name:   spec.Name,
		Size:   spec.Size,
		Region: spec.Region,
	}
}

// DatabaseReplicaStatus defines the observed state of DatabaseReplica
type DatabaseReplicaStatus struct {
	// UUID is the UUID of the database cluster.
	UUID string `json:"uuid,omitempty"`
	// Status is the status of the database cluster.
	Status string `json:"status,omitempty"`
	// CreatedAt is the time at which the database cluster was created.
	CreatedAt metav1.Time `json:"createdAt,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// DatabaseReplica is the Schema for the databasereadonlyreplicas API
type DatabaseReplica struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DatabaseReplicaSpec   `json:"spec,omitempty"`
	Status DatabaseReplicaStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DatabaseReplicaList contains a list of DatabaseReplica
type DatabaseReplicaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DatabaseReplica `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DatabaseReplica{}, &DatabaseReplicaList{})
}
