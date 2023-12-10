package main

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	_ "github.com/joho/godotenv/autoload"
	"github.com/tinkler/subscribe_uniswap/internal/arg"
	"github.com/tinkler/subscribe_uniswap/internal/collector"
)

var (
	uniswapV2Address = common.HexToAddress(`0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D`)
	uniswapV3Address = common.HexToAddress(`0xE592427A0AEce92De3Edee1F18E0157C05861564`)
)

var fromTime time.Time
var topics = [][]common.Hash{}
var captureAddresses = []common.Address{
	uniswapV2Address,
	uniswapV3Address,
}

func init() {
	fromTime, _ = time.Parse(time.RFC3339[:10], "2023-12-10")
}

func newEthClient(rawurl string) (*ethclient.Client, error) {

	hc := http.Client{}
	if proxyURL := os.Getenv(arg.FlagProxy); len(proxyURL) > 0 {
		// Parse the proxy URL
		proxy, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}

		// Create a new transport and set the proxy
		transport := &http.Transport{
			Proxy: http.ProxyURL(proxy),
		}
		hc.Transport = transport
	}

	c, err := rpc.DialOptions(context.Background(), rawurl, rpc.WithHTTPClient(&hc))
	if err != nil {
		return nil, err
	}

	return ethclient.NewClient(c), nil

}

func main() {
	// Dial new client
	client, err := newEthClient(os.Getenv(arg.FlagEthereumNetworkAddress))
	if err != nil {
		fmt.Println("Failed to connect to the Ethereum network:", err)
		return
	}
	defer client.Close()
	wssClient, err := ethclient.Dial(os.Getenv(arg.FlagEthereumNetworkAddressWss))
	if err != nil {
		fmt.Println("Failed to connect to the Ethereum wss network:", err)
		return
	}
	defer client.Close()

	waitc := make(chan struct{})

	// TODO: check if is finalized
	currentBlockNumber, err := historyCapture(client)
	if err != nil {
		fmt.Println("Program running error:", err.Error())
		os.Exit(0)
	}
	fmt.Println(currentBlockNumber)
	go func() {
		if err := startSubscribeHead(wssClient, currentBlockNumber); err != nil {
			os.Exit(0)
		}
	}()

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
				// Topics:    topics,
			}
			logs, err := client.FilterLogs(context.Background(), query)
			if err != nil {
				fmt.Println("Failed to filter logs:", err)
				return
			}

			for _, l := range logs {
				collector.DefaultCollector.Store(l.TxHash.String(), l)
			}

			fmt.Printf("Success to get block %d\n", preBlockNumber)

			preBlockNumber--
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

	initilized := false
	recapturing := uint32(0)

	for {
		select {
		case err := <-sub.Err():
			fmt.Printf("Subscribe NewHead err: %s\n", err)
			return err
		case header := <-headers:
			headerBlockNumber := header.Number.Uint64()
			// no from 0
			if fromBlockNumber == 0 && !initilized {
				initilized = true
				currentBlockNumber = headerBlockNumber
			}
			fmt.Println("New block header:", header.Number.String())

			logs, err := client.FilterLogs(context.Background(), ethereum.FilterQuery{
				Addresses: captureAddresses,
				FromBlock: header.Number,
				ToBlock:   header.Number,
				Topics:    topics,
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
			} else {
				currentBlockNumber = headerBlockNumber
			}
		}
	}
}
