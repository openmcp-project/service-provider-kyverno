/*
Copyright 2025.

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
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// KyvernoSpec defines the desired state of Kyverno
type KyvernoSpec struct {

	// Version is the Kyverno version to install.
	// +kubebuilder:validation:Required
	Version string `json:"version"`
}

// KyvernoStatus defines the observed state of Kyverno.
type KyvernoStatus struct {
	commonapi.Status `json:",inline"`

	// HelmReleaseFailureCount tracks the number of consecutive times the HelmRelease
	// reported a failed condition. The controller will stop retrying after a fixed threshold.
	// +optional
	HelmReleaseFailureCount int `json:"helmReleaseFailureCount,omitempty"`
}

// Kyverno is the Schema for the kyvernos API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.phase`,name="Phase",type=string
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=onboarding"
type Kyverno struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Kyverno
	// +required
	Spec KyvernoSpec `json:"spec"`

	// status defines the observed state of Kyverno
	// +optional
	Status KyvernoStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// KyvernoList contains a list of Kyverno
type KyvernoList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Kyverno `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &Kyverno{}, &KyvernoList{})
		return nil
	})
}

// Finalizer returns the finalizer string for the Kyverno resource
func (o *Kyverno) Finalizer() string {
	return GroupVersion.Group + "/finalizer"
}

// GetStatus returns the status of the Kyverno resource
func (o *Kyverno) GetStatus() any {
	return o.Status
}

// GetConditions returns the conditions of the Kyverno resource
func (o *Kyverno) GetConditions() *[]metav1.Condition {
	return &o.Status.Conditions
}

// SetPhase sets the phase of the Kyverno resource status
func (o *Kyverno) SetPhase(phase string) {
	o.Status.Phase = phase
}

// SetObservedGeneration sets the observed generation of the Kyverno resource
func (o *Kyverno) SetObservedGeneration(gen int64) {
	o.Status.ObservedGeneration = gen
}
