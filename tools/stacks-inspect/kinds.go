package main

// recordKind distinguishes snapshot records in the JSONL stream.
type recordKind string

const (
	recordCapture             recordKind = "capture"
	recordStacksTip           recordKind = "stacks-tip"
	recordPoX                 recordKind = "pox"
	recordRewardSet           recordKind = "reward-set"
	recordBitcoinTip          recordKind = "bitcoin-tip"
	recordBitcoinHeader       recordKind = "bitcoin-header"
	recordBitcoinTransactions recordKind = "bitcoin-transactions"
	recordAncestry            recordKind = "ancestry"
)

// ancestryRelation states only what the bounded hash walks establish.
type ancestryRelation string

const (
	ancestryUnknown            ancestryRelation = "unknown-within-bound"
	ancestryComparisonAncestor ancestryRelation = "comparison-is-ancestor"
	ancestryPrimaryAncestor    ancestryRelation = "primary-is-ancestor"
	ancestryShared             ancestryRelation = "shared-ancestor"
	ancestrySame               ancestryRelation = "same-tip"
)
