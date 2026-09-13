package participantworkload

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantworkload/stacksconfig"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PrivateInput pins one immutable Secret; only scoped resolvers read its bytes.
type PrivateInput struct {
	// Binding pins the exact Secret identity.
	Binding common.Binding `json:"binding"`
	// Key selects one private key or complete configuration entry.
	Key string `json:"key,omitempty"`
	// OwnerUID optionally requires a participant owner for runtime credentials.
	OwnerUID types.UID `json:"ownerUID,omitempty"`
}

// StacksConfigInput binds public rendering parameters and private resource identities.
type StacksConfigInput struct {
	// Namespace scopes every read and output.
	Namespace string `json:"namespace"`
	// ParticipantUID owns the configuration and report outputs.
	ParticipantUID types.UID `json:"participantUID"`
	// Kind selects the native node or consensus signer renderer.
	Kind api.ParticipantKind `json:"kind"`
	// PolicyDigest identifies the admitted compiled policy.
	PolicyDigest string `json:"policyDigest"`
	// Genesis binds the verified frozen chain object and its public digest.
	Genesis common.Binding `json:"genesis"`
	// CandidateDigest identifies pre-admission validation without a published genesis identity.
	CandidateDigest string `json:"candidateDigest,omitempty"`
	// Node contains public fixed native node parameters.
	Node stacksconfig.NodeParameters `json:"node"`
	// NodeHost selects the paired node RPC address for a signer.
	NodeHost string `json:"nodeHost,omitempty"`
	// Identity is the expected account identity already resolved publicly.
	Identity common.PublicIdentity `json:"identity"`
	// Key supplies only this actor's node or consensus scalar.
	Key PrivateInput `json:"key"`
	// ActorRPC supplies restricted Bitcoin client credentials, never mutation credentials.
	ActorRPC *PrivateInput `json:"actorRPC,omitempty"`
	// EventAuth identifies the stable node-owned authentication token.
	EventAuth PrivateInput `json:"eventAuth"`
	// Custom identifies an optional complete immutable configuration version.
	Custom *PrivateInput `json:"custom,omitempty"`
	// Customization contains the declared override and compatibility rules.
	Customization *common.Config `json:"customization,omitempty"`
	// Services supplies validated DNS-only substitutions.
	Services map[string]string `json:"services,omitempty"`
	// ServiceBindings pins allocated managed endpoints even across same-name replacement.
	ServiceBindings []common.Binding `json:"serviceBindings,omitempty"`
	// Config identifies the precreated immutable configuration output.
	Config common.Binding `json:"config"`
	// Report identifies the exact public result ConfigMap.
	Report common.Binding `json:"report"`
}

// StacksConfigReport publishes agreement and content identity without private bytes.
type StacksConfigReport struct {
	// InputDigest binds the complete public rendering request.
	InputDigest string `json:"inputDigest"`
	// ConfigDigest identifies the rendered native configuration.
	ConfigDigest string `json:"configDigest"`
	// GenesisDigest records the exact frozen public chain input.
	GenesisDigest string `json:"genesisDigest"`
	// Identity records the publicly verified private-key agreement.
	Identity common.PublicIdentity `json:"identity"`
	// Verified excludes deliberately divergent custom actors from protocol prerequisites.
	Verified bool `json:"verified"`
}

// RunStacksConfigResolver renders private actor configuration only in its scoped Job.
func RunStacksConfigResolver(ctx context.Context, c client.Client, in StacksConfigInput) error {
	return runStacksConfigResolver(ctx, c, in, false)
}

// RunStacksCandidateConfigResolver validates private candidate bytes without activating an actor.
func RunStacksCandidateConfigResolver(ctx context.Context, c client.Client, in StacksConfigInput) error {
	return runStacksConfigResolver(ctx, c, in, true)
}

// runStacksConfigResolver shares exact private rendering with explicit candidate enrollment.
func runStacksConfigResolver(ctx context.Context, c client.Client, in StacksConfigInput, candidate bool) error {
	validGenesis := in.CandidateDigest == "" && in.Genesis.UID != "" && in.Genesis.Kind == "StacksGenesis"
	if candidate {
		validGenesis = strings.HasPrefix(in.CandidateDigest, "sha256:") && in.Genesis.UID == "" && in.Genesis.Kind == ""
	}
	if in.Namespace == "" || in.ParticipantUID == "" || in.PolicyDigest == "" || !validGenesis || !strings.HasPrefix(in.Genesis.Fingerprint, "sha256:") {
		return fmt.Errorf("incomplete Stacks configuration binding")
	}
	if in.Kind != api.ParticipantStacksNode && in.Kind != api.ParticipantStacksSigner {
		return fmt.Errorf("unsupported Stacks actor kind")
	}
	key, err := readPrivateInput(ctx, c, in.Namespace, in.Key)
	if err != nil {
		return err
	}
	public, err := identity.FromPrivate(string(key.Data[in.Key.Key]))
	if err != nil || public.Address != in.Identity.Address || !strings.EqualFold(public.PublicKey, in.Identity.PublicKey) {
		return fmt.Errorf("private actor key disagrees with admitted public identity")
	}
	if in.EventAuth.OwnerUID == "" || in.EventAuth.Key != "token" {
		return fmt.Errorf("node-owned event authentication is required")
	}
	var event *corev1.Secret
	if in.Kind == api.ParticipantStacksNode {
		if in.EventAuth.OwnerUID != in.ParticipantUID {
			return fmt.Errorf("event authentication owner differs from node")
		}
		event, err = resolverSecret(ctx, c, BitcoinConfigInput{Namespace: in.Namespace, ParticipantUID: in.ParticipantUID}, in.EventAuth.Binding)
		if err == nil {
			err = generateEventToken(ctx, c, event)
		}
	} else {
		event, err = readPrivateInput(ctx, c, in.Namespace, in.EventAuth)
	}
	if err != nil {
		return err
	}
	if len(event.Data["token"]) != 64 {
		return fmt.Errorf("invalid immutable event authentication")
	}
	var generated []byte
	if in.Kind == api.ParticipantStacksNode {
		if in.ActorRPC == nil || in.ActorRPC.OwnerUID == "" || digest(in.Node.Chain) != in.Genesis.Fingerprint {
			return fmt.Errorf("node genesis or Bitcoin credential binding is incomplete")
		}
		rpc, err := readPrivateInput(ctx, c, in.Namespace, *in.ActorRPC)
		if err != nil {
			return err
		}
		generated, err = stacksconfig.Node(in.Node, stacksconfig.NodeSecrets{PrivateKey: string(key.Data[in.Key.Key]), RPCUsername: string(rpc.Data["username"]), RPCPassword: string(rpc.Data["password"]), EventToken: string(event.Data["token"])})
		if err != nil {
			return err
		}
	} else {
		generated, err = stacksconfig.Signer(in.NodeHost, string(key.Data[in.Key.Key]), string(event.Data["token"]))
		if err != nil {
			return err
		}
	}
	var custom []byte
	if in.Custom != nil {
		if in.Customization == nil || in.Customization.SecretRef == nil || in.Customization.SecretRef.Name != in.Custom.Binding.Name || in.Customization.SecretRef.Key != in.Custom.Key {
			return fmt.Errorf("custom configuration binding disagrees")
		}
		secret, err := readPrivateInput(ctx, c, in.Namespace, *in.Custom)
		if err != nil {
			return err
		}
		custom = secret.Data[in.Custom.Key]
	} else if in.Customization != nil && in.Customization.SecretRef != nil {
		return fmt.Errorf("custom configuration identity missing")
	}
	data, verified, err := stacksconfig.Apply(in.Kind, generated, custom, in.Customization, in.Services)
	if err != nil {
		return err
	}
	config, err := resolverSecret(ctx, c, BitcoinConfigInput{Namespace: in.Namespace, ParticipantUID: in.ParticipantUID}, in.Config)
	if err != nil {
		return err
	}
	if ptr.Deref(config.Immutable, false) {
		if len(config.Data) != 1 || !bytes.Equal(config.Data["config.toml"], data) {
			return fmt.Errorf("immutable configuration output disagrees")
		}
	} else {
		if len(config.Data) != 0 {
			return fmt.Errorf("partially populated configuration output")
		}
		base := config.DeepCopy()
		config.Data = map[string][]byte{"config.toml": data}
		config.Immutable = ptr.To(true)
		if err := c.Patch(ctx, config, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
	}
	result := StacksConfigReport{InputDigest: digest(in), ConfigDigest: digest(string(data)), GenesisDigest: in.Genesis.Fingerprint, Identity: in.Identity, Verified: verified}
	return writePublicConfigReport(ctx, c, in.Namespace, in.ParticipantUID, in.Report, result)
}

// readPrivateInput enforces immutable UID and optional node ownership inside the Job.
func readPrivateInput(ctx context.Context, c client.Client, namespace string, in PrivateInput) (*corev1.Secret, error) {
	if in.Binding.Kind != "Secret" || in.Binding.Name == "" || in.Binding.UID == "" {
		return nil, fmt.Errorf("private input lacks exact identity")
	}
	var secret corev1.Secret
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: in.Binding.Name}, &secret); err != nil {
		return nil, err
	}
	if secret.UID != in.Binding.UID || secret.DeletionTimestamp != nil || !ptr.Deref(secret.Immutable, false) || (in.OwnerUID != "" && !resolverOwned(&secret, in.OwnerUID)) {
		return nil, fmt.Errorf("private input identity unavailable")
	}
	if in.Key != "" && len(secret.Data[in.Key]) == 0 {
		return nil, fmt.Errorf("private input key missing")
	}
	return &secret, nil
}

// generateEventToken fills a node-owned output once, retaining committed tokens on retries.
func generateEventToken(ctx context.Context, c client.Client, secret *corev1.Secret) error {
	if ptr.Deref(secret.Immutable, false) {
		if len(secret.Data) != 1 || len(secret.Data["token"]) != 64 {
			return fmt.Errorf("invalid immutable event authentication")
		}
		return nil
	}
	if len(secret.Data) != 0 {
		return fmt.Errorf("partially populated event authentication")
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	base := secret.DeepCopy()
	secret.Data = map[string][]byte{"token": []byte(hex.EncodeToString(random[:]))}
	secret.Immutable = ptr.To(true)
	return c.Patch(ctx, secret, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// writePublicConfigReport publishes one exact immutable logical result using optimistic locking.
func writePublicConfigReport(ctx context.Context, c client.Client, namespace string, owner types.UID, ref common.Binding, result any) error {
	if ref.Kind != "ConfigMap" || ref.UID == "" {
		return fmt.Errorf("public report binding missing")
	}
	var report corev1.ConfigMap
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, &report); err != nil {
		return err
	}
	if report.UID != ref.UID || report.DeletionTimestamp != nil || !resolverOwned(&report, owner) {
		return fmt.Errorf("public report identity changed")
	}
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode public configuration report")
	}
	if current := report.Data["report.json"]; current != "" {
		if current != string(data) {
			return fmt.Errorf("public configuration report cannot change")
		}
		return nil
	}
	base := report.DeepCopy()
	if report.Data == nil {
		report.Data = map[string]string{}
	}
	report.Data["report.json"] = string(data)
	return c.Patch(ctx, &report, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// StacksConfigRules separates immutable private inputs from owned writable outputs.
func StacksConfigRules(in StacksConfigInput) []rbacv1.PolicyRule {
	read := []string{in.Key.Binding.Name}
	write := []string{in.Config.Name}
	if in.Kind == api.ParticipantStacksNode {
		write = append(write, in.EventAuth.Binding.Name)
	} else {
		read = append(read, in.EventAuth.Binding.Name)
	}
	if in.ActorRPC != nil {
		read = append(read, in.ActorRPC.Binding.Name)
	}
	if in.Custom != nil {
		read = append(read, in.Custom.Binding.Name)
	}
	sort.Strings(read)
	sort.Strings(write)
	return []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"secrets"}, ResourceNames: read, Verbs: []string{"get"}}, {APIGroups: []string{""}, Resources: []string{"secrets"}, ResourceNames: write, Verbs: []string{"get", "patch"}}, {APIGroups: []string{""}, Resources: []string{"configmaps"}, ResourceNames: []string{in.Report.Name}, Verbs: []string{"get", "patch"}}}
}
