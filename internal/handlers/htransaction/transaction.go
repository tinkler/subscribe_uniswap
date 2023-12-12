package htransaction

import (
	"net/http"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/gin-gonic/gin"
	"github.com/tinkler/subscribe_uniswap/internal/collector"
	"github.com/tinkler/subscribe_uniswap/internal/model/block_chain"
)

func Get(ctx *gin.Context) {
	txnAddr := ctx.Param("txn")
	if txnAddr == "" {
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}

	txnIn, exist := collector.DefaultTransactionCollector.Load(txnAddr)
	if exist {
		ctx.JSON(http.StatusOK, gin.H{"data": txnIn.(*types.Transaction)})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"status": 1, "message": "Transaction is not found"})
}

var (
	cachedLatestBlockNumber  uint64
	cachedLatestTransactions []*types.Transaction
)

type transactionArr []*types.Transaction

func (a transactionArr) Len() int           { return len(a) }
func (a transactionArr) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a transactionArr) Less(i, j int) bool { return a[i].Time().Before(a[j].Time()) }

func cacheLatestTransactions() {
	latestBlockNumber := collector.DefaultBlockCollector.LatestBlockNumber()
	if latestBlockNumber == 0 {
		cachedLatestTransactions = make([]*types.Transaction, 0)
		return
	}
	if atomic.LoadUint64(&cachedLatestBlockNumber) != latestBlockNumber {
		latestBlock := collector.DefaultBlockCollector.LatestBlock()
		if txns := latestBlock.Transactions(); len(txns) >= 10 {
			sort.Sort(sort.Reverse(transactionArr(txns)))
			cachedLatestTransactions = txns[:10]
			atomic.StoreUint64(&cachedLatestBlockNumber, latestBlockNumber)
		} else {
			var preBlock *types.Block
			var preBlockNumber = latestBlockNumber - 1
			var depth = 10
			for preBlock != nil && depth > 0 {
				depth--
				preBlock, _ = collector.DefaultBlockCollector.Load(preBlockNumber)
			}
			if preBlock != nil {
				txns = append(txns, preBlock.Transactions()...)
			}
			sort.Sort(sort.Reverse(transactionArr(txns)))
			cachedLatestTransactions = txns[:10]
			atomic.StoreUint64(&cachedLatestBlockNumber, latestBlockNumber)
		}
	}
}

func filteredTransaction(ver string) []*types.Transaction {
	uniswapAddress := ""
	switch ver {
	case "v2":
		uniswapAddress = block_chain.UniswapV2Address.String()
	case "v3":
		uniswapAddress = block_chain.UniswapV3Address.String()
	}
	latestBlockNumber := collector.DefaultBlockCollector.LatestBlockNumber()
	if latestBlockNumber == 0 {
		return nil
	}
	var (
		data         []*types.Transaction
		currentBlock *types.Block = collector.DefaultBlockCollector.LatestBlock()
	)
	for len(data) < 10 && currentBlock != nil {
		txns := currentBlock.Transactions()
		sort.Sort(sort.Reverse(transactionArr(txns)))
		for i := 0; i < len(txns); i++ {
			if txns[i].To().String() == uniswapAddress {
				data = append(data, txns[i])
				if len(data) > 10 {
					break
				}
			}
		}
	}
	return data
}

func LatestTransaction(ctx *gin.Context) {
	ver := ctx.Param("ver")
	if ver != "" && ver != "v2" && ver != "v3" {
		ctx.JSON(http.StatusOK, gin.H{"status": 1, "message": "version is missing"})
		return
	}
	query := strings.TrimSpace(ctx.GetString("query"))
	if len(ver) == 0 && len(query) == 0 {
		cacheLatestTransactions()
		ctx.JSON(http.StatusOK, gin.H{"status": 0, "data": cachedLatestTransactions})
		return
	} else if len(query) > 0 {
		var data []*types.Transaction
		if d, ok := collector.DefaultTransactionCollector.Load(query); ok {
			txn := d.(*types.Transaction)
			switch ver {
			case "v2":
				if txn.To().String() == block_chain.UniswapV2Address.String() {
					data = append(data, txn)
				}
			case "v3":
				if txn.To().String() == block_chain.UniswapV3Address.String() {
					data = append(data, txn)
				}
			default:
				data = append(data, txn)
			}
		}
		ctx.JSON(http.StatusOK, gin.H{"status": 0, "data": data})
		return
	} else {
		ctx.JSON(http.StatusOK, gin.H{"status": 0, "data": filteredTransaction(ver)})
	}

}
