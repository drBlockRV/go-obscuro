package config

import (
	"github.com/ethereum/go-ethereum/common"
)

// EnclaveConfig contains the full configuration for an Obscuro enclave service.
type EnclaveConfig struct {
	// The identity of the host the enclave service is tied to
	HostID common.Address
	// The address on which to serve requests
	Address string
	// The ID of the L1 chain
	ChainID int64
	// Whether to produce a verified attestation report
	WillAttest bool
	// Whether to validate incoming L1 blocks
	ValidateL1Blocks bool
	// When validating incoming blocks, the genesis config for the L1 chain
	GenesisJSON []byte
	// Toggles the speculative execution background process
	SpeculativeExecution bool
	// The management contract address on the L1 network
	ManagementContractAddress common.Address
	// The addresses of ERC20 contracts to monitor on the L1 network
	ERC20ContractAddresses []*common.Address
	// Whether to redirect the enclave's output to the log file.
	WriteToLogs bool
	// The path that the node's logs are written to
	LogPath string
}

// DefaultEnclaveConfig returns an EnclaveConfig with default values.
func DefaultEnclaveConfig() EnclaveConfig {
	return EnclaveConfig{
		HostID:                    common.BytesToAddress([]byte("")),
		Address:                   "127.0.0.1:11000",
		ChainID:                   777,
		WillAttest:                false,
		ValidateL1Blocks:          false,
		GenesisJSON:               nil,
		SpeculativeExecution:      false,
		ManagementContractAddress: common.BytesToAddress([]byte("")),
		ERC20ContractAddresses:    []*common.Address{},
		WriteToLogs:               false,
		LogPath:                   "enclave_logs.txt",
	}
}