package main

type ResponseNC struct {
	Result string `json:"result"`
}

type ErrResponse struct {
	Error string `json:"error"`
}

type SocketAddress struct {
	Address     string `json:"socket-address"`
	IsBroadcast bool   `json:"is-broadcast,omitempty"`
}
