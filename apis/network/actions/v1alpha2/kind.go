package v1alpha2

// Kind identifies a supported finite Bitcoin action, not an arbitrary Kubernetes kind.
type Kind string

const (
	// KindBitcoinBlockGeneration identifies a finite block-generation request.
	KindBitcoinBlockGeneration Kind = "BitcoinBlockGeneration"
	// KindBitcoinReorganization identifies a finite chain-reorganization request.
	KindBitcoinReorganization Kind = "BitcoinReorganization"
)
