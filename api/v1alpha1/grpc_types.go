/*


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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// GrpcSpec defines the desired state of Grpc
type GrpcSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Size is the size of the grpc deployment
	Size int32 `json:"size"`

	// Image is the name of the grpc-client image running in each pod
	Image string `json:"image"`

	// Protocol is the type of protocol
	Protocol corev1.Protocol `json:"protocol"`
}

// GrpcStatus defines the observed state of Grpc
type GrpcStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Nodes are the names of the grpc client pods
	PodNames []string `json:"podNames"`

	// ServerStatus is the current status of the grpc server
	ServerStatus string `json:"serverStatus"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Grpc is the Schema for the grpcs API
type Grpc struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GrpcSpec   `json:"spec,omitempty"`
	Status GrpcStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GrpcList contains a list of Grpc
type GrpcList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Grpc `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Grpc{}, &GrpcList{})
}
