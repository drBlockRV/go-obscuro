package launcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/obscuronet/go-obscuro/go/common/retry"
	"github.com/obscuronet/go-obscuro/go/node"
	"github.com/obscuronet/go-obscuro/testnet/launcher/eth2network"

	l1cd "github.com/obscuronet/go-obscuro/testnet/launcher/l1contractdeployer"
	l2cd "github.com/obscuronet/go-obscuro/testnet/launcher/l2contractdeployer"
)

type Testnet struct {
	cfg *Config
}

func NewTestnetLauncher(cfg *Config) *Testnet {
	// todo bind testnet specific options like number of nodes, etc
	return &Testnet{cfg: cfg}
}

func (t *Testnet) Start() error {
	fmt.Printf("Starting Testnet with config: %+v\n", t.cfg)

	err := startEth2Network()
	if err != nil {
		return fmt.Errorf("unable to start eth2network - %w", err)
	}

	managementContractAddr, messageBusContractAddr, err := deployL1Contracts()
	if err != nil {
		return fmt.Errorf("unable to deploy l1 contracts - %w", err)
	}

	sequencerNodeConfig := node.NewNodeConfig(
		node.WithNodeName("sequencer"),
		node.WithNodeType("sequencer"),
		node.WithGenesis(true),
		node.WithSGXEnabled(false),
		node.WithEnclaveImage(t.cfg.sequencerEnclaveDockerImage),
		node.WithEnclaveDebug(t.cfg.sequencerEnclaveDebug),
		node.WithHostImage("testnetobscuronet.azurecr.io/obscuronet/host:latest"),
		node.WithL1Host("eth2network"),
		node.WithL1WSPort(9000),
		node.WithEnclaveWSPort(11000),
		node.WithHostHTTPPort(13000),
		node.WithHostWSPort(13001),
		node.WithHostP2PPort(15000),
		node.WithHostPublicP2PAddr("sequencer-host:15000"),
		node.WithPrivateKey("8ead642ca80dadb0f346a66cd6aa13e08a8ac7b5c6f7578d4bac96f5db01ac99"),
		node.WithHostID("0x0654D8B60033144D567f25bF41baC1FB0D60F23B"),
		node.WithSequencerID("0x0654D8B60033144D567f25bF41baC1FB0D60F23B"),
		node.WithManagementContractAddress(managementContractAddr),
		node.WithMessageBusContractAddress(messageBusContractAddr),
	)

	sequencerNode, err := node.NewDockerNode(sequencerNodeConfig)
	if err != nil {
		return fmt.Errorf("unable to configure the obscuro node - %w", err)
	}

	err = sequencerNode.Start()
	if err != nil {
		return fmt.Errorf("unable to start the obscuro node - %w", err)
	}
	fmt.Println("Obscuro node was successfully started...")

	// wait until the node it healthy
	err = waitForHealthyNode(13000)
	if err != nil {
		return fmt.Errorf("sequencer obscuro node not healthy - %w", err)
	}

	validatorNodeConfig := node.NewNodeConfig(
		node.WithNodeName("validator"),
		node.WithNodeType("validator"),
		node.WithGenesis(false),
		node.WithSGXEnabled(false),
		node.WithEnclaveImage(t.cfg.validatorEnclaveDockerImage),
		node.WithEnclaveDebug(t.cfg.validatorEnclaveDebug),
		node.WithHostImage("testnetobscuronet.azurecr.io/obscuronet/host:latest"),
		node.WithL1Host("eth2network"),
		node.WithL1WSPort(9000),
		node.WithEnclaveWSPort(11010),
		node.WithHostHTTPPort(13010),
		node.WithHostWSPort(13011),
		node.WithHostP2PPort(15010),
		node.WithHostPublicP2PAddr("validator-host:15010"),
		node.WithPrivateKey("ebca545772d6438bbbe1a16afbed455733eccf96157b52384f1722ea65ccfa89"),
		node.WithHostID("0x2f7fCaA34b38871560DaAD6Db4596860744e1e8A"),
		node.WithSequencerID("0x0654D8B60033144D567f25bF41baC1FB0D60F23B"),
		node.WithManagementContractAddress(managementContractAddr),
		node.WithMessageBusContractAddress(messageBusContractAddr),
	)

	validatorNode, err := node.NewDockerNode(validatorNodeConfig)
	if err != nil {
		return fmt.Errorf("unable to configure the obscuro node - %w", err)
	}

	err = validatorNode.Start()
	if err != nil {
		return fmt.Errorf("unable to start the obscuro node - %w", err)
	}
	fmt.Println("Obscuro node was successfully started...")

	// wait until the node it healthy
	err = waitForHealthyNode(13010)
	if err != nil {
		return fmt.Errorf("validator obscuro node not healthy - %w", err)
	}

	l2ContractDeployer, err := l2cd.NewDockerContractDeployer(
		l2cd.NewContractDeployerConfig(
			l2cd.WithL1Host("eth2network"),
			l2cd.WithL1Port(8025),
			l2cd.WithL2Host("sequencer-host"),
			l2cd.WithL2WSPort(13001),
			l2cd.WithL1PrivateKey("f52e5418e349dccdda29b6ac8b0abe6576bb7713886aa85abea6181ba731f9bb"),
			l2cd.WithMessageBusContractAddress("0xFD03804faCA2538F4633B3EBdfEfc38adafa259B"),
			l2cd.WithL2PrivateKey("8dfb8083da6275ae3e4f41e3e8a8c19d028d32c9247e24530933782f2a05035b"),
			l2cd.WithHocPKString("6e384a07a01263518a09a5424c7b6bbfc3604ba7d93f47e3a455cbdd7f9f0682"),
			l2cd.WithPocPKString("4bfe14725e685901c062ccd4e220c61cf9c189897b6c78bd18d7f51291b2b8f8"),
			l2cd.WithDockerImage("testnetobscuronet.azurecr.io/obscuronet/hardhatdeployer:latest"),
		),
	)
	if err != nil {
		return fmt.Errorf("unable to configure the l2 contract deployer - %w", err)
	}

	err = l2ContractDeployer.Start()
	if err != nil {
		return fmt.Errorf("unable to start the l2 contract deployer - %w", err)
	}

	err = l2ContractDeployer.WaitForFinish()
	if err != nil {
		return fmt.Errorf("unexpected error waiting for l2 contract deployer to finish - %w", err)
	}
	fmt.Println("L2 Contracts were successfully deployed...")

	return nil
}

func startEth2Network() error {
	eth2Network, err := eth2network.NewDockerEth2Network(
		eth2network.NewEth2NetworkConfig(
			eth2network.WithGethHTTPStartPort(8025),
			eth2network.WithGethWSStartPort(9000),
			eth2network.WithGethPrefundedAddrs([]string{
				"0x13E23Ca74DE0206C56ebaE8D51b5622EFF1E9944", // contract deployment pk - f52e5418e349dccdda29b6ac8b0abe6576bb7713886aa85abea6181ba731f9bb
				"0x0654D8B60033144D567f25bF41baC1FB0D60F23B", // sequencer pk - 8ead642ca80dadb0f346a66cd6aa13e08a8ac7b5c6f7578d4bac96f5db01ac99
				"0x2f7fCaA34b38871560DaAD6Db4596860744e1e8A", // validator pk - ebca545772d6438bbbe1a16afbed455733eccf96157b52384f1722ea65ccfa89
				"0xE09a37ABc1A63441404007019E5BC7517bE2c43f", // bridge admin pk - 4bfe14725e685901c062ccd4e220c61cf9c189897b6c78bd18d7f51291b2b8f1
			}),
		),
	)
	if err != nil {
		return fmt.Errorf("unable to configure eth2network - %w", err)
	}

	err = eth2Network.Start()
	if err != nil {
		return fmt.Errorf("unable to start eth2network - %w", err)
	}
	fmt.Println("Eth2 network started...")

	err = eth2Network.IsReady()
	if err != nil {
		return fmt.Errorf("eth2network not ready in time - %w", err)
	}
	fmt.Println("Eth2 network is ready...")

	return nil
}

func deployL1Contracts() (string, string, error) {
	l1ContractDeployer, err := l1cd.NewDockerContractDeployer(
		l1cd.NewContractDeployerConfig(
			l1cd.WithL1Host("eth2network"),
			l1cd.WithL1Port(8025),
			l1cd.WithPrivateKey("f52e5418e349dccdda29b6ac8b0abe6576bb7713886aa85abea6181ba731f9bb"),
			l1cd.WithDockerImage("testnetobscuronet.azurecr.io/obscuronet/hardhatdeployer:latest"),
		),
	)
	if err != nil {
		return "", "", fmt.Errorf("unable to configure l1 contract deployer - %w", err)
	}

	err = l1ContractDeployer.Start()
	if err != nil {
		return "", "", fmt.Errorf("unable to start l1 contract deployer - %w", err)
	}

	managementContractAddr, messageBusContractAddr, err := l1ContractDeployer.RetrieveL1ContractAddresses()
	if err != nil {
		return "", "", fmt.Errorf("unable to fetch l1 contract addresses - %w", err)
	}
	fmt.Println("L1 Contracts were successfully deployed...")
	return managementContractAddr, messageBusContractAddr, nil
}

// waitForHealthyNode retries continuously for the node to respond to a healthcheck http request
func waitForHealthyNode(port int) error { // todo: hook the cfg
	requestURL := fmt.Sprintf("http://localhost:%d", port)
	reqBody := `{"method": "obscuro_health", "id": 1}`

	fmt.Println("Waiting for obscuro node to be healthy...")
	return retry.Do(
		func() error {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, requestURL, bytes.NewBufferString(reqBody))
			if err != nil {
				return fmt.Errorf("client: could not create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")

			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer res.Body.Close()

			resBody, err := io.ReadAll(res.Body)
			if err != nil {
				return err
			}

			response := map[string]interface{}{}
			err = json.Unmarshal(resBody, &response)
			if err != nil {
				return err
			}

			if r := response["result"]; r != nil { //nolint: nestif
				if h, ok := r.(map[string]interface{}); ok {
					if overallHealth := h["OverallHealth"]; overallHealth != nil {
						if health, ok := overallHealth.(bool); ok && health {
							fmt.Println("obscuro node is ready")
							return nil
						}
					}
				}
			}
			return fmt.Errorf("node OverallHealth is not good yet")
		}, retry.NewTimeoutStrategy(1*time.Second, 2*time.Minute),
	)
}
