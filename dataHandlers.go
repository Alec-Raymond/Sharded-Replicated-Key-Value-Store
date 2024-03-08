package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
)

func (r *Replica) handlePut(c echo.Context) error {
	request := new(Request)
	key := c.Param("key")

	if len(key) > 50 {
		return c.JSON(http.StatusBadRequest, ErrResponse{Error: "Key is too long"})
	}

	err := c.Bind(request)
	if err != nil || request.Value == nil {
		return c.JSON(
			http.StatusBadRequest,
			ErrResponse{Error: "PUT request does not specify a value"},
		)
	}

	// Read client's causal metadata.
	remoteHost := strings.Split(c.Request().RemoteAddr, ":")[0]

	// log.Println("[DEBUG] Remote host", remoteHost)
	// log.Println("[DEBUG] Real IP", c.RealIP())
	// log.Println("[DEBUG] Request details:", spew.Sdump(c.Request().Header))

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
		copiedClock := VectorClock{
			Self:   clientClock.Self,
			Clocks: make(map[string]int),
		}
		maps.Copy(copiedClock.Clocks, clientClock.Clocks)
		broadcastPayload := Request{
			StoreValue:  StoreValue{Value: request.Value},
			Metadata:    copiedClock,
			IsBroadcast: true,
		}

		broadcastPayloadJson, err := json.Marshal(broadcastPayload)

		if err != nil {
			fmt.Println("failed to marshal broadcast payload (handlePut):", broadcastPayload, "with error", err)
			os.Exit(1)
		}

		go r.BufferAtSender(&PollRequest{
			Method:   "PUT",
			Payload:  broadcastPayloadJson,
			Endpoint: "/kvs/" + key,
		})
	}

	// Update both vector clocks
	r.vc.Accept(&clientClock, false, &r.vcLock)
	_, ok := r.kv[key]

	if !ok {
		r.kv[key] = request.Value
		// Still need to return the updated causal metadata
		fmt.Println(key, ":", r.kv[key], "created at", c.RealIP())
		return c.JSON(http.StatusCreated, Response{Result: "created", CausalMetadata: clientClock})
	}

	r.kv[key] = request.Value
	fmt.Println(key, ":", r.kv[key], "replacement at", c.RealIP())
	return c.JSON(http.StatusOK, Response{Result: "replaced", CausalMetadata: clientClock})
}

func (r *Replica) handleGet(c echo.Context) error {
	request := new(Request)
	key := c.Param("key")

	_ = c.Bind(request)

	clientClock := GetClientVectorClock(request, c.Request().RemoteAddr)

	// fmt.Println("Client Vector Clock:", clientClock.Clocks)
	// fmt.Println("My Vector Clock:", r.vc.Clocks)

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

	fmt.Println("key", key, "value:", val, "got at", c.RealIP())

	return c.JSON(http.StatusOK, GetResponse{
		Response: Response{
			Result:         "found",
			CausalMetadata: clientClock,
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
			StoreValue:  StoreValue{Value: request.Value},
			Metadata:    copiedClock,
			IsBroadcast: true,
		}

		broadcastPayloadJson, err := json.Marshal(broadcastPayload)

		if err != nil {
			fmt.Println("failed to marshal broadcast payload (handlePut):", broadcastPayload, "with error", err)
			os.Exit(1)
		}

		go r.BufferAtSender(&PollRequest{
			Method:      http.MethodDelete,
			Payload:     broadcastPayloadJson,
			Endpoint:    "/kvs/" + key,
			ExcludeAddr: "",
		})
	}

	r.vc.Accept(&clientClock, false, &r.vcLock)

	delete(r.kv, key)

	fmt.Println("Key", key, "deleted at", c.RealIP())

	return c.JSON(http.StatusOK, Response{Result: "deleted", CausalMetadata: clientClock})
}

func (r *Replica) handleDataTransfer(c echo.Context) error {
	return c.JSON(http.StatusOK, DataTransfer{Kv: r.kv, Vc: *r.vc})
}
