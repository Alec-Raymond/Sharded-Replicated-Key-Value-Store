package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/davecgh/go-spew/spew"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type StoreValue struct {
	Value any `json:"value"`
}

type CausalMetadata struct {
	Metadata VectorClock `json:"causal-metadata"`
}

type Request struct {
	StoreValue
	Metadata    VectorClock `json:"causal-metadata"`
	IsBroadcast bool        `json:"is-broadcast,omitempty"`
}

type Response struct {
	Result         string      `json:"result"`
	CausalMetadata VectorClock `json:"causal-metadata"`
}

type ResponseNC struct {
	Result string `json:"result"`
}

type ErrResponse struct {
	Error string `json:"error"`
}

type GetResponse struct {
	Response
	StoreValue
}

type Replica struct {
	vcLock sync.Mutex
	kv     map[string]any
	vc     *VectorClock
	addr   string
	*ViewInfo
}

type DataTransfer struct {
	Kv map[string]any `json:"Kv"`
	Vc VectorClock    `json:"Vc"`
}

type Choices []DataTransfer

func (c Choices) Len() int           { return len(c) }
func (c Choices) Swap(i, j int)      { c[i], c[j] = c[j], c[i] }
func (c Choices) Less(i, j int) bool { return c[i].Vc.Compare(&c[j].Vc) == Lesser }

func (r *Replica) ChooseData() {
	var choices Choices
	for _, replica := range r.View {
		resp, err := http.Get("http://" + replica + "/data")
		if err != nil {
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		var data DataTransfer
		err = json.Unmarshal(body, &data)
		if err != nil {
			continue
		}
		choices = append(choices, data)
	}

	sort.Sort(choices)
	if len(choices) == 0 {
		return
	}
	last := len(choices) - 1
	r.kv, r.vc = choices[last].Kv, &choices[last].Vc
	r.vc.Self = r.addr
}

func NewReplica() *Replica {
	address := os.Getenv("SOCKET_ADDRESS")
	view := os.Getenv("VIEW")
	return &Replica{
		addr: address,
		ViewInfo: &ViewInfo{
			View: strings.Split(view, ","),
		},
		kv: make(map[string]any),
		vc: &VectorClock{
			Clocks: make(map[string]int),
			Self:   address,
		},
	}
}

func (r *Replica) initReplica() {
	log.Println("[INFO] Initializing replica at", r.addr)
	payload := map[string]string{
		"socket-address": r.addr,
	}

	payloadJson, err := json.Marshal(payload)

	if err != nil {
		fmt.Println("server init json err", err)
	} else {
		log.Println("[INFO] Registering new replica with its views", r.View)
		Broadcast(&BroadcastRequest{
			PollRequest: PollRequest{
				Method:      http.MethodPut,
				Payload:     payloadJson,
				Endpoint:    "/view",
				ExcludeAddr: r.addr,
			},
			Replicas: r.GetOtherViews(),
		})
	}
	r.ChooseData()
	spew.Dump(r.kv, r.View)
}

func (r *Replica) GetOtherViews() []string {
	otherViews := []string{}
	for _, view := range r.View {
		if view != r.addr {
			otherViews = append(otherViews, view)
		}
	}
	return otherViews
}

type SocketAddress struct {
	Address     string `json:"socket-address"`
	IsBroadcast bool   `json:"is-broadcast"`
}

type ViewInfo struct {
	View []string `json:"view"`
}

func (r *Replica) ReplicaStatus(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if err := next(c); err != nil {
			c.Error(err)
		}
		r.vcLock.Lock()
		defer r.vcLock.Unlock()
		log.Printf("[INFO] In Status, kv = %s, replicas = %s\n", spew.Sdump(r.kv), spew.Sdump(r.View))
		return nil
	}
}

func main() {
	e := echo.New()

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "method=${method}, uri=${uri}, status=${status}\n",
	}))

	server := NewReplica()

	e.Use(server.ReplicaStatus)
	e.PUT("/kvs/:key", server.handlePut)
	e.GET("/kvs/:key", server.handleGet)
	e.DELETE("/kvs/:key", server.handleDelete)
	e.PUT("/view", server.handleViewPut)
	e.GET("/view", server.handleViewGet)
	e.DELETE("/view", server.handleViewDelete)
	e.GET("/data", server.handleDataTransfer)

	server.initReplica()
	e.Logger.Fatal(e.Start(":8090"))
}
