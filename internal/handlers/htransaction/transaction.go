package htransaction

import (
	"net/http"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/gin-gonic/gin"
	"github.com/tinkler/subscribe_uniswap/internal/collector"
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
	return
}
