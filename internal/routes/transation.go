package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/tinkler/subscribe_uniswap/internal/handlers/htransaction"
)

func TransactionRoute(r *gin.RouterGroup) {
	r.GET("/:txn", htransaction.Get)
}

func TransactionListRoute(r *gin.RouterGroup) {
	r.GET("/:ver", htransaction.LatestTransaction)
}
