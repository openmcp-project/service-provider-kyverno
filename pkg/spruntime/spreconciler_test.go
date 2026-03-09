package spruntime

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/api/common"
	"github.com/stretchr/testify/assert"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testNamespaceName      = "test-namespace"
	testObjectName         = "test-name"
	testObjectNameNotFound = "notfound"

	testMCPName       = "mcp-name"
	testMCPKubeconfig = "mcp-kubeconfig"

	testWorkloadName       = "workload-name"
	testWorkloadKubeconfig = "workload-kubeconfig"
)

func TestSPReconciler_Reconcile(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		apiObj         ServiceProviderAPI
		providerConfig *fakeProviderConfigImpl
		req            ctrl.Request
		want           ctrl.Result
		wantErr        bool
	}{
		{
			name: "api obj createOrUpdate -> requeue with pc poll interval",
			apiObj: &fakeApiImpl{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testObjectName,
					Namespace: testNamespaceName,
				},
			},
			req: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      testObjectName,
					Namespace: testNamespaceName,
				},
			},
			providerConfig: &fakeProviderConfigImpl{
				FakePollInterval: time.Hour,
			},
			want: ctrl.Result{
				RequeueAfter: time.Hour,
			},
			wantErr: false,
		},
		{
			name: "api obj delete -> requeue with pc poll interval",
			apiObj: &fakeApiImpl{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testObjectName,
					Namespace: testNamespaceName,
					DeletionTimestamp: &metav1.Time{
						Time: time.Now(),
					},
					Finalizers: []string{"string"},
				},
			},
			req: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      testObjectName,
					Namespace: testNamespaceName,
				},
			},
			providerConfig: &fakeProviderConfigImpl{
				FakePollInterval: time.Hour,
			},
			want: ctrl.Result{
				RequeueAfter: time.Hour,
			},
			wantErr: false,
		},
		{
			name: "api obj not found -> do not requeue",
			apiObj: &fakeApiImpl{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testObjectName,
					Namespace: testNamespaceName,
					DeletionTimestamp: &metav1.Time{
						Time: time.Now(),
					},
					Finalizers: []string{"string"},
				},
			},
			req: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      testObjectNameNotFound,
					Namespace: testNamespaceName,
				},
			},
			providerConfig: &fakeProviderConfigImpl{
				FakePollInterval: time.Hour,
			},
			want:    ctrl.Result{},
			wantErr: false,
		},
		{
			name: "provider config not found -> error",
			apiObj: &fakeApiImpl{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testObjectName,
					Namespace: testNamespaceName,
				},
			},
			req: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      testObjectName,
					Namespace: testNamespaceName,
				},
			},
			want:    ctrl.Result{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			onboardingCluster := createFakeCluster(t, "onboarding", tt.apiObj)
			platformCluster := createFakeCluster(t, "platform")
			mockSPR := &MockServiceProviderReconciler{}
			r := NewSPReconciler[*fakeApiImpl, *fakeProviderConfigImpl](func() *fakeApiImpl {
				return &fakeApiImpl{}
			}).
				WithOnboardingCluster(onboardingCluster).
				WithPlatformCluster(platformCluster).
				WithClusterAccessReconciler(FakeClusterAccessProvider{
					ManagedControlPlane: createFakeCluster(t, testMCPName),
					ManagedControlPlaneAR: &clustersv1alpha1.AccessRequest{
						ObjectMeta: metav1.ObjectMeta{
							Name:      testMCPName,
							Namespace: testNamespaceName,
						},
						Status: clustersv1alpha1.AccessRequestStatus{
							SecretRef: &common.LocalObjectReference{
								Name: testMCPKubeconfig,
							},
						},
					},
					Workload: createFakeCluster(t, testWorkloadName),
					WorkloadAR: &clustersv1alpha1.AccessRequest{
						ObjectMeta: metav1.ObjectMeta{
							Name:      testWorkloadName,
							Namespace: testNamespaceName,
						},
						Status: clustersv1alpha1.AccessRequestStatus{
							SecretRef: &common.LocalObjectReference{
								Name: testWorkloadKubeconfig,
							},
						},
					},
				}).
				WithServiceProviderReconciler(mockSPR).
				WithWorkloadCluster(true)
			if tt.providerConfig != nil {
				r.WithProviderConfig(tt.providerConfig)
			}
			got, gotErr := r.Reconcile(context.Background(), tt.req)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("Reconcile() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("Reconcile() succeeded unexpectedly")
			}
			assert.Equal(t, tt.want, got)
			if tt.req.Name != testObjectNameNotFound {
				// assert that the generic reconciler delegates objects to the target reconciler as expected
				assert.Equal(t, client.ObjectKeyFromObject(tt.apiObj), client.ObjectKeyFromObject(mockSPR.apiObj))
				assert.Equal(t, client.ObjectKeyFromObject(tt.providerConfig), client.ObjectKeyFromObject(mockSPR.pcObj))
				assert.Equal(t, client.ObjectKey{
					Namespace: tt.req.Namespace,
					Name:      testMCPKubeconfig,
				}, mockSPR.contextObj.MCPAccessSecretKey)
				assert.Equal(t, client.ObjectKey{
					Namespace: tt.req.Namespace,
					Name:      testWorkloadKubeconfig,
				}, mockSPR.contextObj.WorkloadAccessSecretKey)
			}
		})
	}
}

var _ ClusterAccessProvider = FakeClusterAccessProvider{}
var _ ServiceProviderReconciler[*fakeApiImpl, *fakeProviderConfigImpl] = &MockServiceProviderReconciler{}

type MockServiceProviderReconciler struct {
	apiObj     ServiceProviderAPI
	pcObj      ProviderConfig
	contextObj ClusterContext
}

// CreateOrUpdate implements [runtime.ServiceProviderReconciler].
func (f *MockServiceProviderReconciler) CreateOrUpdate(_ context.Context, obj *fakeApiImpl, pc *fakeProviderConfigImpl, cc ClusterContext) (ctrl.Result, error) {
	f.apiObj = obj
	f.pcObj = pc
	f.contextObj = cc
	return reconcile.Result{}, nil
}

// Delete implements [runtime.ServiceProviderReconciler].
func (f *MockServiceProviderReconciler) Delete(_ context.Context, obj *fakeApiImpl, pc *fakeProviderConfigImpl, cc ClusterContext) (ctrl.Result, error) {
	f.apiObj = obj
	f.pcObj = pc
	f.contextObj = cc
	return reconcile.Result{}, nil
}

type FakeClusterAccessProvider struct {
	ManagedControlPlane   *clusters.Cluster
	ManagedControlPlaneAR *clustersv1alpha1.AccessRequest
	Workload              *clusters.Cluster
	WorkloadAR            *clustersv1alpha1.AccessRequest
}

// MCPAccessRequest implements [ClusterAccessProvider].
func (f FakeClusterAccessProvider) MCPAccessRequest(ctx context.Context, request reconcile.Request) (*clustersv1alpha1.AccessRequest, error) {
	return f.ManagedControlPlaneAR, nil
}

// MCPCluster implements [ClusterAccessProvider].
func (f FakeClusterAccessProvider) MCPCluster(ctx context.Context, request reconcile.Request) (*clusters.Cluster, error) {
	return f.ManagedControlPlane, nil
}

// Reconcile implements [ClusterAccessProvider].
func (f FakeClusterAccessProvider) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

// ReconcileDelete implements [ClusterAccessProvider].
func (f FakeClusterAccessProvider) ReconcileDelete(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

// WorkloadAccessRequest implements [ClusterAccessProvider].
func (f FakeClusterAccessProvider) WorkloadAccessRequest(ctx context.Context, request reconcile.Request) (*clustersv1alpha1.AccessRequest, error) {
	return f.WorkloadAR, nil
}

// WorkloadCluster implements [ClusterAccessProvider].
func (f FakeClusterAccessProvider) WorkloadCluster(ctx context.Context, request reconcile.Request) (*clusters.Cluster, error) {
	return f.Workload, nil
}

func createFakeCluster(t *testing.T, id string, clusterObjects ...client.Object) *clusters.Cluster {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = apiextv1.AddToScheme(scheme)
	_ = clustersv1alpha1.AddToScheme(scheme)
	scheme.AddKnownTypes(schema.GroupVersion{
		Group:   "openmcp.test",
		Version: "v1",
	}, &fakeApiImpl{}, &fakeProviderConfigImpl{})

	// init cluster with objects
	fakeClient := fake.NewClientBuilder().WithObjects(clusterObjects...).WithScheme(scheme).Build()
	return clusters.NewTestClusterFromClient(id, fakeClient)
}

var _ ServiceProviderAPI = &fakeApiImpl{}

type fakeApiImpl struct {
	metav1.TypeMeta
	metav1.ObjectMeta
	common.Status
}

func (f *fakeApiImpl) DeepCopyObject() runtime.Object {
	return f
}

func (f *fakeApiImpl) Finalizer() string {
	return "fakeFinalizer"
}

func (f *fakeApiImpl) GetConditions() *[]metav1.Condition {
	return nil
}

func (f *fakeApiImpl) GetStatus() any {
	return f.Status
}

func (f *fakeApiImpl) SetPhase(phase string) {
}
func (f *fakeApiImpl) SetObservedGeneration(g int64) {
}

var _ ProviderConfig = &fakeProviderConfigImpl{}

type fakeProviderConfigImpl struct {
	metav1.TypeMeta
	metav1.ObjectMeta
	FakePollInterval time.Duration
}

func (f *fakeProviderConfigImpl) DeepCopyObject() runtime.Object {
	return f
}

func (f *fakeProviderConfigImpl) PollInterval() time.Duration {
	return f.FakePollInterval
}
