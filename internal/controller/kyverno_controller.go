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

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/openmcp-project/controller-utils/pkg/clusters"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/lib/clusteraccess"
	libutils "github.com/openmcp-project/openmcp-operator/lib/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	apiv1alpha1 "github.com/openmcp-project/service-provider-kyverno/api/v1alpha1"
	spruntime "github.com/openmcp-project/service-provider-kyverno/pkg/spruntime"
)

const (
	// HelmReleaseName is the name of the Helm release used to deploy Kyverno in the onboarding cluster.
	HelmReleaseName = "kyverno"
	// OCIRepositoryName is the name of the OCI repository where the Kyverno Helm chart is stored.
	OCIRepositoryName = "kyverno"
	// OCMSystemNamespace is the namespace in the onboarding cluster where Kyverno controller will be deployed.
	OCMSystemNamespace = "openmcp-system"
	// requestSuffixMCP is the suffix used for the mcp cluster.
	requestSuffixMCP = "--mcp"
)

// clusterAccessName is the name of the access object containing the kubeconfig for the mcp target cluster.
var clusterAccessName = apiv1alpha1.GroupVersion.Group

// KyvernoReconciler reconciles a Kyverno object
type KyvernoReconciler struct {
	// OnboardingCluster is the cluster where this controller watches Kyverno resources and reacts to their changes.
	OnboardingCluster *clusters.Cluster
	// PlatformCluster is the cluster where this controller is deployed and configured.
	PlatformCluster *clusters.Cluster
	// PodNamespace is the namespace where this controller is deployed in.
	PodNamespace string
}

// CreateOrUpdate is called on every add or update event
func (r *KyvernoReconciler) CreateOrUpdate(ctx context.Context, svcobj *apiv1alpha1.Kyverno, providerConfig *apiv1alpha1.ProviderConfig, clusters spruntime.ClusterContext) (ctrl.Result, error) {
	l := logf.FromContext(ctx)
	l.Info("Reconciling Kyverno resource", "name", svcobj.Name, "namespace", svcobj.Namespace)
	spruntime.StatusProgressing(svcobj, "Reconciling", "Reconcile in progress")
	tenantNamespace, err := libutils.StableMCPNamespace(svcobj.Name, svcobj.Namespace)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to determine stable namespace for Kyverno instance: %w", err)
	}
	l.Info("Checking tenant namespace", "namespace", tenantNamespace)

	if err = r.replicateImagePullSecret(ctx, providerConfig, tenantNamespace); err != nil {
		spruntime.StatusFailed(svcobj, err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to replicate image pull secret: %w", err)
	}

	if err := r.createOrUpdateHelmRepository(ctx, svcobj, clusters, tenantNamespace, providerConfig); err != nil {
		spruntime.StatusFailed(svcobj, err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to reconcile OCIRepository: %w", err)
	}

	if err := r.createOrUpdateHelmRelease(ctx, tenantNamespace, svcobj, providerConfig); err != nil {
		spruntime.StatusFailed(svcobj, err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to reconcile HelmRelease: %w", err)
	}
	l.Info("Done Reconciling Kyverno resource", "name", svcobj.Name)
	// TODO: put in logic that checks the condition of the HelmRelease before setting the status
	spruntime.StatusReady(svcobj)
	return ctrl.Result{}, nil
}

// Delete is called on every delete event
func (r *KyvernoReconciler) Delete(ctx context.Context, obj *apiv1alpha1.Kyverno, providerConfig *apiv1alpha1.ProviderConfig, _ spruntime.ClusterContext) (ctrl.Result, error) {
	spruntime.StatusTerminating(obj)
	tenantNamespace, err := libutils.StableMCPNamespace(obj.Name, obj.Namespace)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to determine stable namespace for Kyverno instance: %w", err)
	}
	var objects []client.Object
	ociRepo := createHelmRepository(providerConfig, obj.Spec.Version, tenantNamespace)
	objects = append(objects, ociRepo)
	helmRelease, err := r.createHelmRelease(ctx, tenantNamespace, obj, providerConfig)
	if err != nil {
		spruntime.StatusFailed(obj, err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to create helm release: %w", err)
	}
	objects = append(objects, helmRelease)
	objectStillExists := false
	for _, object := range objects {
		if err := r.PlatformCluster.Client().Delete(ctx, object); client.IgnoreNotFound(err) == nil {
			spruntime.StatusFailed(obj, err.Error())
			return ctrl.Result{}, fmt.Errorf("delete object failed: %w", err)
		}

		if err := r.PlatformCluster.Client().Get(ctx, client.ObjectKeyFromObject(object), object); err != nil {
			objectStillExists = true
		}
	}

	if objectStillExists {
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}
	spruntime.StatusReady(obj)
	return ctrl.Result{}, nil
}

func (r *KyvernoReconciler) replicateImagePullSecret(ctx context.Context, providerConfig *apiv1alpha1.ProviderConfig, targetNamespace string) error {
	ref := providerConfig.GetImagePullSecretRef()
	if ref == nil {
		return nil
	}
	platformClient := r.PlatformCluster.Client()

	sourceSecret := &corev1.Secret{}
	sourceKey := client.ObjectKey{Name: ref.Name, Namespace: r.PodNamespace}

	if err := platformClient.Get(ctx, sourceKey, sourceSecret); err != nil {
		return fmt.Errorf("failed to get source image pull secret: %q from namespace %q: %w", ref.Name, r.PodNamespace, err)
	}

	targetSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ref.Name,
			Namespace: targetNamespace,
		},
	}
	if _, err := ctrl.CreateOrUpdate(ctx, platformClient, targetSecret, func() error {
		targetSecret.Data = sourceSecret.Data
		targetSecret.Type = sourceSecret.Type
		return nil
	}); err != nil {
		return fmt.Errorf("failed to replicate image pull secret: %q in namespace %q: %w", ref.Name, targetNamespace, err)
	}
	return nil
}

func (r *KyvernoReconciler) createOrUpdateHelmRepository(ctx context.Context, svcobj *apiv1alpha1.Kyverno, _ spruntime.ClusterContext, namespace string, providerConfig *apiv1alpha1.ProviderConfig) error {
	helmRepo := createHelmRepository(providerConfig, svcobj.Spec.Version, namespace)
	managedObj := &sourcev1.HelmRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmRepo.Name,
			Namespace: namespace,
		},
	}
	l := logf.FromContext(ctx)
	l.Info("Creating Helm Repository", "object", helmRepo)
	if _, err := ctrl.CreateOrUpdate(ctx, r.PlatformCluster.Client(), managedObj, func() error {
		managedObj.Spec = helmRepo.Spec
		return nil
	}); err != nil {
		return fmt.Errorf("failed to create or update HelmRepository: %w", err)
	}

	return nil
}

func createHelmRepository(providerConfig *apiv1alpha1.ProviderConfig, _ string, namespace string) *sourcev1.HelmRepository {
	return &sourcev1.HelmRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OCIRepositoryName,
			Namespace: namespace,
		},
		Spec: sourcev1.HelmRepositorySpec{
			Interval: metav1.Duration{Duration: time.Minute},
			URL:      ensureOCIScheme(providerConfig.GetChartURL()),
			Type:     "oci",
		},
	}
}
func ensureOCIScheme(url string) string {
	if !strings.HasPrefix(url, "oci://") {
		return "oci://" + url
	}
	return url
}

func (r *KyvernoReconciler) createOrUpdateHelmRelease(ctx context.Context, namespace string, svcobj *apiv1alpha1.Kyverno, providerConfig *apiv1alpha1.ProviderConfig) error {
	helmRelease, err := r.createHelmRelease(ctx, namespace, svcobj, providerConfig)
	if err != nil {
		return fmt.Errorf("failed to create HelmRelease object: %w", err)
	}
	managedObj := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmRelease.Name,
			Namespace: namespace,
		},
	}
	l := logf.FromContext(ctx)
	l.Info("Creating HelmRelease", "object", managedObj)
	if _, err := ctrl.CreateOrUpdate(ctx, r.PlatformCluster.Client(), managedObj, func() error {
		managedObj.Spec = helmRelease.Spec
		return nil
	}); err != nil {
		return fmt.Errorf("failed to create or update HelmRelease: %w", err)
	}
	return nil
}

func (r *KyvernoReconciler) createHelmRelease(ctx context.Context, namespace string, svcobj *apiv1alpha1.Kyverno, providerConfig *apiv1alpha1.ProviderConfig) (*helmv2.HelmRelease, error) {
	fluxConfigRef, err := r.getMcpFluxConfig(ctx, namespace, svcobj.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get FluxConfig for MCP cluster: %w", err)
	}
	helmValues := providerConfig.GetValues()
	remediationStrategy := helmv2.RollbackRemediationStrategy

	return &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      HelmReleaseName,
			Namespace: namespace,
		},
		Spec: helmv2.HelmReleaseSpec{
			ReleaseName:      apiv1alpha1.GetReleaseName(svcobj.Name),
			Interval:         metav1.Duration{Duration: time.Minute},
			TargetNamespace:  OCMSystemNamespace,
			StorageNamespace: OCMSystemNamespace,
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
				Kind:      "HelmRepository",
				Name:      OCIRepositoryName,
				Namespace: namespace,
			},
			Values: helmValues,
			KubeConfig: &meta.KubeConfigReference{
				SecretRef: fluxConfigRef,
			},
		},
	}, nil
}

func (r *KyvernoReconciler) getMcpFluxConfig(ctx context.Context, namespace string, objectName string) (*meta.SecretKeyReference, error) {
	mcpAccessRequest := &clustersv1alpha1.AccessRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusteraccess.StableRequestNameFromLocalName(clusterAccessName, objectName) + requestSuffixMCP,
			Namespace: namespace,
		},
	}
	if err := r.PlatformCluster.Client().Get(ctx, client.ObjectKeyFromObject(mcpAccessRequest), mcpAccessRequest); err != nil {
		return nil, fmt.Errorf("failed to get MCP AccessRequest: %w", err)
	}

	return &meta.SecretKeyReference{
		Name: mcpAccessRequest.Status.SecretRef.Name,
		Key:  "kubeconfig",
	}, nil
}
