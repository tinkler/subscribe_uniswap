package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"math/big"
	"os"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/tinkler/subscribe_uniswap/internal/arg"
)

func main() {
	// Dial new client
	client, err := ethclient.Dial(os.Getenv(arg.FlagEthereumNetworkAddress))
	if err != nil {
		fmt.Println("Failed to connect to the Ethereum network:", err)
		return
	}
	defer client.Close()

	waitc := make(chan struct{})
	// v2
	go startSubscribe(client, common.HexToAddress(``), []byte(``))
	// v3
	go startSubscribe(client, common.HexToAddress(``), []byte(``))
	<-waitc
}

func startSubscribe(client *ethclient.Client, uniswapAddress common.Address, uniswapABIJSON []byte) {
	// Parse the ABI
	uniswapABI, err := abi.JSON(bytes.NewReader(uniswapABIJSON))
	if err != nil {
		fmt.Println("Failed to parse the abi json")
		return
	}

	var (
		latestBlockNumber  uint64
		currentBlockNumber uint64
	)

	// Get the latest header
	header, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		fmt.Println("Failed to get the latest header:", err)
		return
	}
	latestBlockNumber = header.Number.Uint64()
	fmt.Println("Latest Block Number:", latestBlockNumber)
	// Get the latest block data's uniswap log
	latestBlock, err := client.BlockByNumber(context.Background(), big.NewInt(int64(latestBlockNumber)))
	if err != nil {
		fmt.Println("Failed to get the latest block data")
		return
	}

	topics := [][]common.Hash{{}, {uniswapABI.Events["Transfer"].ID}}
	query := ethereum.FilterQuery{
		FromBlock: latestBlock.Number(),
		ToBlock:   latestBlock.Number(),
		Addresses: []common.Address{uniswapAddress},
		Topics:    topics,
	}
	logs, err := client.FilterLogs(context.Background(), query)
	if err != nil {
		fmt.Println("Failed to filter logs:", err)
		return
	}

	for _, l := range logs {
		// TODO: store to mem
		fmt.Println(l.TxHash.Hex())
	}

	// success to get the latest block's uniswap logs
	currentBlockNumber = latestBlockNumber

	// subscribe new head
	headers := make(chan *types.Header)
	sub, err := client.SubscribeNewHead(context.Background(), headers)
	if err != nil {
		fmt.Println("Failed to subscribe new header")
		return
	}

	//

	for {
		select {
		case err := <-sub.Err():
			log.Fatal(err)
		case header := <-headers:
			headerBlockNumber := header.Number.Uint64()
			fmt.Println("New block header:", header.Number.String())

			logs, err := client.FilterLogs(context.Background(), ethereum.FilterQuery{
				Addresses: []common.Address{uniswapAddress},
				Topics:    topics,
				FromBlock: header.Number,
				ToBlock:   header.Number,
			})

			if err != nil {
				log.Fatal(err)
			}

			for _, log := range logs {
				// TODO: store to mem
				fmt.Println("Transfer event:", log)
			}

			// compare header's number with currentBlockNumber and impute missing logs
			if currentBlockNumber+1 < headerBlockNumber {
				logs, err = client.FilterLogs(context.Background(), ethereum.FilterQuery{
					Addresses: []common.Address{uniswapAddress},
					Topics:    topics,
					FromBlock: big.NewInt(int64(currentBlockNumber) + 1),
					ToBlock:   big.NewInt(int64(headerBlockNumber) - 1),
				})

				if err != nil {
					log.Fatal(err)
				}

				for _, log := range logs {
					// TODO: store to mem
					fmt.Println("Transfer event:", log)
				}
			}

		}
	}
}
