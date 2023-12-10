package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/tinkler/subscribe_uniswap/internal/arg"
	"github.com/tinkler/subscribe_uniswap/internal/collector"
)

var fromTime time.Time
var topics = [][]common.Hash{{}}

func init() {
	fromTime, _ = time.Parse(time.RFC3339[:10], "2023-12-10")
}

func main() {
	// Dial new client
	client, err := ethclient.Dial(os.Getenv(arg.FlagEthereumNetworkAddress))
	if err != nil {
		fmt.Println("Failed to connect to the Ethereum network:", err)
		return
	}
	defer client.Close()

	waitc := make(chan struct{})
	uniswapV2Address := common.HexToAddress(`0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D`)
	uniswapV3Address := common.HexToAddress(`0xE592427A0AEce92De3Edee1F18E0157C05861564`)
	go startSubscribe(client, []common.Address{uniswapV2Address, uniswapV3Address})

	<-waitc
}

func startSubscribe(client *ethclient.Client, uniswapAddresses []common.Address) {

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

	query := ethereum.FilterQuery{
		FromBlock: latestBlock.Number(),
		ToBlock:   latestBlock.Number(),
		Addresses: uniswapAddresses,
		Topics:    topics,
	}
	logs, err := client.FilterLogs(context.Background(), query)
	if err != nil {
		fmt.Println("Failed to filter logs:", err)
		return
	}

	for _, l := range logs {
		collector.DefaultCollector.Store(l.TxHash.String(), l)
	}

	// success to get the latest block's uniswap logs
	currentBlockNumber = latestBlockNumber
	// 重新采集之前的
	go func() {
		var (
			preBlockNumber = latestBlockNumber - 1
		)
		for {
			preBlock, err := client.BlockByNumber(context.Background(), big.NewInt(int64(preBlockNumber)))
			if err != nil {
				fmt.Printf("Faile to get the block with number:%d, will retry in 1s\n", preBlockNumber)
				time.Sleep(time.Second)
				continue
			}
			if time.Unix(int64(preBlock.Time()), 0).Before(fromTime) {
				break
			}

			query := ethereum.FilterQuery{
				FromBlock: preBlock.Number(),
				ToBlock:   preBlock.Number(),
				Addresses: uniswapAddresses,
				Topics:    topics,
			}
			logs, err := client.FilterLogs(context.Background(), query)
			if err != nil {
				fmt.Println("Failed to filter logs:", err)
				return
			}

			for _, l := range logs {
				collector.DefaultCollector.Store(l.TxHash.String(), l)
			}
		}
	}()

	// subscribe new head
	headers := make(chan *types.Header)
	sub, err := client.SubscribeNewHead(context.Background(), headers)
	if err != nil {
		fmt.Println("Failed to subscribe new header")
		return
	}

	recapturing := uint32(0)

	for {
		select {
		case err := <-sub.Err():
			log.Fatal(err)
		case header := <-headers:
			headerBlockNumber := header.Number.Uint64()
			fmt.Println("New block header:", header.Number.String())

			logs, err := client.FilterLogs(context.Background(), ethereum.FilterQuery{
				Addresses: uniswapAddresses,
				Topics:    topics,
				FromBlock: header.Number,
				ToBlock:   header.Number,
			})

			if err != nil {
				log.Fatal(err)
			}

			for _, log := range logs {
				collector.DefaultCollector.Store(log.TxHash.String(), log)
			}

			// compare header's number with currentBlockNumber
			// 不可预见原因导致丢失,数据补偿逻辑
			if currentBlockNumber+1 < headerBlockNumber {
				go func(targetBlockNumber uint64) {
					if atomic.CompareAndSwapUint32(&recapturing, 0, 1) {
						defer func() {
							atomic.StoreUint32(&recapturing, 0)
						}()
						logs, err = client.FilterLogs(context.Background(), ethereum.FilterQuery{
							Addresses: uniswapAddresses,
							Topics:    topics,
							FromBlock: big.NewInt(int64(currentBlockNumber) + 1),
							ToBlock:   big.NewInt(int64(targetBlockNumber) - 1),
						})

						if err != nil {
							log.Fatal(err)
						}

						for _, log := range logs {
							collector.DefaultCollector.Store(log.TxHash.String(), log)
						}
						currentBlockNumber = targetBlockNumber
					}
				}(headerBlockNumber)
			}
		}
	}
}
