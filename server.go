package main

import (
	"net/http"
	"strings"

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
		zap.L().Info("In Status" /*zap.Any("kv", r.kv), zap.Strings("views", r.View),*/, zap.Any("shards", r.shards))
		return nil
	}
}

func (r *Replica) ForwardRemoteKey(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		key := c.Param("key")
		if len(key) > 50 {
			return c.JSON(http.StatusBadRequest, ErrResponse{Error: "Key is too long"})
		}
		shardId := findShard(key, r.shards)

		// If it belongs to the current replica then call the next function
		if shardId == r.shardId {
			zap.L().Info("Local key, no need to forward")
			return next(c)
		}

		// Otherwise begin forwarding
		nodes, ok := r.shards[shardId]
		if !ok {
			zap.L().Error("No nodes to forward to", zap.String("shardId", shardId))
			return c.JSON(http.StatusInternalServerError, ErrResponse{Error: "No nodes in shard"})
		}
		method := c.Request().Method
		br := BroadcastRequest{
			Targets:  nodes,
			Method:   method,
			Endpoint: "/kvs/" + key,
		}
		// Update causal metadata and send it downstream
		request := new(Request)
		if err := c.Bind(request); err != nil {
			return c.JSON(http.StatusBadRequest, ErrResponse{Error: "invalid data format"})
		}
		remoteHost := strings.Split(c.Request().RemoteAddr, ":")[0]
		request.CausalMetadata = GetClientVectorClock(request, remoteHost)
		br.Payload = request

		zap.L().Info("Remote key, forwarding request to", zap.String("shardId", shardId), zap.Strings("nodes", nodes))
		res, err := BroadcastFirst(&BroadcastFirstRequest{
			BroadcastRequest: br, srcAddr: r.addr,
		})
		// Return
		if err != nil {
			return c.JSON(
				http.StatusInternalServerError,
				ErrResponse{Error: "couldn't forward request"},
			)
		}
		return c.Stream(res.StatusCode, "application/json", res.Body)
	}
}

func main() {

	logger, _ := zap.NewDevelopment()
	defer logger.Sync()
	zap.ReplaceGlobals(logger)

	e := echo.New()
	server := NewReplica()

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "method=${method}, remote_ip=${remote_ip} uri=${uri}, status=${status}\n",
	}), server.ReplicaStatus, middleware.Recover())

	kv := e.Group("/kvs/:key", server.ForwardRemoteKey)
	kv.PUT("", server.handlePut)
	kv.GET("", server.handleGet)
	kv.DELETE("", server.handleDelete)

	e.PUT("/view", server.handleViewPut)
	e.GET("/view", server.handleViewGet)
	e.DELETE("/view", server.handleViewDelete)

	sh := e.Group("/shard")
	sh.PUT("/add-member/:id", server.handleShardMemberPut)
	sh.GET("/ids", server.handleShardIdGet)
	sh.GET("/node-shard-id", server.handleShardNodeGet)
	sh.GET("/members/:id", server.handleShardMembersGet)
	sh.GET("/key-count/:id", server.handleShardKeyCount)
	sh.PUT("/reshard", server.handleReshard)
	sh.PUT("/update", server.handleUpdateShard)

	e.GET("/data", server.handleDataTransfer)
	e.PUT("/cm", server.handlePutCM)

	server.initReplica()
	e.Logger.Fatal(e.Start(":8090"))
}
