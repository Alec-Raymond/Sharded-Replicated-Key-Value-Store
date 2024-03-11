package main

import (
	"net/http"

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

func (r *Replica) ForwardRemoteKey(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		var shardNames []string
		key := c.Param("key")
		if len(key) > 50 {
			return c.JSON(http.StatusBadRequest, ErrResponse{Error: "Key is too long"})
		}

		for shardName := range r.shards {
			shardNames = append(shardNames, shardName)
		}

		shardId := findShard(key, shardNames)

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
		var payload any = make(map[string]any)
		// Set the payload to the request body if it is a PUT request
		if method == http.MethodPut {
			request := new(Request)
			if err := c.Bind(request); err != nil || request == nil {
				return c.JSON(http.StatusBadRequest, ErrResponse{Error: "invalid data format"})
			}
			payload = request

		}
		zap.L().Info("remote key, forwarding request to", zap.String("shardId", shardId))
		res, err := SendRequest(HttpRequest{
			method:   method,
			endpoint: "/kv/" + key,
			addr:     nodes[0],
			payload:  payload,
		})
		// Return
		if err != nil {
			return c.JSON(http.StatusInternalServerError, ErrResponse{Error: "couldn't forward request"})
		}
		return c.Stream(res.StatusCode, "application/json", res.Body)
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
	e.Use(middleware.Recover())

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
