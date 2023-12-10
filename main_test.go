package main

import (
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	_ "github.com/joho/godotenv/autoload"
	"github.com/tinkler/subscribe_uniswap/internal/arg"
	"github.com/tinkler/subscribe_uniswap/internal/collector"
)

func TestHistoryCapture(t *testing.T) {
	client, err := newEthClient(os.Getenv(arg.FlagEthereumNetworkAddress))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	currentBlockNumber, err := historyCapture(client)
	if err != nil {
		t.Fatal(err)
		os.Exit(0)
	}
	if currentBlockNumber == 0 {
		t.Fail()
	}
	time.Sleep(time.Second * 3)
}

func TestStartSubscribeHead(t *testing.T) {
	client, err := ethclient.Dial(os.Getenv(arg.FlagEthereumNetworkAddressWss))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	go func() {
		if err := startSubscribeHead(client, 0); err != nil {
			t.Log(err)
			t.Fail()
		}
	}()

	go func() {
		<-time.NewTimer(time.Second * 15).C
		t.Fail()
	}()

	for range time.NewTicker(time.Second).C {
		has := false
		collector.DefaultCollector.Range(func(key, value any) bool {
			has = true
			return true
		})
		if has {
			return
		}
	}

}
