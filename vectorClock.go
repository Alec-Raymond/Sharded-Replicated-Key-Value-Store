package main

import (
	"maps"
	"sync"
)

type VectorClock struct {
	Clocks map[string]int
	Self   string
}

type ComparisonResult int64

const (
	Greater ComparisonResult = iota
	Concurrent
	Lesser
)

func (vc *VectorClock) IsReadyFor(clientClock VectorClock, isRead bool, vcLock *sync.Mutex) bool {
	// Returns true if I have satisfied all dependencies for the client clock.
	vcLock.Lock()
	defer vcLock.Unlock()
	if isRead {
		clk, present := vc.Clocks[clientClock.Self]
		clientSelfClock := clientClock.Clocks[clientClock.Self]

		return present && (clk >= clientSelfClock)
	}

	isReady := true

	// Check if I know about all clients this client knows about, I'm not ready if I don't.
	// Check if all my clock's entries are greater than or equal the client's corresponding ones apart from that of this client, who should only have one new write, namely, this one.

	for client, clientEntry := range clientClock.Clocks {
		nodeEntry, present := vc.Clocks[client]

		if client == clientClock.Self {
			isReady = isReady && nodeEntry == clientEntry
			continue
		}

		isReady = isReady && present && nodeEntry >= clientEntry
	}

	return isReady
}

func (vc *VectorClock) Compare(vc2 *VectorClock) ComparisonResult {
	// If all entries of vc >= vc2, and all clients in vc2 are in vc, vc > vc2.
	clients := getAllClients(vc.Clocks, vc2.Clocks)

	vcGreater := false
	vcLess := false

	for client := range clients {
		vcEntry := vc.Clocks[client]
		vc2Entry := vc2.Clocks[client]

		vcGreater = vcGreater && vcEntry >= vc2Entry
		vcLess = vcLess && vcEntry <= vc2Entry
	}

	if vcGreater && vcLess {
		return Concurrent
	} else if vcGreater {
		return Greater
	} else {
		return Lesser
	}
}

func (vc *VectorClock) Accept(clientClock *VectorClock, isRead bool, vcLock *sync.Mutex) {
	// Returns new clientClock (updates the value necessary).
	vcLock.Lock()
	defer vcLock.Unlock()

	if !isRead {
		vc.Clocks[clientClock.Self] = vc.Clocks[clientClock.Self] + 1

		clientClock.Clocks[clientClock.Self] = clientClock.Clocks[clientClock.Self] + 1
	}

	maps.Copy(clientClock.Clocks, vc.Clocks)
}

func GetClientVectorClock(request *Request, clientIP string) VectorClock {
	// fmt.Println("clientIP in getClientVectorClock:", clientIP)
	var clientClock VectorClock

	if len(request.CausalMetadata.Clocks) > 0 {
		clientClock = request.CausalMetadata
	} else {
		clientClock = VectorClock{Clocks: make(map[string]int), Self: clientIP}
		clientClock.Clocks[clientIP] = 0
	}

	return clientClock
}

func getAllClients(clocks ...map[string]int) map[string]struct{} {
	clients := make(map[string]struct{})
	for _, clock := range clocks {
		for client := range clock {
			clients[client] = struct{}{}
		}
	}

	return clients
}
