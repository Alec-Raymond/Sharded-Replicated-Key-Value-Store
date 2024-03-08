package main

/*
import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Simple smoke test to ensure no concurrency issues
func TestLongPollSmoke(t *testing.T) {
	timeout := time.After(3 * time.Second)
	done := make(chan bool)
	go func() {
		assert.NoError(t, LongPoll(&PollRequest{
			Method:   "GET",
			Payload:  nil,
			Endpoint: "/",
			Replicas: []string{"https://google.com"},
		}))
		done <- true
	}()
	select {
	case <-timeout:
		t.Fatal("Test timed out")
	case <-done:
	}
}

// Check that we don't retry non-503 errors
func TestLongPoll_FailingNon503(t *testing.T) {
	tries := 0
	badSvr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tries++
		w.WriteHeader(404)
	}))
	defer badSvr.Close()
	timeout := time.After(3 * time.Second)
	done := make(chan bool)
	go func() {
		assert.NoError(t, LongPoll(&PollRequest{
			Method:   "GET",
			Payload:  nil,
			Endpoint: "/",
			Replicas: []string{badSvr.URL},
		}))
		done <- true
	}()
	select {
	case <-timeout:
		t.Fatal("Test timed out")
	case <-done:
	}
	assert.Equal(t, tries, 1)

}

// Check that we don't retry successful responses
func TestLongPoll_SuccessServer(t *testing.T) {
	tries := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tries++
		w.WriteHeader(200)
	}))
	defer srv.Close()
	timeout := time.After(3 * time.Second)
	done := make(chan bool)
	go func() {
		assert.NoError(t, LongPoll(&PollRequest{
			Method:   "GET",
			Payload:  nil,
			Endpoint: "/",
			Replicas: []string{srv.URL},
		}))
		done <- true
	}()
	select {
	case <-timeout:
		t.Fatal("Test timed out")
	case <-done:
	}
	assert.Equal(t, tries, 1)
}

// Check that we don't allow non-GET, PUT, & DELETE requests
func TestLongPoll_InvalidMethod(t *testing.T) {
	assert.Error(t, LongPoll(&PollRequest{
		Method:   "PATCH",
		Payload:  nil,
		Endpoint: "/",
		Replicas: []string{"https://google.com"},
	}))

}
*/
