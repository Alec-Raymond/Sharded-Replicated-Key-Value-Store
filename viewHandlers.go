package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/labstack/echo/v4"
)

func sendViewRequest(method string, addr string, send string, path string) (*http.Response, error) {
	requestURL, err := url.Parse("http://" + addr + "/view" + path)
	if err != nil {
		zap.L().Error("Couldn't parse URL", zap.Error(err))
	}

	payload := map[string]string{
		"socket-address": send,
	}
	json, err := json.Marshal(payload)
	if err != nil {
		zap.L().Error("Couldn't marshal to JSON", zap.Error(err))
		return nil, err
	}
	req, err := http.NewRequest(method, requestURL.String(), bytes.NewBuffer(json))
	if err != nil {
		zap.L().Error("Couldn't construct request", zap.Error(err))
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)

	return resp, err
}

func (replica *Replica) handleViewPut(c echo.Context) error {
	var socket SocketAddress
	err := c.Bind(&socket)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrResponse{Error: "Bad Request"})
	}

	if len(replica.View) == 0 {
		replica.View = append(replica.View, replica.addr)
	}

	for _, addr := range replica.View {
		if addr == socket.Address {
			return c.JSON(http.StatusOK, ResponseNC{Result: "already present"})
		}
	}

	replica.View = append(replica.View, socket.Address)
	// sendRequest(http.MethodGet, serverAddr, socket.Address, "/heartbeat")
	// TO-DO: Trigger recurring/scheduled heartbeat

	payload := map[string]string{
		"socket-address": socket.Address,
	}

	broadcastPayload, err := json.Marshal(payload)

	if err != nil {
		zap.L().Error("failed to marshal broadcast payload (handlePut):", zap.Any("broadcastPayload", broadcastPayload), zap.Error(err))
		return c.JSON(http.StatusInternalServerError, ErrResponse{Error: "failed to marshal payload to json"})
	}

	go replica.BufferAtSender(&PollRequest{
		Method:      http.MethodPut,
		Payload:     broadcastPayload,
		Endpoint:    "/view",
		ExcludeAddr: socket.Address,
		// Only other replicas will be broadcasted to
	})

	return c.JSON(http.StatusOK, ResponseNC{Result: "added"})
}

func (replica *Replica) handleViewGet(c echo.Context) error {
	if len(replica.View) == 0 {
		replica.View = append(replica.View, replica.addr)
	}
	return c.JSON(http.StatusOK, replica.ViewInfo)
}

type FailingRequest struct {
	address string
	// err should be non-nil if it is a non-retryable error
	// such that the replica at `address` should be removed
	err error
}

type PollRequest struct {
	// Method must be either GET, PUT, or DELETE
	Method  string
	Payload []byte
	// Endpoint must start with a /
	Endpoint string

	// ExcludeAddr is the endpoint to not broadcast the
	// address to
	ExcludeAddr string
}

// RemoveExcludedAddress removes `exlude` from the list of `views`
func RemoveExcludedAddress(views []string, exclude string) []string {
	var newViews []string
	for _, v := range views {
		if v != exclude {
			newViews = append(newViews, v)
		}
	}
	return newViews
}

func (replica *Replica) BufferAtSender(pr *PollRequest) error {
	switch pr.Method {
	case http.MethodGet, http.MethodPut, http.MethodDelete:
		break
	default:
		return errors.New(fmt.Sprintf("invalid method %s", pr.Method))
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*15)
	defer cancel()

	conns := RemoveExcludedAddress(replica.GetOtherViews(), pr.ExcludeAddr)
	for {
		zap.L().Info("Longpolling the following replicas", zap.Strings("conns", conns))
		select {
		case <-ctx.Done():
			zap.L().Error("LongPoll timed out")
			return errors.New("timed out")
		default:
			failingReqs := Broadcast(&BroadcastRequest{
				Replicas:    conns,
				PollRequest: *pr,
			})
			var toRetry []string
			for _, val := range failingReqs {
				if val.err == nil {
					toRetry = append(toRetry, val.address)
				} else {
					// Delete the view if it got a non-503 error
					zap.L().Warn("Deleting view", zap.String("address", val.address))
					sendViewRequest(http.MethodDelete, replica.addr, val.address, "")
				}
			}
			if len(toRetry) == 0 {
				return nil
			}
			conns = toRetry
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func (replica *Replica) handleViewDelete(c echo.Context) error {
	zap.L().Info("In DELETE /view", zap.Strings("view", replica.View))
	defer func() {
		zap.L().Info("Exiting DELETE /view", zap.Strings("view", replica.View))
	}()
	var socket SocketAddress
	err := c.Bind(&socket)
	if err != nil {
		zap.L().Error("Couldn't obtain socket address")
		return c.JSON(http.StatusBadRequest, ErrResponse{Error: "Bad Request"})
	}

	if len(replica.View) == 0 {
		replica.View = append(replica.View, replica.addr)
	}

	if replica.addr == socket.Address {
		zap.L().Error("Can't delete Self")
		return c.JSON(http.StatusBadRequest, ErrResponse{Error: "Can't Delete Self"})
	}

	var new_view []string
	for _, addr := range replica.View {
		if socket.Address != addr {
			new_view = append(new_view, addr)
		}
	}

	if len(new_view) != len(replica.View) {
		replica.View = new_view
		if !socket.IsBroadcast {
			payload := map[string]any{
				"socket-address": socket.Address,
				"is-broadcast":   true,
			}
			broadcastPayload, err := json.Marshal(payload)

			if err != nil {
				zap.L().Error("failed to marshal broadcast payload (handlePut):", zap.Any("broadcastPayload", broadcastPayload), zap.Error(err))
				return c.JSON(http.StatusInternalServerError, ErrResponse{Error: "failed to marshal payload to json"})
			}
			go replica.BufferAtSender(&PollRequest{
				Method:      http.MethodDelete,
				Payload:     broadcastPayload,
				Endpoint:    "/view",
				ExcludeAddr: socket.Address,
			})

		}

		return c.JSON(http.StatusOK, ResponseNC{Result: "deleted"})
	}
	return c.JSON(http.StatusNotFound, ErrResponse{Error: "View has no such replica"})
}
