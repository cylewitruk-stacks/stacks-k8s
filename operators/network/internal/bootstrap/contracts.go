package bootstrap

import "github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"

// contractSource binds a deployment to reviewed source bytes.
type contractSource = protocolcontracts.Source

// loadContracts verifies the entire pinned bundle before external bootstrap.
func loadContracts(directory string) ([]contractSource, error) {
	return protocolcontracts.Load(directory)
}

// directManager renders the shared non-custodial direct holder manager.
func directManager(holder string) (string, error) { return protocolcontracts.DirectManager(holder) }

// contractPins retains the external helper's metadata verification seam.
var contractPins = protocolcontracts.Pins()
