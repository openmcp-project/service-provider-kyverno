package status

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openmcp-project/service-provider-kyverno/pkg/spruntime"
)

// ConditionsTerminatingFailed indicates terminating with synced false and an error message
func ConditionsTerminatingFailed(obj spruntime.ServiceProviderAPI, err string) {
	meta.SetStatusCondition(obj.GetConditions(), metav1.Condition{
		Type:               spruntime.ServiceProviderConditionReady,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: obj.GetGeneration(),
		Reason:             "Terminating",
		Message:            err,
	})
	obj.SetObservedGeneration(obj.GetGeneration())
	obj.SetPhase(spruntime.StatusPhaseTerminating)
}
