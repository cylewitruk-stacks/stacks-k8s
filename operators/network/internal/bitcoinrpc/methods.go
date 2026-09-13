package bitcoinrpc

const (
	// MethodGetBlockchainInfo identifies the getblockchaininfo RPC method.
	MethodGetBlockchainInfo = "getblockchaininfo"
	// MethodGetBlockHash identifies the getblockhash RPC method.
	MethodGetBlockHash = "getblockhash"
	// MethodGetBlockHeader identifies the getblockheader RPC method.
	MethodGetBlockHeader = "getblockheader"
	// MethodGetChainTips identifies the getchaintips RPC method.
	MethodGetChainTips = "getchaintips"
	// MethodGetDescriptorInfo identifies the getdescriptorinfo RPC method.
	MethodGetDescriptorInfo = "getdescriptorinfo"
	// MethodGetTxOut identifies the gettxout RPC method.
	MethodGetTxOut = "gettxout"
	// MethodGetWalletInfo identifies the getwalletinfo RPC method.
	MethodGetWalletInfo = "getwalletinfo"
	// MethodListDescriptors identifies the listdescriptors RPC method.
	MethodListDescriptors = "listdescriptors"
	// MethodListUnspent identifies the listunspent RPC method.
	MethodListUnspent = "listunspent"
	// MethodListWalletDir identifies the listwalletdir RPC method.
	MethodListWalletDir = "listwalletdir"
	// MethodListWallets identifies the listwallets RPC method.
	MethodListWallets = "listwallets"
	// MethodCreateWallet identifies the createwallet RPC method.
	MethodCreateWallet = "createwallet"
	// MethodLoadWallet identifies the loadwallet RPC method.
	MethodLoadWallet = "loadwallet"
	// MethodUnloadWallet identifies the unloadwallet RPC method.
	MethodUnloadWallet = "unloadwallet"
	// MethodImportDescriptors identifies the importdescriptors RPC method.
	MethodImportDescriptors = "importdescriptors"
	// MethodValidateAddress identifies the validateaddress RPC method.
	MethodValidateAddress = "validateaddress"
	// MethodGenerateToAddress identifies the generatetoaddress RPC method.
	MethodGenerateToAddress = "generatetoaddress"
	// MethodInvalidateBlock identifies the invalidateblock RPC method.
	MethodInvalidateBlock = "invalidateblock"
	// MethodReconsiderBlock identifies the reconsiderblock RPC method.
	MethodReconsiderBlock = "reconsiderblock"
)

const (
	// ChainRegtest is the native chain identity required by production and preflight.
	ChainRegtest = "regtest"
	// ChainTipActive identifies Core's selected best tip.
	ChainTipActive = "active"
	// ChainTipInvalid identifies a tip excluded by Core validation or invalidation.
	ChainTipInvalid = "invalid"
)
