package simulation

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/obscuronet/go-obscuro/integration/erc20contract"

	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/obscuronet/go-obscuro/go/enclave/bridge"
	"github.com/obscuronet/go-obscuro/go/rpcclientlib"
	"github.com/obscuronet/go-obscuro/integration/simulation/network"

	"github.com/obscuronet/go-obscuro/go/common/log"

	"github.com/obscuronet/go-obscuro/go/common"

	"github.com/obscuronet/go-obscuro/go/ethadapter"

	testcommon "github.com/obscuronet/go-obscuro/integration/common"
	"github.com/obscuronet/go-obscuro/integration/simulation/params"

	"github.com/obscuronet/go-obscuro/integration/simulation/stats"
)

const (
	initialBalance  = 5000
	allocObsWallets = 750000000000000 // The amount the faucet allocates to each Obscuro wallet.
)

// Simulation represents all the data required to inject transactions on a network
type Simulation struct {
	RPCHandles       *network.RPCHandles
	AvgBlockDuration uint64
	TxInjector       *TransactionInjector
	SimulationTime   time.Duration
	Stats            *stats.Stats
	Params           *params.SimParams
}

// Start executes the simulation given all the Params. Injects transactions.
func (s *Simulation) Start() {
	log.Info(fmt.Sprintf("Genesis block: b_%d.", common.ShortHash(common.GenesisBlock.Hash())))

	s.waitForObscuroGenesisOnL1()

	// Arbitrary sleep to wait for RPC clients to get up and running
	time.Sleep(1 * time.Second)

	s.prefundObscuroAccounts() // Prefund every L2 wallet
	s.deployObscuroERC20s()    // Deploy the Obscuro OBX and ETH ERC20 contracts
	s.prefundL1Accounts()      // Prefund every L1 wallet

	timer := time.Now()
	log.Info("Starting injection")
	go s.TxInjector.Start()

	stoppingDelay := s.Params.AvgBlockDuration * 7

	// Wait for the simulation time
	time.Sleep(s.SimulationTime - stoppingDelay)
	log.Info("Stopping injection")

	s.TxInjector.Stop()

	// Allow for some time after tx injection was stopped so that the network can process all transactions
	time.Sleep(stoppingDelay)

	fmt.Printf("Ran simulation for %f secs, configured to run for: %s ... \n", time.Since(timer).Seconds(), s.SimulationTime)
}

func (s *Simulation) Stop() {
	// nothing to do for now
}

func (s *Simulation) waitForObscuroGenesisOnL1() {
	// grab an L1 client
	client := s.RPCHandles.EthClients[0]

	for {
		// spin through the L1 blocks periodically to see if the genesis rollup has arrived
		head := client.FetchHeadBlock()
		for _, b := range client.BlocksBetween(common.GenesisBlock, head) {
			for _, tx := range b.Transactions() {
				t := s.Params.MgmtContractLib.DecodeTx(tx)
				if t == nil {
					continue
				}
				if _, ok := t.(*ethadapter.L1RollupTx); ok {
					// exit at the first obscuro rollup we see
					return
				}
			}
		}
		time.Sleep(s.Params.AvgBlockDuration)
		log.Trace("Waiting for the Obscuro genesis rollup...")
	}
}

// Prefunds the L2 wallets with `allocObsWallets` each.
func (s *Simulation) prefundObscuroAccounts() {
	faucetWallet := s.Params.Wallets.L2FaucetWallet
	faucetClient := s.RPCHandles.ObscuroWalletRndClient(faucetWallet)
	nonce := NextNonce(s.RPCHandles, faucetWallet)
	testcommon.PrefundWallets(faucetWallet, faucetClient, nonce, s.Params.Wallets.AllObsWallets(), big.NewInt(allocObsWallets))
}

// This deploys an ERC20 contract on Obscuro, which is used for token arithmetic.
func (s *Simulation) deployObscuroERC20s() {
	tokens := []bridge.ERC20{bridge.OBX, bridge.ETH}

	wg := sync.WaitGroup{}
	for _, token := range tokens {
		wg.Add(1)
		go func(token bridge.ERC20) {
			defer wg.Done()
			owner := s.Params.Wallets.Tokens[token].L2Owner
			contractBytes := erc20contract.L2BytecodeWithDefaultSupply(string(token))

			deployContractTx := types.DynamicFeeTx{
				Nonce:     NextNonce(s.RPCHandles, owner),
				Gas:       1025_000_000,
				GasFeeCap: gethcommon.Big1, // This field is used to derive the gas price for dynamic fee transactions.
				Data:      contractBytes,
			}
			signedTx, err := owner.SignTransaction(&deployContractTx)
			if err != nil {
				panic(err)
			}

			err = s.RPCHandles.ObscuroWalletRndClient(owner).Call(nil, rpcclientlib.RPCSendRawTransaction, testcommon.EncodeTx(signedTx))
			if err != nil {
				panic(err)
			}

			err = testcommon.AwaitReceipt(s.RPCHandles.ObscuroWalletRndClient(owner), signedTx.Hash())
			if err != nil {
				panic(fmt.Sprintf("ERC20 deployment transaction failed. Cause: %s", err))
			}
		}(token)
	}
	wg.Wait()
}

// Sends an amount from the faucet to each L1 account, to pay for transactions.
func (s *Simulation) prefundL1Accounts() {
	for _, w := range s.Params.Wallets.SimEthWallets {
		addr := w.Address()
		txData := &ethadapter.L1DepositTx{
			Amount:        initialBalance,
			To:            s.Params.MgmtContractAddr,
			TokenContract: s.Params.Wallets.Tokens[bridge.OBX].L1ContractAddress,
			Sender:        &addr,
		}
		tx := s.Params.ERC20ContractLib.CreateDepositTx(txData, w.GetNonceAndIncrement())
		signedTx, err := w.SignTransaction(tx)
		if err != nil {
			panic(err)
		}
		err = s.RPCHandles.RndEthClient().SendTransaction(signedTx)
		if err != nil {
			panic(err)
		}

		s.Stats.Deposit(initialBalance)
		go s.TxInjector.TxTracker.trackL1Tx(txData)
	}
}
