/*
Copyright 2023 The access Authors.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Access is the Schema for the access api
type Access struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AccessSpec   `json:"spec,omitempty"`
	Status AccessStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AccessList contains a list of Access
type AccessList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Access `json:"items"`
}

// AccessSpec defines the desired state of Access
type AccessSpec struct {
	// IPs define the blacklist ips
	// +required
	IPs []string `json:"ips"`

	// NodeSelector define the blacklist ips join node
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// nodeName define the blacklist ips join node name
	// +optional
	NodeName string `json:"nodeName,omitempty"`
}

// AccessStatus defines the observed state of Access
type AccessStatus struct {
	// ObservedGeneration is the most recent generation observed. It corresponds to the
	// Object's generation, which is updated on mutation by the API Server.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// NodeStatus define the node blacklist ips
	// +optional
	NodeStatus map[string][]string `json:"nodeStatuses,omitempty"`
}
