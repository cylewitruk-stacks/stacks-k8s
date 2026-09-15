module github.com/cylewitruk-stacks/stacks-k8s/tools/stacks-experiment

go 1.27.0

require github.com/cylewitruk-stacks/stacks-k8s/libs/stacks v0.1.0

require (
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.1 // indirect
	golang.org/x/crypto v0.57.0 // indirect
)

replace github.com/cylewitruk-stacks/stacks-k8s/libs/stacks => ../../libs/stacks
