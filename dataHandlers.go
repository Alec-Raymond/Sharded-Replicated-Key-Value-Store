package main

import (
	"maps"
	"net/http"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

type Request struct {
	StoreValue
	CausalMetadata VectorClock `json:"causal-metadata"`
	IsBroadcast    bool        `json:"is-broadcast,omitempty"`
}

type Response struct {
	Result         string      `json:"result"`
	CausalMetadata VectorClock `json:"causal-metadata"`
	ShardId        string      `json:"shard-id"`
}

type GetResponse struct {
	Response
	StoreValue
}

func (r *Replica) handlePut(c echo.Context) error {
	request := new(Request)
	key := c.Param("key")

	if len(key) > 50 {
		return c.JSON(http.StatusBadRequest, ErrResponse{Error: "Key is too long"})
	}

	err := c.Bind(request)
	if err != nil || request.Value == nil {
		spew.Dump(request)
		spew.Dump(err)
		return c.JSON(
			http.StatusBadRequest,
			ErrResponse{Error: "PUT request does not specify a value"},
		)
	}

	// Read client's causal metadata.
	remoteHost := strings.Split(c.Request().RemoteAddr, ":")[0]

	zap.L().Debug("In PUT /kvs/:key", zap.String("remoteHost", remoteHost), zap.String("realIp", c.RealIP()))

	clientClock := GetClientVectorClock(request, remoteHost)

	// Check if all causal dependencies are satisfied
	if !r.vc.IsReadyFor(clientClock, false, &r.vcLock) {
		return c.JSON(
			http.StatusServiceUnavailable,
			ErrResponse{Error: "Causal Dependencies not satisfied; try again later"},
		)
	}

	// Prepare broadcast
	if !request.IsBroadcast {
		copiedClock := CloneVC(clientClock)
		broadcastPayload := Request{
			StoreValue:     StoreValue{Value: request.Value},
			CausalMetadata: copiedClock,
			IsBroadcast:    true,
		}

		go r.BufferAtSender(&BufferAtSenderRequest{
			Method:   http.MethodPut,
			Payload:  broadcastPayload,
			Endpoint: "/kvs/" + key,
			Targets:  FilterViews(r.shards[r.shardId], r.addr),
		})
	}

	// Update both vector clocks
	r.vc.Accept(&clientClock, false, &r.vcLock)
	_, ok := r.kv[key]

	// Broadcast the updated vc to all other replicas outside of the current shard
	copiedClock := CloneVC(*r.vc)
	broadcastPayload := CMRequest{
		CausalMetadata: copiedClock,
	}

	go r.BufferAtSender(&BufferAtSenderRequest{
		Method:   http.MethodPut,
		Endpoint: "/cm",
		Targets:  FilterViews(r.GetOtherViews(), r.shards[r.shardId]...),
		Payload:  broadcastPayload,
	})

	if !ok {
		r.kv[key] = request.Value
		// Still need to return the updated causal metadata
		zap.L().Debug("Created kv", zap.String("key", key), zap.Any("value", r.kv[key]), zap.String("producer IP", c.RealIP()))
		return c.JSON(http.StatusCreated, Response{Result: "created", CausalMetadata: clientClock})
	}

	r.kv[key] = request.Value
	zap.L().Debug("Replaced kv", zap.String("key", key), zap.Any("value", r.kv[key]), zap.String("producer IP", c.RealIP()))
	return c.JSON(http.StatusOK, Response{Result: "replaced", CausalMetadata: clientClock, ShardId: r.shardId})
}

func (r *Replica) handleGet(c echo.Context) error {
	request := new(Request)
	key := c.Param("key")

	_ = c.Bind(request)

	clientClock := GetClientVectorClock(request, c.Request().RemoteAddr)

	// Check if all causal dependencies are satisfied
	if !r.vc.IsReadyFor(clientClock, true, &r.vcLock) {
		return c.JSON(
			http.StatusServiceUnavailable,
			ErrResponse{Error: "Causal Dependencies not satisfied; try again later"},
		)
	}

	val, ok := r.kv[key]

	if !ok {
		// Not sure why we don't need causal metadata here, shouldn't this count as the reader finding out about a potential delete event or that a write to this key has not yet happened?
		return c.JSON(http.StatusNotFound, ErrResponse{Error: "Key does not exist"})
	}

	r.vc.Accept(&clientClock, true, &r.vcLock)

	zap.L().Info("In GET /kvs/:key", zap.String("key", key), zap.Any("value", val), zap.String("ip", c.RealIP()))

	return c.JSON(http.StatusOK, GetResponse{
		Response: Response{
			Result:         "found",
			CausalMetadata: clientClock,
			ShardId:        r.shardId,
		},
		StoreValue: StoreValue{
			Value: val,
		},
	})
}

func (r *Replica) handleDelete(c echo.Context) error {
	request := new(Request)
	key := c.Param("key")

	_ = c.Bind(request)

	clientClock := GetClientVectorClock(request, c.Request().RemoteAddr)

	if !r.vc.IsReadyFor(clientClock, false, &r.vcLock) {
		return c.JSON(
			http.StatusServiceUnavailable,
			ErrResponse{Error: "Causal Dependencies not satisfied; try again later"},
		)
	}

	_, ok := r.kv[key]
	if !ok {
		return c.JSON(http.StatusNotFound, ErrResponse{Error: "Key does not exist"})
	}

	// Prepare broadcast
	if !request.IsBroadcast {
		copiedClock := VectorClock{
			Self:   clientClock.Self,
			Clocks: make(map[string]int),
		}
		maps.Copy(copiedClock.Clocks, clientClock.Clocks)
		broadcastPayload := Request{
			StoreValue:     StoreValue{Value: request.Value},
			CausalMetadata: copiedClock,
			IsBroadcast:    true,
		}

		go r.BufferAtSender(&BufferAtSenderRequest{
			Method:   http.MethodDelete,
			Payload:  broadcastPayload,
			Endpoint: "/kvs/" + key,
		})
	}

	r.vc.Accept(&clientClock, false, &r.vcLock)

	delete(r.kv, key)

	zap.L().Info("In DELETE /kvs/:key", zap.String("key", key), zap.String("ip", c.RealIP()))

	return c.JSON(http.StatusOK, Response{Result: "deleted", CausalMetadata: clientClock, ShardId: r.shardId})
}

func (r *Replica) handleDataTransfer(c echo.Context) error {
	return c.JSON(http.StatusOK, DataTransfer{Kv: r.kv, Vc: *r.vc})
}
