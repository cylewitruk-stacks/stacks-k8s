package participantworkload

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestBitcoinObserverCredentialAndWorkloadBoundary(t *testing.T) {
	c, in, _ := bitcoinCustomFixture(t)
	in.Custom = nil
	in.Customization = nil
	p := participantFixture()
	observer := &corev1.Secret{ObjectMeta: objectMeta(p, "rpc-observer", "support")}
	observer.UID = "observer-uid"
	if err := c.Create(t.Context(), observer); err != nil {
		t.Fatal(err)
	}
	in.ObserverCredentials = binding("Secret", observer)
	for range 2 {
		if err := RunBitcoinConfigResolver(t.Context(), c, in); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(observer), observer); err != nil {
		t.Fatal(err)
	}
	var config corev1.Secret
	if err := c.Get(t.Context(), client.ObjectKey{Namespace: in.Namespace, Name: in.Config.Name}, &config); err != nil {
		t.Fatal(err)
	}
	server := string(config.Data["bitcoin.conf"])
	for _, ref := range []common.Binding{in.ActorCredentials, in.ControlCredentials} {
		var principal corev1.Secret
		if err := c.Get(
			t.Context(),
			client.ObjectKey{Namespace: in.Namespace, Name: ref.Name},
			&principal,
		); err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(observer.Data["password"], principal.Data["password"]) {
			t.Fatal("observer shares another principal's credential")
		}
	}

	if !ptr.Deref(observer.Immutable, false) || string(observer.Data["username"]) != "observer" ||
		strings.Contains(server, string(observer.Data["password"])) {
		t.Fatal("credential boundary")
	}
	for _, method := range []string{
		"getblockchaininfo", "getchaintips", "getnetworkinfo", "getmempoolinfo", "getnettotals",
	} {
		if !renderedRPCAllows(server, "observer", method) {
			t.Fatal("missing observer permission", method)
		}
	}
	for _, method := range []string{
		"generatetoaddress", "invalidateblock", "reconsiderblock", "sendrawtransaction", "stop", "createwallet",
	} {
		if renderedRPCAllows(server, "observer", method) {
			t.Fatal("observer mutation allowed", method)
		}
	}
	grants := 0
	for _, rule := range BitcoinConfigRules(in) {
		for _, name := range rule.ResourceNames {
			if name == observer.Name {
				grants++
				if !reflect.DeepEqual(rule.Verbs, []string{"get", "patch"}) {
					t.Fatal("unexpected resolver access")
				}
			}
		}
	}
	if grants != 1 {
		t.Fatal("credential grant missing or duplicated")
	}
	state := &api.ParticipantRuntimeStatus{ObserverRPCSecretRef: in.ObserverCredentials}
	workload, err := StatefulSet(p, "config", "actor", 1)
	if err != nil {
		t.Fatal(err)
	}
	before := workload.DeepCopy()
	if err := attachObserver(p, state, workload, "operator:test"); err != nil || !reflect.DeepEqual(before, workload) {
		t.Fatal("omission must be opt-out")
	}
	p.Status.Admission.Configuration.BitcoinNode.Observer = &bitcoin.BitcoinObserverSpec{
		Enabled:         ptr.To(true),
		IntervalSeconds: ptr.To[int32](5),
	}
	if err := attachObserver(p, state, workload, "operator:test"); err != nil {
		t.Fatal(err)
	}
	pod := workload.Spec.Template.Spec
	if len(pod.Containers) != 2 || !reflect.DeepEqual(pod.Containers[0], before.Spec.Template.Spec.Containers[0]) {
		t.Fatal("native process altered")
	}
	sidecar := pod.Containers[1]
	if sidecar.Name != "bitcoin-observer" || sidecar.Image != "operator:test" || sidecar.Ports[0].Name != "metrics" ||
		sidecar.ReadinessProbe != nil ||
		sidecar.LivenessProbe != nil ||
		len(sidecar.Env) != 0 ||
		len(sidecar.VolumeMounts) != 1 ||
		sidecar.VolumeMounts[0].MountPath != "/rpc" ||
		!sidecar.VolumeMounts[0].ReadOnly ||
		ptr.Deref(pod.AutomountServiceAccountToken, true) {
		t.Fatal("sidecar authority or health coupling")
	}
	data, _ := json.Marshal(pod)
	if strings.Contains(string(data), string(observer.Data["password"])) {
		t.Fatal("plaintext credential escaped")
	}
	if err := attachObserver(p, &api.ParticipantRuntimeStatus{}, before, "operator:test"); err == nil {
		t.Fatal("missing binding accepted")
	}
	in.ObserverCredentials.UID = "replacement"
	if err := RunBitcoinConfigResolver(context.Background(), c, in); err == nil {
		t.Fatal("replacement credential accepted")
	}
}

// TestObserverDisabledRetainsCredentialPin leaves deletion/recreation identity checks to re-enablement.
func TestObserverDisabledRetainsCredentialPin(t *testing.T) {
	p := participantFixture()
	p.Status.Admission.Configuration.BitcoinNode.Observer = &bitcoin.BitcoinObserverSpec{Enabled: ptr.To(false)}
	state := &api.ParticipantRuntimeStatus{ObserverRPCSecretRef: &common.Binding{Name: "old", UID: "uid"}}
	workload, err := StatefulSet(p, "config", "actor", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := attachObserver(
		p,
		state,
		workload,
		"",
	); err != nil || len(workload.Spec.Template.Spec.Containers) != 1 ||
		state.ObserverRPCSecretRef.UID != "uid" {
		t.Fatal("disabled observer changes retained identity")
	}
}
