package helm

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// Values defines the helm values that are explicitly processed during reconciliation.
type Values struct {
	Global Global `json:"global,omitempty,omitzero"`
}

// Global defines the global settings that are explicitly processed during reconciliation.
type Global struct {
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

// ExtractHelmValues extracts helm values required for processing.
func ExtractHelmValues(values *apiextensionsv1.JSON) (*Values, error) {
	if values == nil || len(values.Raw) == 0 {
		return &Values{}, nil
	}

	vals := &Values{}
	if err := json.Unmarshal(values.Raw, vals); err != nil {
		return nil, fmt.Errorf("failed to unmarshal helm values: %w", err)
	}

	return vals, nil
}
