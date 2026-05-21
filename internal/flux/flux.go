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

package flux

import (
	"strings"
	"time"

	"github.com/fluxcd/pkg/apis/meta"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
)

// HelmReleaseParams holds all inputs needed to construct a HelmRelease.
type HelmReleaseParams struct {
	Name             string
	Namespace        string
	ReleaseName      string
	TargetNamespace  string
	StorageNamespace string
	OCIRepoName      string
	OCIRepoNamespace string
	Values           *apiextensionsv1.JSON
	KubeConfigRef    *meta.SecretKeyReference
}

// CreateOciRepository builds a fully-specified OCIRepository resource.
func CreateOciRepository(chartURL, version, name, namespace string) *sourcev1.OCIRepository {
	return &sourcev1.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: sourcev1.OCIRepositorySpec{
			Interval: metav1.Duration{Duration: time.Minute},
			URL:      EnsureOCIScheme(chartURL),
			LayerSelector: &sourcev1.OCILayerSelector{
				MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
				Operation: sourcev1.OCILayerCopy,
			},
			Reference: &sourcev1.OCIRepositoryRef{
				Tag: version,
			},
		},
	}
}

// EnsureOCIScheme prefixes url with "oci://" if not already present.
func EnsureOCIScheme(url string) string {
	if !strings.HasPrefix(url, "oci://") {
		return "oci://" + url
	}
	return url
}

// CreateHelmRelease builds a fully-specified HelmRelease resource.
func CreateHelmRelease(p HelmReleaseParams) *helmv2.HelmRelease {
	remediationStrategy := helmv2.RollbackRemediationStrategy
	return &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.Name,
			Namespace: p.Namespace,
		},
		Spec: helmv2.HelmReleaseSpec{
			ReleaseName:      p.ReleaseName,
			Interval:         metav1.Duration{Duration: time.Minute},
			TargetNamespace:  p.TargetNamespace,
			StorageNamespace: p.StorageNamespace,
			Install: &helmv2.Install{
				CRDs:            helmv2.Create,
				CreateNamespace: true,
				Remediation: &helmv2.InstallRemediation{
					Retries: 3,
				},
			},
			Upgrade: &helmv2.Upgrade{
				CRDs:          helmv2.CreateReplace,
				CleanupOnFail: true,
				Remediation: &helmv2.UpgradeRemediation{
					Retries:  3,
					Strategy: &remediationStrategy,
				},
			},
			ChartRef: &helmv2.CrossNamespaceSourceReference{
				Kind:      "OCIRepository",
				Name:      p.OCIRepoName,
				Namespace: p.OCIRepoNamespace,
			},
			Values: p.Values,
			KubeConfig: &meta.KubeConfigReference{
				SecretRef: p.KubeConfigRef,
			},
		},
	}
}

// OciRepositoryRef returns a minimal OCIRepository stub for kubeclient requests (only ObjectMeta is set).
func OciRepositoryRef(name, namespace string) *sourcev1.OCIRepository {
	return &sourcev1.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

// HelmReleaseRef returns a minimal HelmRelease stub for kubeclient requests (only ObjectMeta is set).
func HelmReleaseRef(name, namespace string) *helmv2.HelmRelease {
	return &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}
