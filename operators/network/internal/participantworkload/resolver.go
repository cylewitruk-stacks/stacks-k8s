package participantworkload

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/bitcoinobserver"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantworkload/bitcoinconfig"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BitcoinConfigInput limits a resolver Job to exact precreated resources.
type BitcoinConfigInput struct {
	// Namespace scopes every input and output.
	Namespace string `json:"namespace"`
	// ParticipantUID identifies the resource owner.
	ParticipantUID types.UID `json:"participantUID"`
	// PolicyDigest identifies the admitted configuration revision.
	PolicyDigest string `json:"policyDigest"`
	// Config identifies the immutable server configuration output.
	Config common.Binding `json:"config"`
	// ControlCredentials identifies node-scoped mutation credentials.
	ControlCredentials common.Binding `json:"controlCredentials"`
	// ActorCredentials identifies restricted protocol-client credentials.
	ActorCredentials common.Binding `json:"actorCredentials"`
	// ObserverCredentials optionally pins the read-only observer principal.
	ObserverCredentials *common.Binding `json:"observerCredentials,omitempty"`
	// Report identifies the exact public result ConfigMap.
	Report common.Binding `json:"report"`
	// Seeds freezes startup peer DNS addresses without waiting for live peers.
	Seeds []string `json:"seeds,omitempty"`
	// Custom pins the optional immutable user-owned native configuration.
	Custom *PrivateInput `json:"custom,omitempty"`
	// Customization selects native overrides or complete configuration.
	Customization *common.Config `json:"customization,omitempty"`
	// Services contains admitted DNS-only substitutions.
	Services map[string]string `json:"services,omitempty"`
}

// BitcoinConfigReport attests exact public identities without revealing RPC credentials.
type BitcoinConfigReport struct {
	// InputDigest identifies the complete resolver input.
	InputDigest string `json:"inputDigest"`
	// ConfigDigest identifies the rendered server configuration.
	ConfigDigest string `json:"configDigest"`
	// Verified attests syntax and managed settings, not Core startup compatibility.
	Verified bool `json:"verified"`
}

const (
	actorMethods = "getblockchaininfo,getnetworkinfo,getblockcount,getblockhash,getblockheader," +
		"getblock,getrawtransaction,getrawmempool,getmempoolinfo,estimatesmartfee," +
		"listunspent,gettransaction,getwalletinfo,getaddressinfo,sendrawtransaction," +
		"listwallets"
	controlMethods = actorMethods + ",createwallet,loadwallet,unloadwallet,listwalletdir,importdescriptors," +
		"getdescriptorinfo,listdescriptors,validateaddress,generatetoaddress," +
		"getnewaddress,getpeerinfo,getchaintips,gettxout,addnode,disconnectnode," +
		"setnetworkactive,invalidateblock,reconsiderblock"
)

// RunBitcoinConfigResolver runs only inside the scoped Job process.
func RunBitcoinConfigResolver(ctx context.Context, c client.Client, in BitcoinConfigInput) error {
	if in.Namespace == "" || in.ParticipantUID == "" || in.PolicyDigest == "" {
		return fmt.Errorf("incomplete Bitcoin resolver binding")
	}
	control, err := resolverSecret(ctx, c, in, in.ControlCredentials)
	if err != nil {
		return err
	}
	actor, err := resolverSecret(ctx, c, in, in.ActorCredentials)
	if err != nil {
		return err
	}
	if err := generateCredential(ctx, c, control, "control"); err != nil {
		return err
	}
	if err := generateCredential(ctx, c, actor, common.BitcoinActorRPCUsername); err != nil {
		return err
	}
	config, err := resolverSecret(ctx, c, in, in.Config)
	if err != nil {
		return err
	}
	var observer *corev1.Secret
	if in.ObserverCredentials != nil {
		observer, err = resolverSecret(ctx, c, in, *in.ObserverCredentials)
		if err != nil {
			return err
		}
		if err := generateCredential(ctx, c, observer, bitcoinobserver.Username); err != nil {
			return err
		}
	}
	server, err := renderConfig(in.Seeds, actor, control, observer)
	if err != nil {
		return err
	}
	var custom []byte
	if in.Custom != nil {
		if in.Customization == nil || in.Customization.SecretRef == nil ||
			in.Customization.SecretRef.Name != in.Custom.Binding.Name ||
			in.Customization.SecretRef.Key != in.Custom.Key {
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
	data, verified, err := bitcoinconfig.Apply([]byte(server), custom, in.Customization, in.Services)
	if err != nil {
		return err
	}
	if ptr.Deref(config.Immutable, false) {
		if len(config.Data) != 1 || !bytes.Equal(config.Data["bitcoin.conf"], data) {
			return fmt.Errorf("immutable configuration output disagrees")
		}
	} else {
		if len(config.Data) != 0 {
			return fmt.Errorf("partially populated configuration is not reusable")
		}
		base := config.DeepCopy()
		config.Data = map[string][]byte{"bitcoin.conf": data}
		config.Immutable = ptr.To(true)
		if err := c.Patch(
			ctx,
			config,
			client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}),
		); err != nil {
			return err
		}
	}
	var report corev1.ConfigMap
	if err := c.Get(ctx, types.NamespacedName{
		Namespace: in.Namespace,
		Name:      in.Report.Name,
	}, &report); err != nil {
		return err
	}
	if report.UID != in.Report.UID || !resolverOwned(&report, in.ParticipantUID) || report.DeletionTimestamp != nil {
		return fmt.Errorf("resolver report identity changed")
	}
	result := BitcoinConfigReport{
		InputDigest:  digest(in),
		ConfigDigest: digest(string(config.Data["bitcoin.conf"])),
		Verified:     verified,
	}
	data, _ = json.Marshal(result)
	if current := report.Data["report.json"]; current != "" {
		if current != string(data) {
			return fmt.Errorf("resolver report cannot change")
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

// resolverOwned verifies ownership of a scoped resolver input or output.
func resolverOwned(obj metav1.Object, uid types.UID) bool {
	owner := metav1.GetControllerOf(obj)
	return owner != nil && owner.APIVersion == api.GroupVersion.String() &&
		owner.Kind == api.KindStacksNetworkParticipant &&
		owner.UID == uid
}

// resolverSecret reads private data only inside a resolver with exact UID checks.
func resolverSecret(
	ctx context.Context,
	c client.Client,
	in BitcoinConfigInput,
	ref common.Binding,
) (*corev1.Secret, error) {
	if ref.UID == "" || ref.Kind != common.KindSecret {
		return nil, fmt.Errorf("missing exact Secret binding")
	}
	var secret corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: in.Namespace, Name: ref.Name}, &secret); err != nil {
		return nil, err
	}
	if secret.UID != ref.UID || !resolverOwned(&secret, in.ParticipantUID) || secret.DeletionTimestamp != nil {
		return nil, fmt.Errorf("resolver Secret identity changed")
	}
	return &secret, nil
}

// generateCredential fills an empty output once and preserves committed credentials on retry.
func generateCredential(ctx context.Context, c client.Client, secret *corev1.Secret, username string) error {
	if ptr.Deref(secret.Immutable, false) {
		if string(secret.Data["username"]) != username || len(secret.Data["password"]) != 64 {
			return fmt.Errorf("invalid immutable RPC credential")
		}
		return nil
	}
	if len(secret.Data) != 0 {
		return fmt.Errorf("partially populated RPC credentials")
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	base := secret.DeepCopy()
	secret.Data = map[string][]byte{
		"username": []byte(username),
		"password": []byte(hex.EncodeToString(random[:])),
	}
	secret.Immutable = ptr.To(true)
	return c.Patch(ctx, secret, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// renderConfig renders authenticated Core configuration with disjoint RPC method sets.
func renderConfig(seeds []string, actor, control, observer *corev1.Secret) (string, error) {
	var out strings.Builder
	out.WriteString(
		"regtest=1\nserver=1\nlisten=1\ntxindex=1\nprinttoconsole=1\nfallbackfee=0.0002\nrpcwhitelistdefault=1\n",
	)
	for _, credentials := range []*corev1.Secret{actor, control, observer} {
		if credentials == nil {
			continue
		}
		// Secret UIDs provide a unique salt while keeping rendering deterministic.
		saltBytes := sha256.Sum256([]byte(credentials.UID))
		salt := hex.EncodeToString(saltBytes[:16])
		mac := hmac.New(sha256.New, []byte(salt))
		mac.Write(credentials.Data["password"])
		fmt.Fprintf(&out, "rpcauth=%s:%s$%x\n", credentials.Data["username"], salt, mac.Sum(nil))
	}
	if observer != nil {
		fmt.Fprintf(
			&out,
			"rpcwhitelist=%s:%s\n",
			bitcoinobserver.Username,
			strings.Join(bitcoinobserver.Methods(), ","),
		)
	}
	fmt.Fprintf(
		&out,
		"rpcwhitelist=actor:%s\nrpcwhitelist=control:%s\n[regtest]\nrpcbind=0.0.0.0\nrpcallow"+
			"ip=0.0.0.0/0\nrpcport=18443\nport=18444\n",
		actorMethods,
		controlMethods,
	)
	for _, seed := range seeds {
		if strings.ContainsAny(seed, "\r\n= \t") || seed == "" {
			return "", fmt.Errorf("invalid peer seed")
		}
		fmt.Fprintf(&out, "addnode=%s:18444\n", seed)
	}
	return out.String(), nil
}

// BitcoinConfigRules grants only named resolver inputs and output patches.
func BitcoinConfigRules(in BitcoinConfigInput) []rbacv1.PolicyRule {
	rules := []rbacv1.PolicyRule{
		{
			APIGroups:     []string{""},
			Resources:     []string{"secrets"},
			ResourceNames: []string{in.Config.Name, in.ControlCredentials.Name, in.ActorCredentials.Name},
			Verbs:         []string{"get", "patch"},
		},
		{
			APIGroups:     []string{""},
			Resources:     []string{"configmaps"},
			ResourceNames: []string{in.Report.Name},
			Verbs:         []string{"get", "patch"},
		},
	}
	if in.ObserverCredentials != nil {
		rules[0].ResourceNames = append(rules[0].ResourceNames, in.ObserverCredentials.Name)
	}
	if in.Custom != nil {
		rules = append(
			rules,
			rbacv1.PolicyRule{
				APIGroups:     []string{""},
				Resources:     []string{"secrets"},
				ResourceNames: []string{in.Custom.Binding.Name},
				Verbs:         []string{"get"},
			},
		)
	}
	return rules
}

// provisionResolver creates one input-bound Job and its narrow named-resource permissions.
func (r *Reconciler) provisionResolver(
	ctx context.Context,
	p *api.StacksNetworkParticipant,
	in BitcoinConfigInput,
) error {
	data, _ := json.Marshal(in)
	return r.provisionConfigurationJob(
		ctx,
		p,
		in.PolicyDigest,
		ModeResolveBitcoinConfig,
		data,
		BitcoinConfigRules(in),
		p.Status.Admission.Configuration.BitcoinNode.WorkerPlacement,
		in.Report.Name,
	)
}

// provisionConfigurationJob shares bounded support-job lifecycle across native renderers.
func (r *Reconciler) provisionConfigurationJob(
	ctx context.Context,
	p *api.StacksNetworkParticipant,
	revision, mode string,
	data []byte,
	rules []rbacv1.PolicyRule,
	placement *common.Placement,
	inputConfigMap string,
) error {
	if r.ResolverImage == "" {
		return fmt.Errorf("resolver image is required")
	}
	purpose := "resolve-" + strings.TrimPrefix(revision, "sha256:")
	metadata := objectMeta(p, purpose, api.RoleSupport)
	account := &corev1.ServiceAccount{ObjectMeta: metadata}
	role := &rbacv1.Role{ObjectMeta: metadata, Rules: rules}
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metadata,
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: common.KindRole, Name: metadata.Name},
		Subjects: []rbacv1.Subject{{
			Kind:      common.KindServiceAccount,
			Name:      metadata.Name,
			Namespace: p.Namespace,
		}},
	}
	for _, object := range []client.Object{account, role, binding} {
		if err := r.createOwned(ctx, p, object); err != nil {
			return err
		}
	}
	job := &batchv1.Job{
		ObjectMeta: metadata,
		Spec: batchv1.JobSpec{
			BackoffLimit:          ptr.To[int32](3),
			ActiveDeadlineSeconds: ptr.To[int64](120),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: Labels(p, api.RoleSupport)},
				Spec: corev1.PodSpec{
					ServiceAccountName: metadata.Name,
					RestartPolicy:      corev1.RestartPolicyNever,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To[int64](65532),
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Containers: []corev1.Container{{
						Name: "resolver", Image: r.ResolverImage,
						Command: []string{"/foundation"},
						Args:    []string{"--mode=" + mode, "--input=" + string(data)},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true),
							Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
					}},
				},
			},
		},
	}
	if inputConfigMap != "" {
		pod := &job.Spec.Template.Spec
		pod.Containers[0].Args = []string{"--mode=" + mode, "--input-file=/input/input.json"}
		pod.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "input", MountPath: "/input", ReadOnly: true}}
		pod.Volumes = []corev1.Volume{
			{
				Name: "input",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: inputConfigMap},
						Items:                []corev1.KeyToPath{{Key: "input.json", Path: "input.json"}},
					},
				},
			},
		}
	}
	applyPlacement(&job.Spec.Template.Spec, placement, Labels(p, api.RoleSupport))
	if err := r.createOwned(ctx, p, job); err != nil {
		return err
	}
	var current batchv1.Job
	if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(job), &current); err != nil {
		return err
	}
	if len(current.Spec.Template.Spec.Containers) != 1 ||
		digest(current.Spec.Template.Spec.Containers[0].Args) != digest(job.Spec.Template.Spec.Containers[0].Args) {
		return fmt.Errorf("resolver Job input changed")
	}
	for _, condition := range current.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return fmt.Errorf("native configuration resolver failed")
		}
	}
	return nil
}

// createOwned creates outputs without adopting an existing foreign identity.
func (r *Reconciler) createOwned(ctx context.Context, p *api.StacksNetworkParticipant, object client.Object) error {
	err := r.Client.Create(ctx, object)
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	current := object.DeepCopyObject().(client.Object)
	if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(object), current); err != nil {
		return err
	}
	if !owned(current, p) || current.GetDeletionTimestamp() != nil {
		return fmt.Errorf("resource %s is foreign or deleting", current.GetName())
	}
	return nil
}
