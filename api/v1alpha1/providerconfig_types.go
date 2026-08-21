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
	"fmt"
	"time"

	ctrlerrors "github.com/openmcp-project/controller-utils/pkg/errors"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DefaultChartURL is the default OCI registry for the Kyverno Helm chart.
const DefaultChartURL = "oci://ghcr.io/kyverno/charts/kyverno"

// ProviderConfigSpec defines the desired state of ProviderConfig
type ProviderConfigSpec struct {
	// Versions specify the valid inputs for the Kyverno.Spec.Version field.
	// +required
	// +kubebuilder:validation:MinItems=1
	// +listType=map
	// +listMapKey=version
	Versions []KyvernoVersion `json:"versions"`

	// PollInterval determines how often to reconcile resources to prevent drift.
	// +optional
	// +kubebuilder:default:="1m"
	// +kubebuilder:validation:Format=duration
	PollInterval *metav1.Duration `json:"pollInterval,omitempty"`
}

// KyvernoVersion defines a version of Kyverno that can be installed.
type KyvernoVersion struct {
	// Version is the Kyverno version that maps to Kyverno.Spec.Version.
	// +required
	Version string `json:"version"`

	// ChartVersion is the version of the Helm chart to install.
	// +required
	ChartVersion string `json:"chartVersion"`

	// ChartURL is a reference to an OCI artifact repository that hosts the Kyverno Helm chart.
	// +optional
	// +kubebuilder:default="oci://ghcr.io/kyverno/charts/kyverno"
	ChartURL *string `json:"chartURL,omitempty"`

	// ChartPullSecret is a reference to a secret in the controller's namespace containing
	// credentials to pull the Helm chart from a private registry.
	// +optional
	ChartPullSecret string `json:"chartPullSecret,omitempty"`

	// Values are arbitrary Helm values passed directly to the managed HelmRelease.
	// +optional
	Values *apiextensionsv1.JSON `json:"values,omitempty"`
}

// ProviderConfigStatus defines the observed state of ProviderConfig.
type ProviderConfigStatus struct {
	// conditions represent the current state of the ProviderConfig resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ProviderConfig is the Schema for the providerconfigs API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
type ProviderConfig struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of ProviderConfig
	// +required
	Spec ProviderConfigSpec `json:"spec"`

	// status defines the observed state of ProviderConfig
	// +optional
	Status ProviderConfigStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ProviderConfigList contains a list of ProviderConfig
type ProviderConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProviderConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &ProviderConfig{}, &ProviderConfigList{})
		return nil
	})
}

// PollInterval returns the poll interval duration from the spec.
func (o *ProviderConfig) PollInterval() time.Duration {
	return o.Spec.PollInterval.Duration
}

// SelectVersion finds the KyvernoVersion entry matching requestedVersion.
// Returns ErrInvalidUserInput if not found.
func (o *ProviderConfig) SelectVersion(requestedVersion string) (KyvernoVersion, error) {
	for _, v := range o.Spec.Versions {
		if v.Version == requestedVersion {
			return v, nil
		}
	}
	return KyvernoVersion{}, fmt.Errorf("%w: requested version (%s) is not available", ctrlerrors.ErrInvalidUserInput, requestedVersion)
}

// GetChartURL returns the configured chart URL or DefaultChartURL if unset.
func (v *KyvernoVersion) GetChartURL() string {
	if v.ChartURL == nil || *v.ChartURL == "" {
		return DefaultChartURL
	}
	return *v.ChartURL
}
