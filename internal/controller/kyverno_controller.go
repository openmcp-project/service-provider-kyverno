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
	"time"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/openmcp-project/controller-utils/pkg/clusters"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	ctrlerrors "github.com/openmcp-project/controller-utils/pkg/errors"
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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/openmcp-project/opencontrolplane-runtime/pkg/serviceprovider"
	spclusteraccess "github.com/openmcp-project/opencontrolplane-runtime/pkg/serviceprovider/clusteraccess"

	apiv1alpha1 "github.com/openmcp-project/service-provider-kyverno/api/v1alpha1"
	"github.com/openmcp-project/service-provider-kyverno/internal/flux"
	"github.com/openmcp-project/service-provider-kyverno/internal/helm"
	internalstatus "github.com/openmcp-project/service-provider-kyverno/internal/status"
)

const (
	// secretNamePrefix is the prefix used for secrets replicated into tenant namespaces.
	secretNamePrefix = "sp-kyverno-"
	// managedByLabelKey / managedByLabelValue mark secrets that were replicated by this controller
	// so they can be identified and cleaned up when no longer needed.
	managedByLabelKey   = "app.kubernetes.io/managed-by"
	managedByLabelValue = "service-provider-kyverno"
	// HelmReleaseName is the name of the Helm release used to deploy Kyverno in the onboarding cluster.
	HelmReleaseName = "kyverno"
	// OCIRepositoryName is the name of the OCI repository where the Kyverno Helm chart is stored.
	OCIRepositoryName = "kyverno"
	// KyvernoNamespace is the dedicated namespace on the controlplane where Kyverno will be installed.
	// Kyverno must be installed in its own namespace per upstream requirements.
	KyvernoNamespace = "kyverno"
	// requestSuffixMCP is the suffix used for the mcp cluster.
	requestSuffixMCP = "--mcp"
	// helmReleaseMaxFailures is the maximum number of consecutive failures from the HelmRelease conditions before the controller stops retrying and surfaces the failure in the Kyverno resource status.
	helmReleaseMaxFailures = 5
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
// nolint:gocyclo
func (r *KyvernoReconciler) CreateOrUpdate(ctx context.Context, svcobj *apiv1alpha1.Kyverno, providerConfig *apiv1alpha1.ProviderConfig, clusters spclusteraccess.ClusterContext) (ctrl.Result, error) {
	l := logf.FromContext(ctx)
	l.Info("Reconciling Kyverno resource", "name", svcobj.Name, "namespace", svcobj.Namespace)
	serviceprovider.StatusProgressing(svcobj, "Reconciling", "Reconcile in progress")

	kyvernoVersion, err := providerConfig.SelectVersion(svcobj.Spec.Version)
	if err != nil {
		internalstatus.Failed(svcobj, err.Error())
		return ctrl.Result{}, ctrlerrors.IgnoreInvalidUserInput(err)
	}

	tenantNamespace, err := libutils.StableMCPNamespace(svcobj.Name, svcobj.Namespace)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to determine stable namespace for Kyverno instance: %w", err)
	}
	l.Info("Checking tenant namespace", "namespace", tenantNamespace)

	if clusters.MCPCluster == nil {
		internalstatus.Failed(svcobj, "ControlPlane cluster context is nil")
		return ctrl.Result{}, fmt.Errorf("ControlPlane cluster context is nil")
	}

	// 1. Replicate Chart Pull Secret to tenant namespace on Platform cluster
	prefixedChartPullSecret, err := r.replicateChartPullSecret(ctx, kyvernoVersion, tenantNamespace)
	if err != nil {
		internalstatus.Failed(svcobj, err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to replicate chart pull secret: %w", err)
	}

	// clean up any managed secrets in the tenant namespace that are no longer desired
	desiredPlatformSecrets := []string{}
	if prefixedChartPullSecret != "" {
		desiredPlatformSecrets = append(desiredPlatformSecrets, prefixedChartPullSecret)
	}
	if err := deleteOrphanSecrets(ctx, r.PlatformCluster.Client(), tenantNamespace, desiredPlatformSecrets); err != nil {
		internalstatus.Failed(svcobj, err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to clean up orphan secrets in tenant namespace: %w", err)
	}

	// 2. Extract Helm Values to delet orphan secrets on the ControlPlane cluster
	helmValues, err := helm.ExtractHelmValues(kyvernoVersion.HelmValues)
	if err != nil {
		internalstatus.Failed(svcobj, err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to extract helm values: %w", err)
	}

	desiredControlPlaneSecrets := make([]string, 0, len(helmValues.Global.ImagePullSecrets))
	for _, ref := range helmValues.Global.ImagePullSecrets {
		prefixedName, err := prefixedSecretName(ref.Name)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error generating prefixed secret name: %w", err)
		}
		desiredControlPlaneSecrets = append(desiredControlPlaneSecrets, prefixedName)
	}
	if err := deleteOrphanSecrets(ctx, clusters.MCPCluster.Client(), KyvernoNamespace, desiredControlPlaneSecrets); err != nil {
		internalstatus.Failed(svcobj, err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to clean up orphan secrets in kyverno namespace on MCP: %w", err)
	}

	// 3. Create or update OCIRepository object
	if err := r.createOrUpdateOCIRepository(ctx, tenantNamespace, kyvernoVersion, prefixedChartPullSecret); err != nil {
		internalstatus.Failed(svcobj, err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to reconcile OCIRepository: %w", err)
	}

	// 4. Create or update HelmRelease object
	if err := r.createOrUpdateHelmRelease(ctx, tenantNamespace, svcobj, kyvernoVersion); err != nil {
		internalstatus.Failed(svcobj, err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to reconcile HelmRelease: %w", err)
	}

	// 5. Replicate image pull secrets to ControlPlane cluster
	if err := r.replicateImagePullSecrets(ctx, clusters.MCPCluster.Client(), helmValues); err != nil {
		internalstatus.Failed(svcobj, err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to replicate image pull secrets: %w", err)
	}

	l.Info("Done Reconciling Kyverno resource", "name", svcobj.Name)
	return r.reconcileHelmReleaseStatus(ctx, svcobj, tenantNamespace)
}

// reconcileHelmReleaseStatus fetches the HelmRelease and reflects its condition onto the Kyverno status.
// It implements a circuit breaker: after helmReleaseMaxFailures consecutive failures, it stops.
func (r *KyvernoReconciler) reconcileHelmReleaseStatus(ctx context.Context, svcobj *apiv1alpha1.Kyverno, tenantNamespace string) (ctrl.Result, error) {
	hr := &helmv2.HelmRelease{}
	if err := r.PlatformCluster.Client().Get(ctx, client.ObjectKey{
		Name:      HelmReleaseName,
		Namespace: tenantNamespace,
	}, hr); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get HelmRelease: %w", err)
	}

	for _, cond := range hr.Status.Conditions {
		if cond.Type != "Ready" {
			continue
		}
		return r.applyHelmReleaseCondition(svcobj, cond)
	}

	serviceprovider.StatusProgressing(svcobj, "Waiting", "HelmRelease not yet processed by Flux")
	return ctrl.Result{}, nil
}

// applyHelmReleaseCondition maps a HelmRelease Ready condition onto the Kyverno status.
func (r *KyvernoReconciler) applyHelmReleaseCondition(svcobj *apiv1alpha1.Kyverno, cond metav1.Condition) (ctrl.Result, error) {
	switch cond.Status {
	case metav1.ConditionTrue:
		svcobj.Status.HelmReleaseFailureCount = 0
		serviceprovider.StatusReady(svcobj)
		return ctrl.Result{}, nil
	case metav1.ConditionFalse:
		return r.handleHelmReleaseFailure(svcobj, cond.Message)
	default: // ConditionUnknown — still progressing
		serviceprovider.StatusProgressing(svcobj, cond.Reason, cond.Message)
		return ctrl.Result{}, nil
	}
}

// handleHelmReleaseFailure increments the failure counter.
func (r *KyvernoReconciler) handleHelmReleaseFailure(svcobj *apiv1alpha1.Kyverno, message string) (ctrl.Result, error) {
	svcobj.Status.HelmReleaseFailureCount++
	if svcobj.Status.HelmReleaseFailureCount >= helmReleaseMaxFailures {
		internalstatus.Failed(svcobj, fmt.Sprintf(
			"HelmRelease failed %d times, giving up: %s",
			svcobj.Status.HelmReleaseFailureCount, message,
		))
		return ctrl.Result{}, nil
	}
	internalstatus.Failed(svcobj, fmt.Sprintf(
		"HelmRelease failed (attempt %d/%d): %s",
		svcobj.Status.HelmReleaseFailureCount, helmReleaseMaxFailures, message,
	))
	return ctrl.Result{}, nil
}

// Delete is called in reconciliation when the Kyverno resource is marked for deletion
// nolint:gocyclo
func (r *KyvernoReconciler) Delete(ctx context.Context, obj *apiv1alpha1.Kyverno, _ *apiv1alpha1.ProviderConfig, clusterCtx spclusteraccess.ClusterContext) (ctrl.Result, error) {
	// mark for deletion
	serviceprovider.StatusTerminating(obj)

	tenantNamespace, err := libutils.StableMCPNamespace(obj.Name, obj.Namespace)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to determine stable namespace for Kyverno instance: %w", err)
	}

	// block deletion if domain objects still exist in the cluster
	if clusterCtx.MCPCluster != nil {
		blocked, err := r.kyvernoDomainObjectsExist(ctx, clusterCtx.MCPCluster.Client())
		if err != nil {
			return ctrl.Result{}, err
		}
		if blocked {
			internalstatus.ConditionsTerminatingFailed(obj, "deletion blocked until domain objects are removed")
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}
	if clusterCtx.MCPCluster == nil {
		panic("ControlPlane cluster context is nil, expected it to be set in Delete()")
	}

	objectsToDelete := objectsForDeletion(tenantNamespace)
	var deletedObjects []client.Object
	for _, object := range objectsToDelete {
		err := r.PlatformCluster.Client().Delete(ctx, object)
		if client.IgnoreNotFound(err) != nil {
			internalstatus.Failed(obj, err.Error())
			return ctrl.Result{}, fmt.Errorf("delete object failed: %w", err)
		}
		if apierrors.IsNotFound(err) {
			deletedObjects = append(deletedObjects, object)
		}
	}
	if len(deletedObjects) == len(objectsToDelete) {
		// all objects are deleted; clean up replicated secrets
		if err := deleteOrphanSecrets(ctx, r.PlatformCluster.Client(), tenantNamespace, nil); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to clean up secrets in tenant namespace: %w", err)
		}
		if err := deleteOrphanSecrets(ctx, clusterCtx.MCPCluster.Client(), KyvernoNamespace, nil); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to clean up secrets in kyverno namespace on MCP: %w", err)
		}
		return ctrl.Result{}, nil
	}
	// wait until all objects are deleted
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// objectsForDeletion returns the resources that should be cleaned up
// when a Kyverno instance is deleted.
func objectsForDeletion(namespace string) []client.Object {
	return []client.Object{
		flux.OCIRepositoryRef(OCIRepositoryName, namespace),
		flux.HelmReleaseRef(HelmReleaseName, namespace),
	}
}

// kyvernoDomainObjectsExist reports whether any Kyverno ClusterPolicy or Policy resources
// exist in the target cluster, blocking deletion until they are removed.
func (r *KyvernoReconciler) kyvernoDomainObjectsExist(ctx context.Context, cl client.Client) (bool, error) {
	// move somewhere generic
	for _, apiVersionKind := range [][2]string{
		{"kyverno.io/v1", "ClusterPolicyList"},
		{"kyverno.io/v1", "PolicyList"},
	} {
		list := &unstructured.UnstructuredList{}
		list.SetAPIVersion(apiVersionKind[0])
		list.SetKind(apiVersionKind[1])
		if err := cl.List(ctx, list); err != nil {
			if apimeta.IsNoMatchError(err) {
				continue
			}
			return false, fmt.Errorf("failed to list %s: %w", apiVersionKind[1], err)
		}
		if len(list.Items) > 0 {
			return true, nil
		}
	}
	return false, nil
}

func (r *KyvernoReconciler) replicateChartPullSecret(ctx context.Context, kyvernoVersion apiv1alpha1.KyvernoVersion, targetNamespace string) (string, error) {
	if kyvernoVersion.ChartPullSecret == "" {
		return "", nil
	}
	prefixedName, err := prefixedSecretName(kyvernoVersion.ChartPullSecret)
	if err != nil {
		return "", fmt.Errorf("error generating prefixed secret name: %w", err)
	}
	platformClient := r.PlatformCluster.Client()

	sourceSecret := &corev1.Secret{}
	sourceKey := client.ObjectKey{Name: kyvernoVersion.ChartPullSecret, Namespace: r.PodNamespace}

	if err := platformClient.Get(ctx, sourceKey, sourceSecret); err != nil {
		return "", fmt.Errorf("failed to get chart pull secret %q from namespace %q: %w", kyvernoVersion.ChartPullSecret, r.PodNamespace, err)
	}

	targetSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      prefixedName,
			Namespace: targetNamespace,
		},
	}
	if _, err := ctrl.CreateOrUpdate(ctx, platformClient, targetSecret, func() error {
		if targetSecret.Labels == nil {
			targetSecret.Labels = map[string]string{}
		}
		targetSecret.Labels[managedByLabelKey] = managedByLabelValue
		targetSecret.Data = sourceSecret.Data
		targetSecret.Type = sourceSecret.Type
		return nil
	}); err != nil {
		return "", fmt.Errorf("failed to replicate chart pull secret %q to namespace %q: %w", kyvernoVersion.ChartPullSecret, targetNamespace, err)
	}
	return prefixedName, nil
}

func prefixedSecretName(name string) (string, error) {
	return ctrlutils.ShortenToXCharacters(fmt.Sprintf("%s%s", secretNamePrefix, name), ctrlutils.K8sMaxNameLength)
}

func (r *KyvernoReconciler) replicateImagePullSecrets(ctx context.Context, cpClient client.Client, helmValues *helm.Values) error {
	for _, ref := range helmValues.Global.ImagePullSecrets {
		sourceSecret := &corev1.Secret{}
		if err := r.PlatformCluster.Client().Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: r.PodNamespace}, sourceSecret); err != nil {
			return fmt.Errorf("failed to get image pull secret %q from namespace %q: %w", ref.Name, r.PodNamespace, err)
		}

		prefixedName, err := prefixedSecretName(ref.Name)
		if err != nil {
			return fmt.Errorf("error generating prefixed secret name: %w", err)
		}

		targetSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      prefixedName,
				Namespace: KyvernoNamespace,
			},
		}
		if _, err := ctrl.CreateOrUpdate(ctx, cpClient, targetSecret, func() error {
			if targetSecret.Labels == nil {
				targetSecret.Labels = map[string]string{}
			}
			targetSecret.Labels[managedByLabelKey] = managedByLabelValue
			targetSecret.Data = sourceSecret.Data
			targetSecret.Type = sourceSecret.Type
			return nil
		}); err != nil {
			return fmt.Errorf("failed to replicate image pull secret %q to namespace %q on ControlPlane: %w", ref.Name, KyvernoNamespace, err)
		}
	}
	return nil
}

// deleteOrphanSecrets deletes all secrets in namespace that are labeled as managed by this
// controller but whose names are not in keep. Pass nil to delete all managed secrets.
func deleteOrphanSecrets(ctx context.Context, c client.Client, namespace string, keep []string) error {
	keepSet := make(map[string]struct{}, len(keep))
	for _, name := range keep {
		keepSet[name] = struct{}{}
	}
	list := &corev1.SecretList{}
	if err := c.List(ctx, list,
		client.InNamespace(namespace),
		client.MatchingLabels{managedByLabelKey: managedByLabelValue},
	); err != nil {
		return fmt.Errorf("failed to list managed secrets in namespace %q: %w", namespace, err)
	}
	for i := range list.Items {
		if _, ok := keepSet[list.Items[i].Name]; ok {
			continue
		}
		if err := c.Delete(ctx, &list.Items[i]); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete orphan secret %q in namespace %q: %w", list.Items[i].Name, namespace, err)
		}
	}
	return nil
}

func (r *KyvernoReconciler) createOrUpdateOCIRepository(ctx context.Context, namespace string, kyvernoVersion apiv1alpha1.KyvernoVersion, chartPullSecretName string) error {
	ociRepo := flux.CreateOCIRepository(flux.OCIRepositoryParams{
		ChartURL:            kyvernoVersion.GetChartURL(),
		Version:             kyvernoVersion.ChartVersion,
		Name:                OCIRepositoryName,
		Namespace:           namespace,
		ChartPullSecretName: chartPullSecretName,
	})
	managedObj := &sourcev1.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ociRepo.Name,
			Namespace: namespace,
		},
	}
	l := logf.FromContext(ctx)
	l.Info("Creating OCI Repository", "object", ociRepo)
	if _, err := ctrl.CreateOrUpdate(ctx, r.PlatformCluster.Client(), managedObj, func() error {
		if managedObj.Labels == nil {
			managedObj.Labels = map[string]string{}
		}
		managedObj.Labels[managedByLabelKey] = managedByLabelValue
		managedObj.Spec = ociRepo.Spec
		return nil
	}); err != nil {
		return fmt.Errorf("failed to create or update OCIRepository: %w", err)
	}

	return nil
}

func (r *KyvernoReconciler) createOrUpdateHelmRelease(ctx context.Context, namespace string, svcobj *apiv1alpha1.Kyverno, kyvernoVersion apiv1alpha1.KyvernoVersion) error {
	fluxConfigRef, err := r.getControlPlaneFluxConfig(ctx, namespace, svcobj.Name)
	if err != nil {
		return fmt.Errorf("failed to get FluxConfig for ControlPlane cluster: %w", err)
	}

	helmRelease := flux.CreateHelmRelease(flux.HelmReleaseParams{
		Name:             HelmReleaseName,
		Namespace:        namespace,
		TargetNamespace:  KyvernoNamespace,
		StorageNamespace: KyvernoNamespace,
		OCIRepoName:      OCIRepositoryName,
		OCIRepoNamespace: namespace,
		Values:           kyvernoVersion.HelmValues,
		KubeConfigRef:    fluxConfigRef,
		DriftDetection: &helmv2.DriftDetection{
			Mode: helmv2.DriftDetectionEnabled,
		},
	})
	managedObj := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmRelease.Name,
			Namespace: namespace,
		},
	}
	l := logf.FromContext(ctx)
	l.Info("Creating HelmRelease", "object", managedObj)
	if _, err := ctrl.CreateOrUpdate(ctx, r.PlatformCluster.Client(), managedObj, func() error {
		if managedObj.Labels == nil {
			managedObj.Labels = map[string]string{}
		}
		managedObj.Labels[managedByLabelKey] = managedByLabelValue
		managedObj.Spec = helmRelease.Spec
		return nil
	}); err != nil {
		return fmt.Errorf("failed to create or update HelmRelease: %w", err)
	}
	return nil
}

func (r *KyvernoReconciler) getControlPlaneFluxConfig(ctx context.Context, namespace string, objectName string) (*meta.SecretKeyReference, error) {
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
