package main

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"slices"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

type ShardIdsResponse struct {
	ShardIds []string `json:"shard-ids"`
}

type NodeIdResponse struct {
	NodeShardId string `json:"node-shard-id"`
}

type ShardMembersResponse struct {
	ShardMembers []string `json:"shard-members"`
}

type ShardKeyCountResponse struct {
	ShardKeyCount int `json:"shard-key-count"`
}

type ReshardUpdate struct {
	ShardCount int                 `json:"shard-count"`
	ShardId    string              `json:"node-shard-id"`
	Shards     map[string][]string `json:"shards"`
	KV         map[string]any      `json:"kv"`
}

func (r *Replica) handleReshard(c echo.Context) error {
	type ReshardRequest struct {
		ShardCount int `json:"shard-count"`
	}
	fmt.Println("In handleReshard")
	rr := new(ReshardRequest)
	if err := c.Bind(rr); err != nil || rr == nil || rr.ShardCount == 0 {
		return c.JSON(http.StatusBadRequest,
			ErrResponse{Error: "Reshard request does not specify a valid shard count"},
		)
	}

	totalNodes := 0
	for _, nodes := range r.shards {
		totalNodes += len(nodes)
	}

	// Check if there are enough nodes to provide fault tolerance
	if math.Floor(float64(totalNodes)/float64(rr.ShardCount)) < 2 {
		return c.JSON(http.StatusBadRequest, ErrResponse{
			Error: "Not enough nodes to provide fault tolerance with requested shard count"},
		)
	}

	if rr.ShardCount == r.shardCount {
		return c.JSON(http.StatusOK, ActionResponse{
			Result: "resharded"})
	}

	zap.L().Info("Resharding", zap.String("leader-ip", r.addr))
	// Aggregate all the key-value pairs
	allKvs := make(map[string]any)
	for shardId, nodes := range r.shards {
		res, err := BroadcastFirst(&BroadcastFirstRequest{
			BroadcastRequest: BroadcastRequest{
				Method:   http.MethodGet,
				Endpoint: "/data",
				Targets:  nodes,
			},
		})
		if err != nil || res == nil {
			zap.L().Error("Failed to fetch data for", zap.String("shardId", shardId), zap.Error(err))
			return c.JSON(http.StatusInternalServerError, ErrResponse{Error: "couldn't fetch data"})
		}
		body, _ := io.ReadAll(res.Body)
		var data DataTransfer
		json.Unmarshal(body, &data)
		maps.Copy(allKvs, data.Kv)
	}
	zap.L().Info("Copied all KVS", zap.Int("num-keys", len(allKvs)))
	// Move nodes to new shard
	newShards, err := initShards(rr.ShardCount, r.View)
	if err != nil {
		zap.L().Error("Failed to init shard names", zap.Error(err))
		return c.JSON(http.StatusBadRequest, ErrResponse{Error: "bad reshard request"})
	}

	// Update nodes with new keys and new shardState
	newKv := make(map[string]map[string]any)
	for k, v := range allKvs {
		// Get KV for the relevant shard
		kv, ok := newKv[findShard(k, newShards)]
		if !ok {
			kv = make(map[string]any)
		}

		// Add this key value pair to it
		// zap.L().Info("Setting key in newKv", zap.Any("kv", kv))
		kv[k] = v
		newKv[findShard(k, newShards)] = kv
	}

	for shard := range newKv {
		zap.L().Debug(shard, zap.Int("len of keys allocated:", len(newKv[shard])))
	}

	// Broadcast this state update to each shard
	// Including self
	for sh, nodes := range newShards {
		go r.BufferAtSender(&BufferAtSenderRequest{
			Method: http.MethodPut,
			Payload: ReshardUpdate{
				ShardCount: rr.ShardCount,
				ShardId:    sh,
				Shards:     newShards,
				KV:         newKv[sh],
			},
			Targets:  nodes,
			Endpoint: "/shard/update",
		})
	}

	return c.JSON(http.StatusOK, ActionResponse{Result: "resharded"})
}

func (replica *Replica) handleUpdateShard(c echo.Context) error {
	ru := new(ReshardUpdate)
	if err := c.Bind(ru); err != nil || ru == nil {
		return c.JSON(http.StatusBadRequest, ErrResponse{Error: "missing KV, Shards, or node ID"})
	}
	replica.kv = ru.KV
	replica.shardId = ru.ShardId
	replica.shardCount = ru.ShardCount
	replica.shards = ru.Shards

	return c.JSON(http.StatusOK, ActionResponse{Result: "updated"})
}

func (replica *Replica) handleShardMemberPut(c echo.Context) error {
	shardId := c.Param("id")

	var socket SocketAddress
	err := c.Bind(&socket)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrResponse{Error: "Bad Request"})
	}

	viewExists := false
	// Sync replica data with shard if it hasn't already
	if replica.addr == socket.Address && replica.shardId == "" {
		replica.initKV(shardId)
	}
	_, shardExists := replica.shards[shardId]

	for _, r := range replica.View {
		if socket.Address == r {
			viewExists = true
		}
	}

	// Validate the node's existence as well as the shard
	if !shardExists && !viewExists {
		zap.L().Error("view and shard don't exist")
		return c.JSON(http.StatusNotFound, ErrResponse{Error: "View and Shard don't exist"})
	} else if !viewExists {
		zap.L().Error("view doesn't exist")
		return c.JSON(http.StatusNotFound, ErrResponse{Error: "View doesn't exist"})
	} else if !shardExists {
		zap.L().Error("shard doesn't exist", zap.String("shard-id", shardId))
		return c.JSON(http.StatusNotFound, ErrResponse{Error: "Shard doesn't exist"})
	}

	// Add this node to the shard if it isn't already
	if !slices.Contains(replica.shards[shardId], socket.Address) {
		replica.shards[shardId] = append(replica.shards[shardId], socket.Address)
	}
	// Then broadcast it if this hasn't been broadcast yet
	if !socket.IsBroadcast {
		payload := SocketAddress{
			Address:     socket.Address,
			IsBroadcast: true,
		}
		go replica.BufferAtSender(&BufferAtSenderRequest{
			Method:   http.MethodPut,
			Payload:  payload,
			Endpoint: "/shard/add-member/" + shardId,
			// Only other replicas will be broadcasted to
			Targets: replica.GetOtherViews(),
		})
	}
	return c.JSON(http.StatusOK, ResponseNC{Result: "node added to shard"})

}

func (replica *Replica) handleShardIdGet(c echo.Context) error {
	var ids []string
	for shardId := range replica.shards {
		ids = append(ids, shardId)
	}
	return c.JSON(http.StatusOK, ShardIdsResponse{ShardIds: ids})
}

func (replica *Replica) handleShardNodeGet(c echo.Context) error {
	zap.L().Info("in handleShardNodeGet", zap.String("node-shard-id", replica.shardId))
	if replica.shardId != "" {
		return c.JSON(http.StatusOK, NodeIdResponse{NodeShardId: replica.shardId})
	}
	return c.JSON(http.StatusNotFound, ErrResponse{Error: "Shard Not Found (shouldn't happen)"})
}

func (replica *Replica) handleShardMembersGet(c echo.Context) error {
	shardId := c.Param("id")
	nodes, ok := replica.shards[shardId]
	if ok {
		return c.JSON(http.StatusOK, ShardMembersResponse{ShardMembers: nodes})
	}
	return c.JSON(http.StatusNotFound, ErrResponse{Error: "Shard ID does not exist"})
}

func (replica *Replica) handleShardKeyCount(c echo.Context) error {
	shardId := c.Param("id")

	if shardId == replica.shardId {
		return c.JSON(http.StatusOK, ShardKeyCountResponse{ShardKeyCount: len(replica.kv)})
	}

	shardNodes, shardExists := replica.shards[shardId]
	if !shardExists {
		return c.JSON(http.StatusNotFound, ErrResponse{Error: "Shard ID does not exist"})
	}

	resp, err := http.Get(fmt.Sprintf("http://%s/shard/key-count/%s", shardNodes[0], shardId))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrResponse{Error: "Request for key count failed"})
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrResponse{Error: "Couldn't read response from the node"})
	}

	var shardKeyCount ShardKeyCountResponse
	err = json.Unmarshal(body, &shardKeyCount)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrResponse{Error: "Couldn't unmarshal response data"})
	}

	return c.JSON(http.StatusOK, shardKeyCount)
}
