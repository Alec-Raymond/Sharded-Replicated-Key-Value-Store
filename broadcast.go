package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

type BroadcastRequest struct {
	PollRequest
	Replicas []string
}

// Broadcast sends the requests to all the replicas
func Broadcast(br *BroadcastRequest) []FailingRequest {
	//wg := sync.WaitGroup{}
	log.Println("[INFO] In broadcast")
	var failingReqs []FailingRequest

	urls := br.Replicas
	//wg.Add(len(urls))
	for _, addr := range urls {
		//defer wg.Done()
		// Create request
		payload := br.Payload
		endpoint := br.Endpoint
		method := br.Method

		url := fmt.Sprintf("http://%s%s", addr, endpoint)
		log.Println("[INFO] Broadcasting to", url)
		// fmt.Printf("Broadcasting to %s, with payload %v\n", url, payload)
		req, err := http.NewRequest(method, url, bytes.NewReader(payload))
		if err != nil {
			log.Println("[ERROR]", err)
			continue
		}
		// ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*300)
		// defer cancel()
		// req.WithContext(ctx)
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
			log.Printf("[INFO] Done sending request to channel")
			continue

		}
		// Retry if status code is 503
		if res.StatusCode == 503 {
			log.Printf("[WARN] Broadcasting %s to %s failed...need to retry\n", method, url)
			failingReqs = append(failingReqs, FailingRequest{
				address: addr,
			})
			continue
		}
	}
	//wg.Wait()
	return failingReqs
}
