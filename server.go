package main

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"
)

// ReplicaStatus is a middleware that prints out the KV state and views for a given replica
func (r *Replica) ReplicaStatus(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if err := next(c); err != nil {
			c.Error(err)
		}
		r.vcLock.Lock()
		defer r.vcLock.Unlock()
		zap.L().Info("In Status", zap.Any("kv", r.kv), zap.Strings("views", r.View))
		return nil
	}
}

func main() {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()
	zap.ReplaceGlobals(logger)

	e := echo.New()

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "method=${method}, uri=${uri}, status=${status}\n",
	}))

	server := NewReplica()

	e.Use(server.ReplicaStatus)
	e.PUT("/kvs/:key", server.handlePut)
	e.GET("/kvs/:key", server.handleGet)
	e.DELETE("/kvs/:key", server.handleDelete)
	e.PUT("/view", server.handleViewPut)
	e.GET("/view", server.handleViewGet)
	e.DELETE("/view", server.handleViewDelete)
	e.PUT("/shard/add-member/:id", server.handleShardMemberPut)
	e.GET("/shard/ids", server.handleShardIdGet)
	e.GET("/shard/node-shard-id", server.handleShardNodeGet)
	e.GET("/shard/members/:id", server.handleShardMembersGet)
	e.GET("/shard/key-count/:id", server.handleShardKeyCount)
	e.GET("/data", server.handleDataTransfer)
	e.PUT("/cm", server.handlePutCM)

	server.initReplica()
	e.Logger.Fatal(e.Start(":8090"))
}
