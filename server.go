package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/davecgh/go-spew/spew"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"
)

type StoreValue struct {
	Value any `json:"value"`
}

type Request struct {
	StoreValue
	CausalMetadata VectorClock `json:"causal-metadata"`
	IsBroadcast    bool        `json:"is-broadcast,omitempty"`
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

type Shard struct {
	id           string
	nodeAddrList []string
}

type Replica struct {
	vcLock  sync.Mutex
	kv      map[string]any
	vc      *VectorClock
	addr    string
	shards  map[string][]string
	shardId string
	*ViewInfo
}

type DataTransfer struct {
	Kv map[string]any `json:"Kv"`
	Vc VectorClock    `json:"Vc"`
}

func (r *Replica) ChooseData() {
	var choices []DataTransfer
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

	slices.SortFunc(choices, func(a, b DataTransfer) int {
		return int(a.Vc.Compare(&b.Vc))
	})
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
		shards: make(map[string][]string),
	}
}

func (r *Replica) initReplica() {
	zap.L().Info("Initializing replica", zap.String("addr", r.addr))
	payload := map[string]string{
		"socket-address": r.addr,
	}

	payloadJson, err := json.Marshal(payload)

	if err != nil {
		zap.L().Error("server init", zap.Error(err))
	} else {
		zap.L().Info("Registering new replica with its views", zap.Strings("views", r.View))
		Broadcast(&BroadcastRequest{
			PollRequest: PollRequest{
				Method:   http.MethodPut,
				Payload:  payloadJson,
				Endpoint: "/view",
			},
			Targets: r.GetOtherViews(),
		})
	}
	r.ChooseData()
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
		zap.L().Info("In Status", zap.String("kv", spew.Sdump(r.kv)), zap.Strings("replicas", r.View))
		return nil
	}
}

func main() {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()
	zap.ReplaceGlobals(logger)

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
	e.PUT("/shard/add-member/:id", server.handleShardMemberPut)
	e.GET("/shard/ids", server.handleShardIdGet)
	e.GET("/shard/node-shard-id", server.handleShardNodeGet)
	e.GET("/shard/members/:id", server.handleShardMembersGet)
	e.GET("/shard/key-count/:id", server.handleShardKeyCount)
	e.GET("/data", server.handleDataTransfer)

	server.initReplica()
	e.Logger.Fatal(e.Start(":8090"))
}
