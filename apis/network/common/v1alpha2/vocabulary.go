package v1alpha2

const (
	// CompatibilityManaged requires the supported configuration contract.
	CompatibilityManaged = "Managed"
	// CompatibilityUnverified permits custom configuration without managed protocol claims.
	CompatibilityUnverified = "Unverified"
	// DiscoveryNetwork selects peers from admitted network participants.
	DiscoveryNetwork = "Network"
)
