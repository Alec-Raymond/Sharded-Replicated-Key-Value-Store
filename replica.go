package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"go.uber.org/zap"
)

type StoreValue struct {
	Value any `json:"value"`
}

type ViewInfo struct {
	View []string `json:"view"`
}

type Replica struct {
	vcLock     sync.Mutex
	kv         map[string]any
	vc         *VectorClock
	addr       string
	shards     map[string][]string
	shardId    string
	shardCount int
	*ViewInfo
}

type DataTransfer struct {
	Kv map[string]any `json:"Kv"`
	Vc VectorClock    `json:"Vc"`
}

func getKvData(addr string) (DataTransfer, error) {
	resp, err := http.Get(fmt.Sprintf("http://%s/data", addr))
	if err != nil {
		return DataTransfer{}, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return DataTransfer{}, err
	}

	var data DataTransfer
	err = json.Unmarshal(body, &data)
	if err != nil {
		return DataTransfer{}, err
	}
	return data, nil
}

// initKV initializes a Replica's kv store and vc from the existing replicas with the
// most updated state.
func (r *Replica) initKV() {
	var choices []DataTransfer
	for _, replica := range r.View {
		data, err := getKvData(replica)
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

func (r *Replica) initReplica() {
	// Skip registration if the shardCount is 0 indicating that
	// the replica has stopped and came back up
	if r.shardCount == 0 {
		return
	}
	zap.L().Info("Initializing replica", zap.String("addr", r.addr))
	payload := map[string]string{
		"socket-address": r.addr,
	}

	payloadJson, err := json.Marshal(payload)

	if err != nil {
		zap.L().Error("JSON error during server init", zap.Error(err))
	} else {
		zap.L().Info("Registering new replica with its views", zap.Strings("views", r.View))
		Broadcast(&BroadcastRequest{
			BufferAtSenderRequest: BufferAtSenderRequest{
				Method:   http.MethodPut,
				Payload:  payloadJson,
				Endpoint: "/view",
			},
			Targets: r.GetOtherViews(),
		})
	}
	r.initKV()
}

func NewReplica() *Replica {
	address := os.Getenv("SOCKET_ADDRESS")
	view := os.Getenv("VIEW")
	shardCountStr := os.Getenv("SHARD_COUNT")
	var (
		shardCount int
		err        error
	)
	if shardCountStr != "" {
		shardCount, err = strconv.Atoi(os.Getenv("SHARD_COUNT"))
		if err != nil {
			panic(err)
		}

	}
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
		shardCount: shardCount,
		shards:     make(map[string][]string),
	}
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
