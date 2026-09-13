package foundation

import (
	"errors"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
)

// ErrUnsupportedWalletProfile identifies private Core wallet requests outside the supported profile.
var ErrUnsupportedWalletProfile = errors.New(
	"only watch-only Core wallets are supported; omit watchOnly or set it to true",
)

// ValidateWalletProfile rejects unsupported stored inputs independently of schema enforcement.
func ValidateWalletProfile(spec bitcoin.BitcoinWalletSpec) error {
	if spec.WatchOnly != nil && !*spec.WatchOnly {
		return ErrUnsupportedWalletProfile
	}
	return nil
}
