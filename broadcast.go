package main

import (
	"bytes"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type BroadcastRequest struct {
	BufferAtSenderRequest
	Targets []string
}

// Broadcast sends the requests to all the nodes in br.Targets
func Broadcast(br *BroadcastRequest) []FailingRequest {
	zap.L().Info("In Broadcast", zap.Any("payload", *br))
	var failingReqs []FailingRequest

	urls := br.Targets
	for _, addr := range urls {
		// Create request
		payload := br.Payload
		endpoint := br.Endpoint
		method := br.Method

		url := fmt.Sprintf("http://%s%s", addr, endpoint)
		zap.L().Info("Broadcasting to", zap.String("url", url))
		req, err := http.NewRequest(method, url, bytes.NewReader(payload))
		if err != nil {
			zap.L().Error("Couldn't create request", zap.Error(err))
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		// Perform request
		client := http.Client{
			Timeout: 200 * time.Millisecond,
		}
		res, err := client.Do(req)
		if err != nil {
			failingReqs = append(failingReqs, FailingRequest{
				address: addr,
				err:     errors.New("failed request"),
			})
			zap.L().Warn("Request failed to", zap.String("url", url), zap.String("method", method))
			continue

		}
		// Retry if status code is 503
		if res.StatusCode == 503 {
			zap.L().Info("Response returned 503. Going to retry this", zap.String("url", url), zap.String("method", method))
			failingReqs = append(failingReqs, FailingRequest{
				address: addr,
			})
			continue
		}
	}
	return failingReqs
}
