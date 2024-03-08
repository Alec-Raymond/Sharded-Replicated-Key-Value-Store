package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type BroadcastRequest struct {
	PollRequest
	Replicas []string
}

// Broadcast sends the requests to all the replicas
func Broadcast(br *BroadcastRequest) []FailingRequest {
	zap.L().Info("In Broadcast", zap.Any("payload", *br))
	var failingReqs []FailingRequest

	urls := br.Replicas
	for _, addr := range urls {
		//defer wg.Done()
		// Create request
		payload := br.Payload
		endpoint := br.Endpoint
		method := br.Method

		url := fmt.Sprintf("http://%s%s", addr, endpoint)
		zap.L().Info("Broadcasting to", zap.String("url", url))
		// fmt.Printf("Broadcasting to %s, with payload %v\n", url, payload)
		req, err := http.NewRequest(method, url, bytes.NewReader(payload))
		if err != nil {
			log.Println("[ERROR]", err)
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
