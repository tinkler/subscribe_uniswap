package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	_ "github.com/joho/godotenv/autoload"
	"github.com/tinkler/subscribe_uniswap/internal/arg"
	"github.com/tinkler/subscribe_uniswap/internal/model/block_chain"
	"github.com/tinkler/subscribe_uniswap/internal/server"
)

func main() {
	ctx, _ := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	// Dial new client
	client, err := block_chain.NewEthClient(ctx, os.Getenv(arg.FlagEthereumNetworkAddress), os.Getenv(arg.FlagProxy))
	if err != nil {
		fmt.Println("Failed to connect to the Ethereum network:", err)
		return
	}
	defer client.Close()
	// Dial wss client
	wssClient, err := ethclient.DialContext(ctx, os.Getenv(arg.FlagEthereumNetworkAddressWss))
	if err != nil {
		fmt.Println("Failed to connect to the Ethereum wss network:", err)
		return
	}
	defer client.Close()

	// TODO: check if is finalized
	currentBlockNumber, err := block_chain.HistoryCapture(ctx, client)
	if err != nil {
		fmt.Println("Program running error:", err.Error())
		os.Exit(0)
	}
	fmt.Println(currentBlockNumber)
	go func() {
		if err := block_chain.HoldSubscribeHead(ctx, wssClient, currentBlockNumber); err != nil {
			os.Exit(0)
		}
	}()
	go block_chain.RepareBlockChain(ctx, client)

	var srv *http.Server
	if httpAddress := os.Getenv(arg.FlagListen); httpAddress != "" {
		srv = server.NewHttpServer(httpAddress)
		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Println("Failed to listen http server," + err.Error())
			}
		}()
		fmt.Println("Listen http server on " + httpAddress)
	}

	<-ctx.Done()

	// shutdown context
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if srv != nil {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Println("Server forced to shutdown")
		}
	}

	fmt.Println("Shutdown success")
}
