package main

import (
	"encoding/json"
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
	shard_id := c.Param("id")

	var socket SocketAddress
	err := c.Bind(&socket)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrResponse{Error: "Bad Request"})
	}

	shardIndex := -1
	viewExists := false

	for i, s := range replica.shardList {
		if s.id == shard_id {
			shardIndex = i
		}
	}

	// This doesn't work if the <IP:PORT> is the current replica's IP, but I'm not sure if it should
	for _, r := range replica.GetOtherViews() {
		if socket.Address == r {
			viewExists = true
		}
	}

	if shardIndex == -1 && !viewExists {
		return c.JSON(http.StatusNotFound, ErrResponse{Error: "View and Shard don't exist"})
	} else if !viewExists {
		return c.JSON(http.StatusNotFound, ErrResponse{Error: "View doesn't exist"})
	} else if shardIndex == -1 {
		return c.JSON(http.StatusNotFound, ErrResponse{Error: "Shard doesn't exist"})
	} else {
		replica.shardList[shardIndex].nodeAddrList = append(replica.shardList[shardIndex].nodeAddrList, socket.Address)
		payload := map[string]string{
			"socket-address": socket.Address,
		}

		payloadJson, err := json.Marshal(payload)

		if err != nil {
			return c.JSON(http.StatusInternalServerError, ErrResponse{Error: "failed to marshal payload to json"})
		}
		go replica.BufferAtSender(&PollRequest{
			Method:      "PUT",
			Payload:     payloadJson,
			Endpoint:    "/socket",
			ExcludeAddr: replica.addr,
			// Only other replicas will be broadcasted to
		})
		//TODO: Sync replica data with shard

		return c.JSON(http.StatusOK, ResponseNC{Result: "node added to shard"})
	}

}

func (replica *Replica) handleShardIdGet(c echo.Context) error {
	var ids []string
	for _, s := range replica.shardList {
		ids = append(ids, s.id)
	}
	return c.JSON(http.StatusOK, ShardIdsResponse{ShardIds: ids})
}

func (replica *Replica) handleShardNodeGet(c echo.Context) error {
	for _, s := range replica.shardList {
		for _, address := range s.nodeAddrList {
			if address == replica.addr {
				return c.JSON(http.StatusOK, NodeIdResponse{NodeShardId: s.id})
			}
		}
	}
	return c.JSON(http.StatusNotFound, ErrResponse{Error: "Shard Not Found (shouldn't happen)"})
}

func (replica *Replica) handleShardMembersGet(c echo.Context) error {
	shard_id := c.Param("id")
	for _, s := range replica.shardList {
		if s.id == shard_id {
			return c.JSON(http.StatusOK, ShardMembersResponse{ShardMembers: s.nodeAddrList})
		}
	}
	return c.JSON(http.StatusNotFound, ErrResponse{Error: "Shard ID does not exist"})
}

func (replica *Replica) handleShardKeyCount(c echo.Context) error {
	shard_id := c.Param("id")
	for _, s := range replica.shardList {
		if s.id == shard_id {
			resp, err := http.Get("http://" + s.nodeAddrList[0] + "/data")
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
			return c.JSON(http.StatusOK, ShardKeyCountResponse{ShardKeyCount: len(data.Kv)})
		}
	}
	return c.JSON(http.StatusNotFound, ErrResponse{Error: "Shard ID does not exist"})
}
