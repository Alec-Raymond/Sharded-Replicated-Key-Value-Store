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

// initKV initializes a Replica's kv store, vc, and shard mapping from the existing replicas with the
// most updated state.
func (r *Replica) initKV(shardId string) {
	// Get all the shards from the first responsive node
	r.shardId = shardId
	res, err := BroadcastFirst(&BroadcastFirstRequest{
		BroadcastRequest: BroadcastRequest{
			Method:   http.MethodGet,
			Targets:  r.GetOtherViews(),
			Endpoint: "/shard/ids",
		},
		srcAddr: r.addr,
	})
	if err != nil {
		zap.L().Fatal("unable to get shardIds")
	}
	var shards ShardIdsResponse
	data, err := io.ReadAll(res.Body)
	if err != nil {
		zap.L().Fatal("unable to read response body")
	}
	err = json.Unmarshal(data, &shards)
	if err != nil {
		zap.L().Fatal("unable to unmarshal response body")
	}
	r.shards, err = initShards(len(shards.ShardIds), r.View)
	if err != nil {
		zap.L().Fatal("unable to initialize shards body")
	}
	// Get the kv data
	shard := r.shards[r.shardId]
	var choices []DataTransfer
	for _, replica := range shard {
		if replica == r.addr {
			continue
		}
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
	// Skip registration if the shardCount is not 0 indicating that
	// the replica has come up for the first time
	if r.shardCount != 0 {
		return
	}
	zap.L().Info("Initializing replica", zap.String("addr", r.addr))
	payload := map[string]string{
		"socket-address": r.addr,
	}

	zap.L().Info("Registering new replica with its views", zap.Strings("views", r.View))
	Broadcast(&BroadcastRequest{
		Method:   http.MethodPut,
		Payload:  payload,
		Endpoint: "/view",
		Targets:  r.GetOtherViews(),
	})
	// Don't initialize KV yet, because we don't know what shard we are part of.
}

func initShards(shardCount int, view []string) (map[string][]string, error) {
	shards := make(map[string][]string)
	var start int
	var shardName string

	if shardCount == 0 {
		return shards, nil
	}

	shardSize := len(view) / shardCount

	if shardSize < 2 {
		return nil, fmt.Errorf("average shard size cannot satisfy fault-tolerance: there are %d shards, %d replicas and an even sharding would result in %d replicas per shard", shardCount, len(view), shardSize)
	}

	start = 0
	for shardId := 0; shardId < shardCount; shardId++ {
		shardName = fmt.Sprintf("s%d", shardId)
		shards[shardName] = view[start : start+shardSize]
		if start+2*shardSize > len(view) {
			shards[shardName] = append(shards[shardName], view[start+shardSize:]...)
		}
		start += shardSize
	}

	zap.L().Info("Initialize Shards", zap.Any("shards", shards))

	return shards, nil
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

	shards, err := initShards(shardCount, strings.Split(view, ","))

	// Get the nodeShardId of the current node
	nodeShardId := ""
	for sh, nodes := range shards {
		if slices.Contains(nodes, address) {
			nodeShardId = sh
			break
		}
	}

	if err != nil {
		panic(err)
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
		shards:     shards,
		shardId:    nodeShardId,
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
