package environment

import (
	"encoding/json"
	"strings"
	"testing"

	core "k8s.io/api/core/v1"
)

func TestBitcoinCompositionBindsFinalConfigurationOnce(t *testing.T) {
	options := BitcoinOptions{Namespace: "test", Name: "test", Image: DefaultBitcoinImage, Address: "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn", Interval: 5, Weights: []int32{1}, RPC: []RPCIdentity{{Username: "actor", Password: "private", Methods: "getblockcount"}}}
	result, err := Bitcoin(options)
	if err != nil {
		t.Fatal(err)
	}
	var config string
	var credentials map[string]string
	for _, resource := range result.Resources {
		switch item := resource.(type) {
		case *core.ConfigMap:
			config = item.Data["bitcoin.conf"]
		case *core.Secret:
			if item.Name == "stacks-bitcoin-production-rpc" {
				if err = json.Unmarshal(item.Data["credentials.json"], &credentials); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	digest := Digest(config)
	if credentials["configDigest"] != digest || result.Network.Spec.BitcoinNodes[0].Config.ConfigMapRef.ExpectedDigest != digest {
		t.Fatal("configuration and consumers have different digests")
	}
	if !strings.Contains(config, "rpcwhitelist=actor:getblockcount\n") || strings.Contains(config, "private") {
		t.Fatal("external RPC identity was not separated")
	}
	options.RPC[0].Username = "producer"
	if _, err = Bitcoin(options); err == nil {
		t.Fatal("external identity replaced the production credential")
	}
}
