package main

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type CMRequest struct {
	CausalMetadata VectorClock `json:"causal-metadata"`
}

type CMResponse struct {
	StatusText string `json:"status-text"`
}

// handlePutCM updates the causal metadata of a replica. This handler should only
// be invoked when a write occurs to a replica in another shard, so that the causal
// metadata is propagated but not the actual kv data. We also don't need to broadcast
// this as it is called by the `BufferAtSender` function when writing to kvs.
func (r *Replica) handlePutCM(c echo.Context) error {
	request := new(CMRequest)

	if err := c.Bind(request); err != nil || request != nil {
		return c.JSON(http.StatusBadRequest, ErrResponse{Error: "must provide causal metadata"})
	}

	if !r.vc.IsReadyFor(request.CausalMetadata, false, &r.vcLock) {
		return c.JSON(http.StatusServiceUnavailable,
			ErrResponse{Error: "Causal Dependencies not satisfied; try again later"},
		)
	}
	r.vc.Accept(&request.CausalMetadata, false, &r.vcLock)

	return c.JSON(http.StatusOK, CMResponse{StatusText: "Updated vectorClock"})
}
