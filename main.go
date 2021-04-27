package main

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/umbracle/go-web3"
	"github.com/umbracle/go-web3/abi"
	"github.com/umbracle/go-web3/jsonrpc"
	"github.com/umbracle/go-web3/testutil"
	"github.com/umbracle/go-web3/wallet"
)

func DecodeHex(str string) []byte {
	str = strings.TrimPrefix(str, "0x")
	buf, err := hex.DecodeString(str)
	if err != nil {
		panic(err)
	}
	return buf
}

func main() {

	// 0xdf7fd4830f4cc1440b469615e9996e9fde92608f
	var privKeyRaw = "0x4b2216c76f1b4c60c44d41986863e7337bc1a317d6a9366adfd8966fe2ac05f6"
	key, err := wallet.NewWalletFromPrivKey(DecodeHex(privKeyRaw))
	if err != nil {
		panic(err)
	}

	clt, err := jsonrpc.NewClient("http://127.0.0.1:8545")
	if err != nil {
		panic(err)
	}

	// check there is enough balance
	balance, err := clt.Eth().GetBalance(key.Address(), web3.Latest)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Balance: %s\n", balance.String())

	// get the chain id and the signer
	chainID, err := clt.Eth().ChainID()
	if err != nil {
		panic(err)
	}
	signer := wallet.NewEIP155Signer(chainID.Uint64())

	sendTransaction := func(txn *web3.Transaction) *web3.Receipt {
		// latest nonce
		txn.Nonce, err = clt.Eth().GetNonce(key.Address(), web3.Latest)
		if err != nil {
			panic(err)
		}

		txn.GasPrice = 1000
		txn.Gas = 10000000

		// estimate gas limit
		estimate := &web3.CallMsg{
			From: key.Address(),
			To:   txn.To,
			Data: txn.Input,
		}
		txn.Gas, err = clt.Eth().EstimateGas(estimate)
		if err != nil {
			panic(err)
		}

		txn, err = signer.SignTx(txn, key)
		if err != nil {
			panic(err)
		}
		hash, err := clt.Eth().SendRawTransaction(txn.MarshalRLP())
		if err != nil {
			panic(err)
		}
		receipt, err := waitForReceipt(clt, hash)
		if err != nil {
			panic(err)
		}
		return receipt
	}

	oneAddr := web3.Address{0x1}

	// create a contract
	cc := &testutil.Contract{}

	// add an event A in the contract
	cc.AddEvent(testutil.NewEvent("A").Add("address", true).Add("address", true))

	// add a method to test calls
	cc.AddDualCaller("setA", "address", "uint256")

	// create a function setA1 that emits the event A
	cc.EmitEvent("setA1", "A", oneAddr.String(), oneAddr.String())

	solcContract, err := cc.Compile()
	if err != nil {
		panic(err)
	}
	receipt := sendTransaction(&web3.Transaction{
		Input: DecodeHex(solcContract.Bin),
	})
	contractAddr := receipt.ContractAddress
	fmt.Printf("Contract deployed: %s\n", contractAddr)

	abiContract, err := abi.NewABI(solcContract.Abi)
	if err != nil {
		panic(err)
	}

	sendCall := func(method string, args ...interface{}) map[string]interface{} {
		m, ok := abiContract.Methods[method]
		if !ok {
			panic(fmt.Errorf("method %s not found", method))
		}

		// Encode input
		data, err := abi.Encode(args, m.Inputs)
		if err != nil {
			panic(err)
		}
		data = append(m.ID(), data...)

		// estimate gas limit
		callMsg := &web3.CallMsg{
			From: key.Address(),
			To:   &contractAddr,
			Data: data,
		}
		rawStr, err := clt.Eth().Call(callMsg, web3.Latest)
		if err != nil {
			panic(err)
		}
		raw, err := hex.DecodeString(rawStr[2:])
		if err != nil {
			panic(err)
		}
		respInterface, err := abi.Decode(m.Outputs, raw)
		if err != nil {
			panic(err)
		}
		return respInterface.(map[string]interface{})
	}

	// send events
	receipt = sendTransaction(&web3.Transaction{
		To:    &contractAddr,
		Input: testutil.MethodSig("setA1"),
	})
	fmt.Println(receipt.Logs)

	resp := sendCall("setA", oneAddr, big.NewInt(1))
	fmt.Println(resp["0"].(web3.Address))
	fmt.Println(resp["1"].(*big.Int))
}

func waitForReceipt(client *jsonrpc.Client, hash web3.Hash) (*web3.Receipt, error) {
	var receipt *web3.Receipt
	var count uint64
	var err error

	for {
		receipt, err = client.Eth().GetTransactionReceipt(hash)
		if err != nil {
			if err.Error() != "not found" {
				return nil, err
			}
		}
		if receipt != nil {
			break
		}
		if count > 5 {
			return nil, fmt.Errorf("timeout")
		}
		time.Sleep(1 * time.Second)
		count++
	}
	return receipt, nil
}
