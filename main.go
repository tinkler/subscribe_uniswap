package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

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

func newEthClient(ctx context.Context, rawurl string) (*ethclient.Client, error) {

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

	c, err := rpc.DialOptions(ctx, rawurl, rpc.WithHTTPClient(&hc))
	if err != nil {
		return nil, err
	}

	return ethclient.NewClient(c), nil

}

func main() {
	ctx, _ := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	// Dial new client
	client, err := newEthClient(ctx, os.Getenv(arg.FlagEthereumNetworkAddress))
	if err != nil {
		fmt.Println("Failed to connect to the Ethereum network:", err)
		return
	}
	defer client.Close()
	wssClient, err := ethclient.DialContext(ctx, os.Getenv(arg.FlagEthereumNetworkAddressWss))
	if err != nil {
		fmt.Println("Failed to connect to the Ethereum wss network:", err)
		return
	}
	defer client.Close()

	// TODO: check if is finalized
	currentBlockNumber, err := historyCapture(ctx, client)
	if err != nil {
		fmt.Println("Program running error:", err.Error())
		os.Exit(0)
	}
	fmt.Println(currentBlockNumber)
	go func() {
		if err := holdSubscribeHead(ctx, wssClient, currentBlockNumber); err != nil {
			os.Exit(0)
		}
	}()

	<-ctx.Done()
	fmt.Println("Shutdown success")
}

func includes(captureAddresses []common.Address, target *common.Address) bool {
	if target == nil {
		return false
	}
	for _, addr := range captureAddresses {
		if addr.String() == target.String() {
			return true
		}
	}
	return false
}

func capture(client *ethclient.Client, blockNumber uint64, captureAddresses []common.Address) error {
	block, err := client.BlockByNumber(context.Background(), big.NewInt(18754505))
	if err != nil {
		return err
	}
	captureBlock(block, captureAddresses)
	return nil
}

func captureBlock(block *types.Block, captureAddresses []common.Address) {
	for _, txn := range block.Transactions() {
		if includes(captureAddresses, txn.To()) {
			collector.DefaultCollector.Store(txn.Hash().String(), txn)
		}
	}
}

// 返回历史数据抓取的区块高度
func historyCapture(ctx context.Context, client *ethclient.Client) (currentBlockNumber uint64, err error) {
	// Get the latest header
	header, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		fmt.Println("Failed to get the latest header:", err)
		return 0, err
	}
	latestBlockNumber := header.Number.Uint64()
	fmt.Println("Latest Block Number:", latestBlockNumber)
	// Get the latest block data's uniswap log
	if err := capture(client, latestBlockNumber, captureAddresses); err != nil {
		fmt.Printf("Failed to capture block %d logs\n", latestBlockNumber)
		return 0, err
	}

	// success to get the latest block's uniswap logs
	currentBlockNumber = latestBlockNumber
	// 重新采集之前的
	go func() {
		var (
			preBlockNumber = latestBlockNumber - 1
		)
		for {
			if ctx.Err() != nil {
				return
			}
			preBlock, err := client.BlockByNumber(ctx, big.NewInt(int64(preBlockNumber)))
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				fmt.Printf("Faile to get the block with number:%d, will retry in 1s\n", preBlockNumber)
				time.Sleep(time.Second)
				continue
			}
			if time.Unix(int64(preBlock.Time()), 0).Before(fromTime) {
				// stop recapture the trasactions which are before fromTime
				break
			}
			captureBlock(preBlock, captureAddresses)

			preBlockNumber--
		}
	}()

	return currentBlockNumber, nil
}

func holdSubscribeHead(ctx context.Context, client *ethclient.Client, fromBlockNumber uint64) error {
	retryCount := 0
	currentBlockNumber := fromBlockNumber
	for {
		var err error
		currentBlockNumber, err = startSubscribeHead(ctx, client, currentBlockNumber)
		if err != nil {
			if retryCount <= 3 {
				retryCount++
				time.Sleep(time.Second * time.Duration(10*retryCount))
				fmt.Printf("Retry %d subscribe header\n", retryCount)
				continue
			}
			return errors.New("Failed to subscribe new head more than 3 times")
		}
	}
}

func startSubscribeHead(ctx context.Context, client *ethclient.Client, fromBlockNumber uint64) (uint64, error) {

	currentBlockNumber := fromBlockNumber

	// subscribe new head
	headers := make(chan *types.Header)
	sub, err := client.SubscribeNewHead(ctx, headers)
	if err != nil {
		fmt.Printf("Failed to subscribe new header, %s\n", err.Error())
		return currentBlockNumber, err
	}
	defer sub.Unsubscribe()

	initilized := false
	recapturing := uint32(0)

	for {
		select {
		case err := <-sub.Err():
			fmt.Printf("Subscribe NewHead err: %s\n", err)
			return currentBlockNumber, err
		case header := <-headers:
			headerBlockNumber := header.Number.Uint64()
			// no from 0
			if fromBlockNumber == 0 && !initilized {
				initilized = true
				currentBlockNumber = headerBlockNumber
			}
			fmt.Println("New block header:", header.Number.String())

			if err := capture(client, headerBlockNumber, captureAddresses); err != nil {
				fmt.Printf("Capture block %d err:%s\n", headerBlockNumber, err.Error())
				continue
			}

			// compare header's number with currentBlockNumber
			// 不可预见原因导致丢失,数据补偿逻辑
			if currentBlockNumber+1 < headerBlockNumber {
				go func(targetBlockNumber uint64) {
					if atomic.CompareAndSwapUint32(&recapturing, 0, 1) {
						defer func() {
							atomic.StoreUint32(&recapturing, 0)
						}()
						capture(client, targetBlockNumber, captureAddresses)
						currentBlockNumber = targetBlockNumber
					}
				}(headerBlockNumber)
			} else {
				currentBlockNumber = headerBlockNumber
			}
		case <-ctx.Done():
			return currentBlockNumber, nil
		}
	}
}
