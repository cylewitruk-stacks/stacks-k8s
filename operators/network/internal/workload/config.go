package workload

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/canonical"
)

// SpecDigest binds an admitted actor identity to its complete leaf declaration.
func SpecDigest(value any) (string, error) {
	return canonical.Digest(value)
}

type mountedConfig struct {
	key            string
	mountPath      string
	volume         corev1.Volume
	configMapData  map[string]string
	digest         string
	expectedDigest string
}

func prepareConfig(descriptor Descriptor) (mountedConfig, error) {
	source := descriptor.Config
	key := defaultConfigKey(descriptor.Kind)
	mountPath := "/etc/stacks"
	result := mountedConfig{}
	switch {
	case source.Inline != nil:
		if source.Inline.Key != "" {
			key = source.Inline.Key
		}
		if source.Inline.MountPath != "" {
			mountPath = source.Inline.MountPath
		}
		sum := sha256.Sum256([]byte(source.Inline.Data))
		result.configMapData = map[string]string{key: source.Inline.Data}
		result.digest = "sha256:" + hex.EncodeToString(sum[:])
		result.expectedDigest = result.digest
		result.volume = corev1.Volume{Name: "actor-config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: configName(descriptor.Owner.GetName())}}}}
	case source.ConfigMapRef != nil:
		if source.ConfigMapRef.Key != "" {
			key = source.ConfigMapRef.Key
		}
		if source.ConfigMapRef.MountPath != "" {
			mountPath = source.ConfigMapRef.MountPath
		}
		result.digest, result.expectedDigest = source.ConfigMapRef.ExpectedDigest, source.ConfigMapRef.ExpectedDigest
		result.volume = corev1.Volume{Name: "actor-config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: source.ConfigMapRef.Name}}}}
	case source.SecretRef != nil:
		if source.SecretRef.Key != "" {
			key = source.SecretRef.Key
		}
		if source.SecretRef.MountPath != "" {
			mountPath = source.SecretRef.MountPath
		}
		result.digest, result.expectedDigest = source.SecretRef.ExpectedDigest, source.SecretRef.ExpectedDigest
		result.volume = corev1.Volume{Name: "actor-config", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: source.SecretRef.Name}}}
	default:
		return mountedConfig{}, fmt.Errorf("%s %s has no renderable configuration source", descriptor.Kind, descriptor.Actor)
	}
	result.key, result.mountPath = key, strings.TrimRight(mountPath, "/")
	if result.digest == "" {
		payload := fmt.Sprintf("%T:%s:%s:%s", source, referenceName(source), key, result.mountPath)
		sum := sha256.Sum256([]byte(payload))
		result.digest = "declaration-sha256:" + hex.EncodeToString(sum[:])
	}
	return result, nil
}

func referenceName(source networkv1alpha1.ConfigSource) string {
	if source.ConfigMapRef != nil {
		return "configmap:" + source.ConfigMapRef.Name
	}
	if source.SecretRef != nil {
		return "secret:" + source.SecretRef.Name
	}
	return "inline"
}

func defaultConfigKey(kind string) string {
	switch kind {
	case "BitcoinNode":
		return "bitcoin.conf"
	case "StacksSigner":
		return "signer.toml"
	default:
		return "config.toml"
	}
}

// ConfigPath returns the mounted path of the selected configuration file.
func ConfigPath(source networkv1alpha1.ConfigSource, kind string) string {
	key, mountPath := defaultConfigKey(kind), "/etc/stacks"
	switch {
	case source.Inline != nil:
		if source.Inline.Key != "" {
			key = source.Inline.Key
		}
		if source.Inline.MountPath != "" {
			mountPath = source.Inline.MountPath
		}
	case source.ConfigMapRef != nil:
		if source.ConfigMapRef.Key != "" {
			key = source.ConfigMapRef.Key
		}
		if source.ConfigMapRef.MountPath != "" {
			mountPath = source.ConfigMapRef.MountPath
		}
	case source.SecretRef != nil:
		if source.SecretRef.Key != "" {
			key = source.SecretRef.Key
		}
		if source.SecretRef.MountPath != "" {
			mountPath = source.SecretRef.MountPath
		}
	}
	return strings.TrimRight(mountPath, "/") + "/" + key
}

// RenderedConfigPath returns the post-substitution path used by Stacks processes.
func RenderedConfigPath(source networkv1alpha1.ConfigSource, kind string) string {
	path := ConfigPath(source, kind)
	return "/tmp/stacks-network-config/" + path[strings.LastIndex(path, "/")+1:]
}

func configName(owner string) string { return resourceName(owner, "config") }

func serviceMapEnvironment(values map[string]string) string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := make([]string, len(names))
	for index, name := range names {
		lines[index] = name + "=" + values[name]
	}
	return strings.Join(lines, "\n")
}

const renderStacksConfigScript = `set -euo pipefail
template="${STACKS_CONFIG_TEMPLATE}"
rendered="${STACKS_CONFIG_RENDERED}"
node_ip="${POD_IP}"
temporary="${rendered}.tmp.$$"
trap 'rm -f "${temporary}"' EXIT
while IFS= read -r line || [ -n "${line}" ]; do
  line="${line//__NODE_IP__/${node_ip}}"
  while IFS='=' read -r logical service; do
    [ -n "${logical}" ] || continue
    token="\${SERVICE:${logical}}"
    line="${line//$token/$service}"
  done <<<"${STACKS_SERVICE_MAP}"
  printf '%s\n' "${line}"
done <"${template}" >"${temporary}"
if grep -qF '${SERVICE:' "${temporary}"; then
  echo "configuration contains an unknown logical service reference" >&2
  exit 1
fi
mv "${temporary}" "${rendered}"
trap - EXIT
exec "$@"`
