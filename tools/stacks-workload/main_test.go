package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
)

func TestLoadCallBoundsAndDeterminism(t *testing.T) {
	request := submitRequest{
		Contract: "ST000000000000000000002AMW42H.load-driver", Function: "run16",
		Writes: 16, Reads: 128, PayloadBytes: 1024, KeyBase: 1000,
	}
	options := transactionOptions(t)
	first, err := loadCall(options, request, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadCall(options, request, 0)
	if err != nil || first.TxID != second.TxID || !bytes.Equal(first.Bytes, second.Bytes) {
		t.Fatal("load transaction is not deterministic")
	}
}

func TestSubmitRequestModesAreExclusiveAndBounded(t *testing.T) {
	base := submitRequest{FeeMicroSTX: 1000, TimeoutSeconds: 60, Contract: "load-driver"}
	deployment := base
	deployment.ContractSource = "(define-public (ok) (ok true))"
	if err := validateSubmitRequest(deployment); err != nil {
		t.Fatal(err)
	}
	call := base
	call.Contract = "ST000000000000000000002AMW42H.load-driver"
	call.Function, call.Count = "run16", 1
	if err := validateSubmitRequest(call); err != nil {
		t.Fatal(err)
	}
	deployment.Count = 1
	if err := validateSubmitRequest(deployment); err == nil {
		t.Fatal("accepted deployment with call fields")
	}
	call.ClarityVersion = 4
	if err := validateSubmitRequest(call); err == nil {
		t.Fatal("accepted call with deployment fields")
	}
}

func transactionOptions(t *testing.T) transaction.Options {
	t.Helper()
	return transaction.Options{
		Version: transaction.Testnet, ChainID: 0x80000000, Fee: 1000,
		PostConditionMode: transaction.Allow,
		PrivateKey:        strings.Repeat("0", 63) + "101",
	}
}
