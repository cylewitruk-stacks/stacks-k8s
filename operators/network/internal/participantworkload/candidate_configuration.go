package participantworkload

import (
	"context"
	"fmt"
	"sort"
	"strings"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantstatus"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// candidateConfigurationDigest binds rendering to semantic chain and exact candidate identities.
func candidateConfigurationDigest(in foundation.CandidateConfiguration) string {
	type participant struct {
		UID    string
		Policy *api.Admission
	}
	policies := map[string]participant{}
	for name, p := range in.Participants {
		policies[name] = participant{UID: string(p.UID), Policy: p.Status.Admission}
	}
	return digest(struct {
		NetworkUID   string
		Chain        api.Chain
		Participants map[string]participant
	}{string(in.Root.UID), in.Genesis.Chain, policies})
}

// ValidateConfiguration prepares only support artifacts and validates the selected actor image.
func (r *Reconciler) ValidateConfiguration(ctx context.Context, in foundation.CandidateConfiguration, name string) (bool, error) {
	local := *r
	snapshot := in
	snapshot.Participants = map[string]*api.StacksNetworkParticipant{}
	for name, p := range in.Participants {
		snapshot.Participants[name] = p.DeepCopy()
	}
	local.candidateConfiguration = &snapshot
	wanted := snapshot.Participants[name]
	if wanted == nil || (wanted.Spec.Kind != api.ParticipantStacksNode && wanted.Spec.Kind != api.ParticipantStacksSigner && wanted.Spec.Kind != api.ParticipantBitcoinNode) {
		return false, fmt.Errorf("unsupported candidate configuration kind")
	}
	needed := map[string]bool{}
	var require func(string) error
	require = func(name string) error {
		if needed[name] {
			return nil
		}
		p := snapshot.Participants[name]
		if p == nil || p.Status.Admission == nil {
			return fmt.Errorf("candidate actor dependency unavailable")
		}
		needed[name] = true
		configuration := p.Status.Admission.Configuration
		if p.Spec.Kind == api.ParticipantStacksNode {
			if configuration.StacksNode == nil || configuration.StacksNode.BitcoinNodeRef == nil {
				return fmt.Errorf("candidate Bitcoin dependency unavailable")
			}
			return require(configuration.StacksNode.BitcoinNodeRef.Name)
		}
		if p.Spec.Kind == api.ParticipantStacksSigner {
			if configuration.StacksSigner == nil || configuration.StacksSigner.NodeRef == nil {
				return fmt.Errorf("candidate signer node unavailable")
			}
			return require(configuration.StacksSigner.NodeRef.Name)
		}
		if p.Spec.Kind != api.ParticipantBitcoinNode {
			return fmt.Errorf("unsupported candidate actor dependency")
		}
		return nil
	}
	if err := require(name); err != nil {
		return false, err
	}
	names := []string{}
	for name := range needed {
		names = append(names, name)
	}
	sort.Strings(names)
	// Allocate only stable endpoints; actor StatefulSets remain behind published genesis.
	for _, name := range names {
		p := snapshot.Participants[name]
		if err := local.checkCandidateParticipant(ctx, p); err != nil {
			return false, err
		}
		for _, service := range Services(p) {
			if err := local.reconcileService(ctx, p, service); err != nil {
				return false, err
			}
		}
	}
	for _, kind := range []api.ParticipantKind{api.ParticipantBitcoinNode, api.ParticipantStacksNode, api.ParticipantStacksSigner} {
		for _, name := range names {
			p := snapshot.Participants[name]
			if p.Spec.Kind != kind {
				continue
			}
			state := api.ParticipantRuntimeStatus{ObservedGeneration: p.Generation}
			if p.Status.Runtime != nil {
				state = *p.Status.Runtime.DeepCopy()
			}
			var ready bool
			var err error
			if kind == api.ParticipantBitcoinNode {
				ready, err = local.configuration(ctx, in.Root, p, &state)
			} else {
				ready, _, err = local.stacksConfiguration(ctx, in.Root, p, &state)
			}
			if err != nil {
				return false, err
			}
			p.Status.Runtime = &state
			if err := local.publishCandidateBindings(ctx, p, &state); err != nil {
				return false, err
			}
			if !ready {
				return false, nil
			}
		}
	}
	// Bitcoin validation is syntax and managed-setting agreement inside its resolver.
	// Core semantic compatibility remains an actor startup/readiness check.
	if wanted.Spec.Kind == api.ParticipantBitcoinNode {
		return true, nil
	}
	return local.validateCandidateImage(ctx, snapshot.Participants[name], candidateConfigurationDigest(snapshot))
}

// checkCandidateParticipant requires the currently allocated object behind a candidate snapshot.
func (r *Reconciler) checkCandidateParticipant(ctx context.Context, p *api.StacksNetworkParticipant) error {
	var actual api.StacksNetworkParticipant
	if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(p), &actual); err != nil {
		return err
	}
	root := r.candidateConfiguration.Root
	if actual.UID != p.UID || actual.DeletionTimestamp != nil || !metav1.IsControlledBy(&actual, root) || !selected(root, &actual) || actual.Spec.Kind != p.Spec.Kind {
		return fmt.Errorf("candidate participant identity changed")
	}
	return nil
}

// readConfigurationParticipant selects explicit candidate inputs only within support preparation.
func (r *Reconciler) readConfigurationParticipant(ctx context.Context, key client.ObjectKey, out *api.StacksNetworkParticipant) error {
	if err := r.Reader.Get(ctx, key, out); err != nil {
		return err
	}
	if r.candidateConfiguration == nil {
		return nil
	}
	expected := r.candidateConfiguration.Participants[out.Spec.ParticipantName]
	if expected == nil || expected.UID != out.UID || out.DeletionTimestamp != nil || !metav1.IsControlledBy(out, r.candidateConfiguration.Root) {
		return fmt.Errorf("candidate render participant changed")
	}
	*out = *expected.DeepCopy()
	return nil
}

// publishCandidateBindings pins stable private artifacts without replacing active runtime configuration.
func (r *Reconciler) publishCandidateBindings(ctx context.Context, p *api.StacksNetworkParticipant, prepared *api.ParticipantRuntimeStatus) error {
	var actual api.StacksNetworkParticipant
	if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(p), &actual); err != nil {
		return err
	}
	if actual.UID != p.UID || actual.DeletionTimestamp != nil {
		return fmt.Errorf("candidate artifact owner changed")
	}
	state := api.ParticipantRuntimeStatus{ObservedGeneration: actual.Generation}
	if actual.Status.Runtime != nil {
		state = *actual.Status.Runtime.DeepCopy()
	}
	for _, pair := range []struct {
		target **common.Binding
		source *common.Binding
	}{{&state.RPCSecretRef, prepared.RPCSecretRef}, {&state.ActorRPCSecretRef, prepared.ActorRPCSecretRef}, {&state.EventAuthSecretRef, prepared.EventAuthSecretRef}} {
		if pair.source == nil {
			continue
		}
		if *pair.target != nil && ((*pair.target).Name != pair.source.Name || (*pair.target).UID != pair.source.UID) {
			return fmt.Errorf("candidate changed a protected credential identity")
		}
		copy := *pair.source
		*pair.target = &copy
	}
	if equality.Semantic.DeepEqual(actual.Status.Runtime, &state) {
		return nil
	}
	return participantstatus.Apply(ctx, r.Client, &actual, api.ParticipantStatus{Runtime: &state, Conditions: ownConditions(actual.Status.Conditions)}, "stacks-network-domain-"+strings.ToLower(string(p.Spec.Kind)))
}

// validateCandidateImage runs only native validation with a single immutable configuration mount.
func (r *Reconciler) validateCandidateImage(ctx context.Context, p *api.StacksNetworkParticipant, candidate string) (bool, error) {
	state := p.Status.Runtime
	if state == nil || state.ConfigRef == nil || state.ConfigRef.Fingerprint == "" {
		return false, nil
	}
	fields, err := actorFields(p)
	if err != nil {
		return false, err
	}
	if ptr.Deref(fields.Image, "") == "" {
		return false, fmt.Errorf("candidate actor image is missing")
	}
	revision := digest(struct {
		Candidate string
		Config    common.Binding
		Image     string
	}{candidate, *state.ConfigRef, ptr.Deref(fields.Image, "")})
	job := &batchv1.Job{
		ObjectMeta: objectMeta(p, "validate-"+strings.TrimPrefix(revision, "sha256:"), "support"),
		Spec: batchv1.JobSpec{
			BackoffLimit: ptr.To[int32](0), ActiveDeadlineSeconds: ptr.To[int64](120),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: Labels(p, "support")},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: ptr.To(false), RestartPolicy: corev1.RestartPolicyNever,
					SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To[int64](1000), RunAsGroup: ptr.To[int64](1000), FSGroup: ptr.To[int64](1000)},
					Containers: []corev1.Container{{
						Name: "validate", Image: *fields.Image, ImagePullPolicy: ptr.Deref(fields.ImagePullPolicy, corev1.PullIfNotPresent),
						Command:         []string{"sh", "-ec", `exec "$1" check-config --config /config/config.toml >/dev/null 2>&1`, "--", actorContainer(p.Spec.Kind)},
						SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
						VolumeMounts:    []corev1.VolumeMount{{Name: "config", MountPath: "/config", ReadOnly: true}},
					}},
					Volumes: []corev1.Volume{{Name: "config", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: state.ConfigRef.Name, DefaultMode: ptr.To[int32](0440)}}}},
				},
			},
		},
	}
	job.Annotations = map[string]string{candidateConfigurationAnnotation: revision}
	if r.candidateConfiguration != nil && r.candidateConfiguration.Root.Spec.Defaults != nil {
		applyPlacement(&job.Spec.Template.Spec, r.candidateConfiguration.Root.Spec.Defaults.WorkerPlacement, Labels(p, "support"))
	}
	if err := r.createOwned(ctx, p, job); err != nil {
		return false, err
	}
	var actual batchv1.Job
	if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(job), &actual); err != nil {
		return false, err
	}
	if actual.Annotations[candidateConfigurationAnnotation] != revision || len(actual.Spec.Template.Spec.Containers) != 1 || actual.Spec.Template.Spec.Containers[0].Image != *fields.Image || !equality.Semantic.DeepEqual(actual.Spec.Template.Spec.Containers[0].Command, job.Spec.Template.Spec.Containers[0].Command) || !equality.Semantic.DeepEqual(actual.Spec.Template.Spec.Volumes, job.Spec.Template.Spec.Volumes) {
		return false, fmt.Errorf("candidate image validation identity changed")
	}
	if err := r.privateMetadata(ctx, p.Namespace, *state.ConfigRef, p.UID); err != nil {
		return false, err
	}
	for _, condition := range actual.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return false, fmt.Errorf("selected image rejected candidate native configuration")
		}
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			return true, nil
		}
	}
	return false, nil
}
