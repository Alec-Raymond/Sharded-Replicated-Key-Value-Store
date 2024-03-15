package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type BroadcastRequest = BufferAtSenderRequest

type BroadcastFirstRequest struct {
	BroadcastRequest
	srcAddr string
}

// Broadcast sends the requests to all the nodes in br.Targets
func Broadcast(br *BroadcastRequest) []FailingRequest {
	// zap.L().Info("In Broadcast", zap.Any("payload", *br))
	var failingReqs []FailingRequest

	urls := br.Targets
	for _, addr := range urls {
		// Create request
		payload := br.Payload
		endpoint := br.Endpoint
		method := br.Method

		res, err := SendRequest(HttpRequest{
			method:   method,
			endpoint: endpoint,
			addr:     addr,
			payload:  payload,
		})
		if err != nil {
			failingReqs = append(failingReqs, FailingRequest{
				address: addr,
				err:     errors.New("failed request"),
			})
			zap.L().Warn("Request failed to", zap.String("addr", addr), zap.String("method", method), zap.Error(err))
			continue

		}
		// Retry if status code is 503
		if res.StatusCode == 503 {
			zap.L().Info("Response returned 503. Going to retry this", zap.String("addr", addr), zap.String("method", method))
			failingReqs = append(failingReqs, FailingRequest{
				address: addr,
			})
			continue
		}
	}
	return failingReqs
}

// BroadcastFirst sends requests to the list of target nodes until one
// responds successfully. If one fails to respond it sends a delete request
func BroadcastFirst(br *BroadcastFirstRequest) (*http.Response, error) {
	zap.L().Info("In BroadcastFirst")
	var (
		res *http.Response
		err error
	)
	for _, n := range br.Targets {
		p := br.Payload
		res, err = SendRequest(HttpRequest{
			method:   br.Method,
			endpoint: br.Endpoint,
			addr:     n,
			payload:  p,
		})
		if err == nil {
			break
		}
		zap.L().Warn("couldn't send read request", zap.String("remote-node", n))
		zap.L().Info("deleting node", zap.String("delete-node", n))
		sendViewRequest(http.MethodDelete, br.srcAddr, n, "")
	}
	return res, nil

}

type HttpRequest struct {
	method   string
	endpoint string
	addr     string
	payload  any
}

func SendRequest(r HttpRequest) (*http.Response, error) {
	requestURL, err := url.Parse(fmt.Sprintf("http://%s%s", r.addr, r.endpoint))
	if err != nil {
		zap.L().Error("Couldn't parse URL", zap.Error(err))
		return nil, err
	}

	json, err := json.Marshal(r.payload)
	if err != nil {
		zap.L().Error("Couldn't marshal to JSON", zap.Error(err))
		return nil, err
	}

	req, err := http.NewRequest(r.method, requestURL.String(), bytes.NewBuffer(json))
	if err != nil {
		zap.L().Error("Couldn't construct request", zap.Error(err))
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	client := http.Client{
		Timeout: 200 * time.Millisecond,
	}
	return client.Do(req)
}
