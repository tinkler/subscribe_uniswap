package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tinkler/subscribe_uniswap/internal/routes"
)

func NewHttpServer(addr string) *http.Server {
	router := gin.Default()

	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	transactionGroup := router.Group("/transaction")
	routes.TransactionRoute(transactionGroup)

	return srv
}
