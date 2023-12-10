package main

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	_ "github.com/joho/godotenv/autoload"
	"github.com/tinkler/subscribe_uniswap/internal/arg"
	"github.com/tinkler/subscribe_uniswap/internal/collector"
)

var (
	uniswapV2Address = common.HexToAddress(`0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D`)
	uniswapV3Address = common.HexToAddress(`0xE592427A0AEce92De3Edee1F18E0157C05861564`)
)

var fromTime time.Time
var topics = [][]common.Hash{{}}
var captureAddresses = []common.Address{uniswapV2Address, uniswapV3Address}

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

	currentBlockNumber, err := historyCapture(client)
	if err != nil {
		fmt.Println("Program running error:", err.Error())
		os.Exit(0)
	}
	go startSubscribeHead(client, currentBlockNumber)

	<-waitc
}

// 返回历史数据抓取的区块高度
func historyCapture(client *ethclient.Client) (currentBlockNumber uint64, err error) {
	// Get the latest header
	header, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		fmt.Println("Failed to get the latest header:", err)
		return 0, err
	}
	latestBlockNumber := header.Number.Uint64()
	fmt.Println("Latest Block Number:", latestBlockNumber)
	// Get the latest block data's uniswap log
	latestBlock, err := client.BlockByNumber(context.Background(), big.NewInt(int64(latestBlockNumber)))
	if err != nil {
		fmt.Println("Failed to get the latest block data")
		return 0, err
	}
	query := ethereum.FilterQuery{
		FromBlock: latestBlock.Number(),
		ToBlock:   latestBlock.Number(),
		Addresses: captureAddresses,
		Topics:    topics,
	}
	logs, err := client.FilterLogs(context.Background(), query)
	if err != nil {
		fmt.Println("Failed to filter logs:", err)
		return 0, err
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
				Addresses: captureAddresses,
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

	return currentBlockNumber, nil
}

func startSubscribeHead(client *ethclient.Client, fromBlockNumber uint64) error {

	currentBlockNumber := fromBlockNumber
	// subscribe new head
	headers := make(chan *types.Header)
	sub, err := client.SubscribeNewHead(context.Background(), headers)
	if err != nil {
		fmt.Printf("Failed to subscribe new header, %s\n", err.Error())
		return err
	}

	recapturing := uint32(0)

	for {
		select {
		case err := <-sub.Err():
			fmt.Printf("Subscribe NewHead err: %s\n", err)
			return err
		case header := <-headers:
			headerBlockNumber := header.Number.Uint64()
			fmt.Println("New block header:", header.Number.String())

			logs, err := client.FilterLogs(context.Background(), ethereum.FilterQuery{
				Addresses: captureAddresses,
				Topics:    topics,
				FromBlock: header.Number,
				ToBlock:   header.Number,
			})

			if err != nil {
				fmt.Printf("Failed to subscribe block %d data: %s", header.Number.Int64(), err.Error())
				continue
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
						fromBlockNumber := big.NewInt(int64(currentBlockNumber) + 1)
						toBlockNumber := big.NewInt(int64(targetBlockNumber) - 1) // targetBlock has been captured
						logs, err = client.FilterLogs(context.Background(), ethereum.FilterQuery{
							Addresses: captureAddresses,
							Topics:    topics,
							FromBlock: fromBlockNumber,
							ToBlock:   toBlockNumber,
						})

						if err != nil {
							fmt.Printf("Recapture from %d to %d error:%s\n", fromBlockNumber.Int64(), toBlockNumber.Int64(), err)
							return
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
