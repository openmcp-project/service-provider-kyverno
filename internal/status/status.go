package status

import (
	"github.com/openmcp-project/opencontrolplane-runtime/pkg/serviceprovider"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openmcp-project/service-provider-kyverno/api/v1alpha1"
)

// StatusPhaseFailed indicates failed reconciliations
const StatusPhaseFailed = "Failed"

// ConditionsTerminatingFailed indicates terminating with synced false and an error message
func ConditionsTerminatingFailed(obj *v1alpha1.Kyverno, err string) {
	meta.SetStatusCondition(obj.GetConditions(), metav1.Condition{
		Type:               serviceprovider.ServiceProviderConditionReady,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: obj.GetGeneration(),
		Reason:             "Terminating",
		Message:            err,
	})
	obj.SetObservedGeneration(obj.GetGeneration())
	obj.SetPhase(serviceprovider.StatusPhaseTerminating)
}

// Failed indicates failed with ready false
func Failed(obj *v1alpha1.Kyverno, msg string) {
	meta.SetStatusCondition(obj.GetConditions(), metav1.Condition{
		Type:               serviceprovider.ServiceProviderConditionReady,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: obj.GetGeneration(),
		Reason:             "ReconcileFailed",
		Message:            msg,
	})
	obj.SetObservedGeneration(obj.GetGeneration())
	obj.SetPhase(StatusPhaseFailed)
}
