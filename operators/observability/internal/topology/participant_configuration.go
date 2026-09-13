package topology

import (
	"context"
	"encoding/json"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// observeConfigurationReport corroborates public output identity without accessing private bytes.
func observeConfigurationReport(ctx context.Context, reads *directRead, p *api.StacksNetworkParticipant) (types.UID, error) {
	reports := &corev1.ConfigMapList{}
	if err := reads.reader.List(ctx, reports, client.InNamespace(p.Namespace), client.MatchingLabels{"network.stacks.org/participant-uid": string(p.UID)}, client.Limit(1001)); err != nil {
		return "", err
	}
	if reports.Continue != "" || len(reports.Items) > 1000 {
		return "", &InconclusiveError{Reason: "configuration report view is incomplete"}
	}
	var matched types.UID
	for i := range reports.Items {
		report := &reports.Items[i]
		if !exactOwner(report, api.GroupVersion.String(), "StacksNetworkParticipant", p.Name, p.UID) {
			continue
		}
		raw := report.Data["input.json"]
		result := report.Data["report.json"]
		if len(raw) > 1048576 || len(result) > 4096 {
			return "", &InconclusiveError{Reason: "configuration report exceeds its input bound"}
		}
		var input struct {
			Namespace      string         `json:"namespace"`
			ParticipantUID types.UID      `json:"participantUID"`
			PolicyDigest   string         `json:"policyDigest"`
			Config         common.Binding `json:"config"`
			Report         common.Binding `json:"report"`
		}
		if json.Unmarshal([]byte(raw), &input) != nil || input.Config.Name != p.Status.Runtime.ConfigRef.Name {
			continue
		}
		var output struct {
			InputDigest  string `json:"inputDigest"`
			ConfigDigest string `json:"configDigest"`
		}
		if report.DeletionTimestamp != nil || report.UID == "" || matched != "" || input.Namespace != p.Namespace || input.ParticipantUID != p.UID || input.PolicyDigest != p.Status.Admission.PolicyDigest || input.Config.Kind != "Secret" || input.Config.UID != p.Status.Runtime.ConfigRef.UID || input.Report.Kind != "ConfigMap" || input.Report.Name != report.Name || input.Report.UID != report.UID || json.Unmarshal([]byte(result), &output) != nil || output.InputDigest != bytesDigest([]byte(raw)) || output.ConfigDigest != p.Status.Runtime.ConfigRef.Fingerprint {
			return "", &InconclusiveError{Reason: "public configuration report binding differs"}
		}
		matched = report.UID
		reads.objects = append(reads.objects, report.DeepCopy())
	}
	if matched == "" {
		return "", &NotReadyError{Reason: "public configuration report is unavailable"}
	}
	return matched, nil
}
