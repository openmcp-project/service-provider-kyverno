package helm

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func TestExtractHelmValues(t *testing.T) {
	tests := []struct {
		name    string
		values  *apiextensionsv1.JSON
		want    *HelmValues
		wantErr bool
	}{
		{
			name:    "Nil input returns empty HelmValues",
			values:  nil,
			want:    &HelmValues{},
			wantErr: false,
		},
		{
			name: "Extract ImagePullSecrets",
			values: &apiextensionsv1.JSON{
				Raw: []byte(`{"global": {"imagePullSecrets": [{"name": "my-secret"}]}}`),
			},
			want: &HelmValues{
				Global: Global{
					ImagePullSecrets: []corev1.LocalObjectReference{
						{Name: "my-secret"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Ignore unknown values",
			values: &apiextensionsv1.JSON{
				Raw: []byte(`{"replicaCount": 3}`),
			},
			want:    &HelmValues{},
			wantErr: false,
		},
		{
			name: "Error on invalid JSON",
			values: &apiextensionsv1.JSON{
				Raw: []byte("invalid json"),
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractHelmValues(tt.values)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ExtractHelmValues() succeeded unexpectedly")
				}
				return
			}
			if err != nil {
				t.Fatalf("ExtractHelmValues() failed: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractHelmValues() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
