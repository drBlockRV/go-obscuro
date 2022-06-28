package obscuroscan

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/obscuronet/obscuro-playground/go/enclave/crypto"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/obscuronet/obscuro-playground/go/enclave/core"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/obscuronet/obscuro-playground/go/ethadapter/mgmtcontractlib"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/obscuronet/obscuro-playground/go/common"

	"github.com/obscuronet/obscuro-playground/go/rpcclientlib"
)

const (
	pathHeadBlock     = "/headblock/"
	pathHeadRollup    = "/headrollup/"
	pathDecryptTxBlob = "/decrypttxblob/"
	staticDir         = "./tools/obscuroscan/static"
	pathRoot          = "/"
	httpCodeErr       = 500
)

// Obscuroscan is a server that allows the monitoring of a running Obscuro network.
type Obscuroscan struct {
	server      *http.Server
	client      rpcclientlib.Client
	contractABI abi.ABI
}

func NewObscuroscan(address string) *Obscuroscan {
	client := rpcclientlib.NewClient(address)
	contractABI, err := abi.JSON(strings.NewReader(mgmtcontractlib.MgmtContractABI))
	if err != nil {
		panic("could not parse management contract ABI to decrypt rollups")
	}
	return &Obscuroscan{
		client:      client,
		contractABI: contractABI,
	}
}

// Serve listens for and serves Obscuroscan requests.
func (o *Obscuroscan) Serve(hostAndPort string) {
	serveMux := http.NewServeMux()
	// Serves the web interface.
	serveMux.Handle(pathRoot, http.FileServer(http.Dir(staticDir)))
	// Handle requests for block head height.
	serveMux.HandleFunc(pathHeadBlock, o.getBlockHead)
	// Handle requests for the head rollup.
	serveMux.HandleFunc(pathHeadRollup, o.getHeadRollup)
	// Handle requests to decrypt a transaction blob.
	serveMux.HandleFunc(pathDecryptTxBlob, o.decryptTxBlob)
	o.server = &http.Server{Addr: hostAndPort, Handler: serveMux}

	err := o.server.ListenAndServe()
	if err != http.ErrServerClosed {
		panic(err)
	}
}

func (o *Obscuroscan) Shutdown() {
	if o.server != nil {
		err := o.server.Shutdown(context.Background())
		if err != nil {
			fmt.Printf("could not shut down Obscuroscan. Cause: %s", err)
		}
	}
}

// Retrieves the current block header for the Obscuro network.
func (o *Obscuroscan) getBlockHead(resp http.ResponseWriter, _ *http.Request) {
	var headBlock *types.Header
	err := o.client.Call(&headBlock, rpcclientlib.RPCGetCurrentBlockHead)
	if err != nil {
		logAndSendErr(resp, fmt.Sprintf("could not retrieve head block. Cause: %s", err))
		return
	}

	jsonBlock, err := json.Marshal(headBlock)
	if err != nil {
		logAndSendErr(resp, fmt.Sprintf("could not return head block to client. Cause: %s", err))
		return
	}
	_, err = resp.Write(jsonBlock)
	if err != nil {
		logAndSendErr(resp, fmt.Sprintf("could not return head block to client. Cause: %s", err))
		return
	}
}

// Retrieves the head rollup for the Obscuro network.
func (o *Obscuroscan) getHeadRollup(resp http.ResponseWriter, _ *http.Request) {
	// TODO - Update logic here once rollups are encrypted.
	// TODO - If required, consolidate the two calls below into a single RPCGetHeadRollup call to minimise round trips.
	var headRollupHeader *common.Header
	err := o.client.Call(&headRollupHeader, rpcclientlib.RPCGetCurrentRollupHead)
	if err != nil {
		logAndSendErr(resp, fmt.Sprintf("could not retrieve head rollup header. Cause: %s", err))
		return
	}

	var headRollup *common.ExtRollup
	err = o.client.Call(&headRollup, rpcclientlib.RPCGetRollup, headRollupHeader.Hash())
	if err != nil {
		logAndSendErr(resp, fmt.Sprintf("could not retrieve head rollup. Cause: %s", err))
		return
	}

	jsonRollup, err := json.Marshal(headRollup)
	if err != nil {
		logAndSendErr(resp, fmt.Sprintf("could not return head rollup to client. Cause: %s", err))
		return
	}
	_, err = resp.Write(jsonRollup)
	if err != nil {
		logAndSendErr(resp, fmt.Sprintf("could not return head rollup to client. Cause: %s", err))
		return
	}
}

// Decrypts the provided transaction blob using the provided key.
// TODO - Use the passed-in key, rather than a hardcoded enclave key.
func (o *Obscuroscan) decryptTxBlob(resp http.ResponseWriter, req *http.Request) {
	body := req.Body
	defer body.Close()
	buffer := new(bytes.Buffer)
	_, err := buffer.ReadFrom(body)
	if err != nil {
		logAndSendErr(resp, fmt.Sprintf("could not read request body: %s", err))
		return
	}

	jsonTxs, err := decryptTxBlob(buffer.Bytes())
	if err != nil {
		logAndSendErr(resp, fmt.Sprintf("could not decrypt transaction blob. Cause: %s", err))
		return
	}

	_, err = resp.Write(jsonTxs)
	if err != nil {
		logAndSendErr(resp, fmt.Sprintf("could not write decrypted transactions to client. Cause: %s", err))
		return
	}
}

// Decrypts the transaction blob and returns it as JSON.
func decryptTxBlob(encryptedTxBytesBase64 []byte) ([]byte, error) {
	encryptedTxBytes, err := base64.StdEncoding.DecodeString(string(encryptedTxBytesBase64))
	if err != nil {
		return nil, fmt.Errorf("could not decode encrypted transaction blob from Base64. Cause: %w", err)
	}

	key := common.Hex2Bytes(crypto.RollupEncryptionKeyHex)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("could not initialise AES cipher for enclave rollup key. Cause: %w", err)
	}
	transactionCipher, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("could not initialise wrapper for AES cipher for enclave rollup key. Cause: %w", err)
	}

	encodedTxs, err := transactionCipher.Open(nil, []byte(crypto.RollupCipherNonce), encryptedTxBytes, nil)
	if err != nil {
		return nil, fmt.Errorf("could not decrypt encrypted L2 transactions. Cause: %w", err)
	}

	cleartextTxs := core.L2Txs{}
	if err = rlp.DecodeBytes(encodedTxs, &cleartextTxs); err != nil {
		return nil, fmt.Errorf("could not decode encoded L2 transactions. Cause: %w", err)
	}

	jsonRollup, err := json.Marshal(cleartextTxs)
	if err != nil {
		return nil, fmt.Errorf("could not decrypt transaction blob: %w", err)
	}

	return jsonRollup, nil
}

// Logs the error message and sends it as an HTTP error.
func logAndSendErr(resp http.ResponseWriter, msg string) {
	fmt.Println(msg)
	http.Error(resp, msg, httpCodeErr)
}
