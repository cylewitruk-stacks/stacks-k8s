package v1alpha2

const (
	// CompatibilityManaged requires the supported configuration contract.
	CompatibilityManaged = "Managed"
	// CompatibilityUnverified permits custom configuration without managed protocol claims.
	CompatibilityUnverified = "Unverified"
	// DiscoveryNetwork selects peers from admitted network participants.
	DiscoveryNetwork = "Network"
)

const (
	// EndpointRPC selects an actor's native RPC service and named port.
	EndpointRPC = "rpc"
	// EndpointP2P selects an actor's peer protocol service and named port.
	EndpointP2P = "p2p"
	// EndpointEvents selects a consensus signer's event service and named port.
	EndpointEvents = "events"
	// BitcoinActorRPCUsername is the method-restricted credential used by Stacks actors.
	BitcoinActorRPCUsername = "actor"
)
