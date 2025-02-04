package main

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/labstack/echo/v4"
)

type FailingRequest struct {
	address string
	// err should be non-nil if it is a non-retryable error
	// such that the replica at `address` should be removed
	err error
}

type BufferAtSenderRequest struct {
	// Method must be either GET, PUT, or DELETE
	Method  string
	Payload any
	// Endpoint must start with a /
	Endpoint string

	// Targets to send the request to
	Targets []string
}

func sendViewRequest(method string, addr string, socketAddr string, path string) (*http.Response, error) {
	payload := map[string]string{
		"socket-address": socketAddr,
	}
	endpoint := "/view" + path
	return SendRequest(HttpRequest{
		method:   method,
		endpoint: endpoint,
		addr:     addr,
		payload:  payload,
	})
}

// FilterViews removes `exclude` from the list of `views`
func FilterViews(views []string, exclude ...string) []string {
	var newViews []string
	for _, v := range views {
		if !slices.Contains(exclude, v) {
			newViews = append(newViews, v)
		}
	}
	return newViews
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

	payload := map[string]string{
		"socket-address": socket.Address,
	}

	replica.BufferAtSender(&BufferAtSenderRequest{
		Method:   http.MethodPut,
		Payload:  payload,
		Endpoint: "/view",
		Targets:  FilterViews(replica.GetOtherViews(), socket.Address),
	})

	return c.JSON(http.StatusOK, ResponseNC{Result: "added"})
}

func (replica *Replica) handleViewGet(c echo.Context) error {
	if len(replica.View) == 0 {
		replica.View = append(replica.View, replica.addr)
	}
	return c.JSON(http.StatusOK, replica.ViewInfo)
}

func (replica *Replica) BufferAtSender(pr *BufferAtSenderRequest) error {
	switch pr.Method {
	case http.MethodPut, http.MethodDelete:
		break
	default:
		return errors.New(fmt.Sprintf("invalid method %s", pr.Method))
	}

	conns := pr.Targets
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*15)
	defer cancel()

	for {
		zap.L().Info("Sending requests to the following replicas", zap.Strings("conns", conns), zap.String("method", pr.Method), zap.String("endpoint", pr.Endpoint))
		select {
		case <-ctx.Done():
			zap.L().Error("BufferAtSender timed out")
			return errors.New("timed out")
		default:
			failingReqs := Broadcast(pr)
			// Retry the broadcast request
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
			replica.BufferAtSender(&BufferAtSenderRequest{
				Method:   http.MethodDelete,
				Payload:  payload,
				Endpoint: "/view",
				Targets:  FilterViews(replica.View, socket.Address),
			})

		}

		return c.JSON(http.StatusOK, ResponseNC{Result: "deleted"})
	}
	return c.JSON(http.StatusNotFound, ErrResponse{Error: "View has no such replica"})
}
