// Package actionstatus maintains shared typed action lifecycle conditions.
package actionstatus

import (
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Finalizer retains cleanup and unresolved execution obligations.
const Finalizer = "actions.stacks.org/action-cleanup"

// Terminal reports a frozen action outcome.
func Terminal(phase string) bool {
	return phase == "Completed" || phase == "Recovered" || phase == "Failed" || phase == "Inconclusive"
}

// Finish freezes the terminal projection while permitting later receipt facts to be copied.
func Finish(status *actionv1.BitcoinBlockGenerationStatus, generation int64, now time.Time, phase, reason, message string, effectObserved bool) {
	if Terminal(status.Phase) {
		return
	}
	finished := metav1.NewTime(now.UTC())
	status.FinishedAt = &finished
	Project(status, generation, now, phase, reason, message, effectObserved)
}

// Project maintains the common conditions and their phase projection in one status write.
func Project(status *actionv1.BitcoinBlockGenerationStatus, generation int64, now time.Time, phase, reason, message string, effectObserved bool) {
	status.Phase = phase
	admitted := metav1.ConditionFalse
	if status.AdmittedAt != nil {
		admitted = metav1.ConditionTrue
	}
	progressing := metav1.ConditionFalse
	if phase == "Active" || phase == "Recovering" {
		progressing = metav1.ConditionTrue
	}
	effect := metav1.ConditionFalse
	if status.BlocksGenerated > 0 || effectObserved {
		effect = metav1.ConditionTrue
	}
	if phase == "Inconclusive" {
		effect = metav1.ConditionUnknown
	}
	for name, conditionStatus := range map[string]metav1.ConditionStatus{"Admitted": admitted, "Progressing": progressing, "EffectObserved": effect} {
		meta.SetStatusCondition(&status.Conditions, metav1.Condition{Type: name, Status: conditionStatus, ObservedGeneration: generation, Reason: reason, Message: message, LastTransitionTime: metav1.NewTime(now.UTC())})
	}
}
