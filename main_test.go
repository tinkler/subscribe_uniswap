package main

import (
	"context"
	"log"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	_ "github.com/joho/godotenv/autoload"
	"github.com/tinkler/subscribe_uniswap/internal/arg"
	"github.com/tinkler/subscribe_uniswap/internal/collector"
)

func TestClient(t *testing.T) {
	client, err := newEthClient(context.Background(), os.Getenv(arg.FlagEthereumNetworkAddress))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	block, err := client.BlockByNumber(context.Background(), big.NewInt(18754505))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, txn := range block.Transactions() {
		if txn.Hash().String() == "0x1d30ad54836553ad89393fe69a458a309551409edb2de2a90ce7021a382e6c64" {
			found = true
			log.Println(txn.To().String() == captureAddresses[0].String())
		}
	}
	if !found {
		t.Fail()
	}
}

func TestHistoryCapture(t *testing.T) {
	client, err := newEthClient(context.Background(), os.Getenv(arg.FlagEthereumNetworkAddress))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	currentBlockNumber, err := historyCapture(context.Background(), client)
	if err != nil {
		t.Fatal(err)
		os.Exit(0)
	}
	if currentBlockNumber == 0 {
		t.Fail()
	}
	time.Sleep(time.Second * 20)
	has := false
	collector.DefaultCollector.Range(func(key, value any) bool {
		has = true
		return true
	})
	if !has {
		t.Fail()
	}
}

func TestStartSubscribeHead(t *testing.T) {
	client, err := ethclient.Dial(os.Getenv(arg.FlagEthereumNetworkAddressWss))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	go func() {
		if _, err := startSubscribeHead(context.Background(), client, 0); err != nil {
			t.Log(err)
			t.Fail()
		}
	}()

	waitc := make(chan struct{})
	go func() {
		<-time.NewTimer(time.Second * 20).C
		close(waitc)
	}()

	has := false

	go func() {
		for range time.NewTicker(time.Second).C {

			collector.DefaultCollector.Range(func(key, value any) bool {
				has = true
				return true
			})

		}

	}()
	<-waitc
	if !has {
		t.Fail()
	}

}
