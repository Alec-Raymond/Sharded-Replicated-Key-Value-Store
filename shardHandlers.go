package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"
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

func (replica *Replica) handleShardMemberPut(c echo.Context) error {
	shardId := c.Param("id")

	var socket SocketAddress
	err := c.Bind(&socket)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrResponse{Error: "Bad Request"})
	}

	viewExists := false
	_, shardExists := replica.shards[shardId]

	// This doesn't work if the <IP:PORT> is the current replica's IP, but I'm not sure if it should
	for _, r := range replica.GetOtherViews() {
		if socket.Address == r {
			viewExists = true
		}
	}

	if !shardExists && !viewExists {
		return c.JSON(http.StatusNotFound, ErrResponse{Error: "View and Shard don't exist"})
	} else if !viewExists {
		return c.JSON(http.StatusNotFound, ErrResponse{Error: "View doesn't exist"})
	} else if !shardExists {
		return c.JSON(http.StatusNotFound, ErrResponse{Error: "Shard doesn't exist"})
	} else {
		replica.shards[shardId] = append(replica.shards[shardId], socket.Address)
		payload := map[string]string{
			"socket-address": socket.Address,
		}

		payloadJson, err := json.Marshal(payload)

		if err != nil {
			return c.JSON(http.StatusInternalServerError, ErrResponse{Error: "failed to marshal payload to json"})
		}
		go replica.BufferAtSender(&PollRequest{
			Method:      http.MethodPut,
			Payload:     payloadJson,
			Endpoint:    "/shard/add-member/" + shardId,
			ExcludeAddr: replica.addr,
			// Only other replicas will be broadcasted to
		})
		//TODO: Sync replica data with shard

		return c.JSON(http.StatusOK, ResponseNC{Result: "node added to shard"})
	}

}

func (replica *Replica) handleShardIdGet(c echo.Context) error {
	var ids []string
	for shardId := range replica.shards {
		ids = append(ids, shardId)
	}
	return c.JSON(http.StatusOK, ShardIdsResponse{ShardIds: ids})
}

func (replica *Replica) handleShardNodeGet(c echo.Context) error {
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
	shardNodes, shardExists := replica.shards[shardId]
	if !shardExists {
		return c.JSON(http.StatusNotFound, ErrResponse{Error: "Shard ID does not exist"})
	}
	resp, err := http.Get(fmt.Sprintf("http://%s/data", shardNodes[0]))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrResponse{Error: "Request to transfer data failed"})
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrResponse{Error: "Couldn't read response data"})
	}

	var data DataTransfer
	err = json.Unmarshal(body, &data)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrResponse{Error: "Couldn't unmarshal response data"})
	}
	return c.JSON(http.StatusOK, ShardKeyCountResponse{ShardKeyCount: len(data.Kv)})
}
