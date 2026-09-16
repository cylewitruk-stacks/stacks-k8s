package main

import (
	"crypto/sha256"
	"fmt"
)

// publicInputs explicitly selects non-secret base inputs for the Started event.
type publicInputs struct {
	Sender         string            `json:"sender"`
	Contract       string            `json:"contract"`
	FeeMicroSTX    uint64            `json:"feeMicroSTX"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
	Call           *callShape        `json:"call,omitempty"`
	Deployment     *deploymentInputs `json:"deployment,omitempty"`
}

// deploymentInputs identifies externally retained source without embedding its contents.
type deploymentInputs struct {
	ClarityVersion byte   `json:"clarityVersion"`
	SourceDigest   string `json:"sourceDigest"`
}

// publicInputEvidence excludes credentials, endpoint details and contract source by construction.
func publicInputEvidence(r submitRequest) *publicInputs {
	inputs := &publicInputs{
		Sender: r.Sender, Contract: r.Contract, FeeMicroSTX: r.FeeMicroSTX, TimeoutSeconds: r.TimeoutSeconds,
	}
	if r.ContractSource != "" {
		inputs.Deployment = &deploymentInputs{
			ClarityVersion: r.deploymentVersion(),
			SourceDigest:   fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(r.ContractSource))),
		}
	} else {
		inputs.Call = &callShape{r.Function, r.Writes, r.Reads, r.PayloadBytes, r.KeyBase}
	}
	return inputs
}
