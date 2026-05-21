package e2e

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/e2e-framework/klient/wait"
	k8sconditions "sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/openmcp-project/openmcp-testing/pkg/clusterutils"
	"github.com/openmcp-project/openmcp-testing/pkg/conditions"
	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/resources"
)

func TestServiceProvider(t *testing.T) {
	var onboardingList unstructured.UnstructuredList
	var domainObjList unstructured.UnstructuredList
	basicProviderTest := features.New("provider test").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			if _, err := resources.CreateObjectsFromDir(ctx, c, "platform"); err != nil {
				t.Errorf("failed to create platform cluster objects: %v", err)
			}
			return ctx
		}).
		Setup(providers.CreateMCP("test-mcp")).
		Assess("verify service can be successfully consumed",
			func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
				onboardingConfig, err := clusterutils.OnboardingConfig()
				if err != nil {
					t.Error(err)
					return ctx
				}
				objList, err := resources.CreateObjectsFromDir(ctx, onboardingConfig, "onboarding")
				if err != nil {
					t.Errorf("failed to create onboarding cluster objects: %v", err)
					return ctx
				}
				for _, obj := range objList.Items {
					if err := wait.For(conditions.Match(&obj, onboardingConfig, "Ready", corev1.ConditionTrue)); err != nil {
						t.Error(err)
					}
				}
				objList.DeepCopyInto(&onboardingList)
				return ctx
			},
		).
		Assess("verify domain objects can be created",
			func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
				mcpConfig, err := clusterutils.McpConfig()
				if err != nil {
					t.Error(err)
					return ctx
				}
				objList, err := resources.CreateObjectsFromDir(ctx, mcpConfig, "mcp")
				if err != nil {
					t.Errorf("failed to create mcp cluster objects: %v", err)
					return ctx
				}
				objList.DeepCopyInto(&domainObjList)
				return ctx
			},
		).
		Assess("verify error on deletion with still existing domain resources",
			func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
				onboardingConfig, err := clusterutils.OnboardingConfig()
				if err != nil {
					t.Error(err)
					return ctx
				}
				//TODO: move to method
				for _, obj := range onboardingList.Items {
					if err := resources.DeleteObject(ctx, onboardingConfig, &obj); err != nil {
						t.Errorf("failed to initiate deletion of onboarding object: %v", err)
						return ctx
					}
				}
				for _, obj := range onboardingList.Items {
					if err := wait.For(conditions.Match(&obj, onboardingConfig, "Ready", corev1.ConditionFalse), wait.WithTimeout(time.Minute)); err != nil {
						t.Error(err)
					}
				}
				return ctx
			},
		).
		Assess("verify serviceprovider is removed after domain objects are deleted",
			func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
				mcpConfig, err := clusterutils.McpConfig()
				if err != nil {
					t.Error(err)
					return ctx
				}
				onboardingConfig, err := clusterutils.OnboardingConfig()
				if err != nil {
					t.Error(err)
					return ctx
				}
				for _, obj := range domainObjList.Items {
					if err := resources.DeleteObject(ctx, mcpConfig, &obj, wait.WithTimeout(time.Minute)); err != nil {
						t.Errorf("failed to delete domain object: %v", err)
					}
				}
				for _, obj := range onboardingList.Items {
					if err := wait.For(
						k8sconditions.New(onboardingConfig.Client().Resources()).ResourceDeleted(&obj),
						wait.WithTimeout(2*time.Minute),
					); err != nil {
						t.Error(err)
					}
				}
				return ctx
			},
		).
		Teardown(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			//TODO: move to method or testing utils
			onboardingConfig, err := clusterutils.OnboardingConfig()
			if err != nil {
				t.Error(err)
				return ctx
			}
			for _, obj := range onboardingList.Items {
				if err := resources.DeleteObject(ctx, onboardingConfig, &obj, wait.WithTimeout(time.Minute)); err != nil {
					t.Errorf("failed to delete onboarding object: %v", err)
				}
			}
			return ctx
		}).
		Teardown(providers.DeleteMCP("test-mcp", wait.WithTimeout(5*time.Minute)))
	testenv.Test(t, basicProviderTest.Feature())
}
