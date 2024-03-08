package main

/*
import "testing"

// Test IsReadyFor:
// IsReadyFor should compare the client clock with the server clock. If the client requires more than the server knows about (either in the individual clocks or in the knowledge of another client), the server is not ready for the update. Else, it is.

// Reads:
// 1. If the read requires clients the server doesn't know about => not ready.
func TestIsReadyForReadUnkClients(t *testing.T) {
	serverVc := &VectorClock{Self: "A", Clocks: make(map[string]int)}
	serverVc.Clocks["B"] = 10
	serverVc.Clocks["C"] = 12

	clientVc := VectorClock{Self: "B", Clocks: make(map[string]int)}
	clientVc.Clocks["B"] = 9
	clientVc.Clocks["C"] = 12
	clientVc.Clocks["D"] = 2

	if serverVc.IsReadyFor(clientVc, true) {
		t.Fatal("Unable to detect that the client knows clients the server doesn't know.")
	}
}

// 2. If the read requires prior writes the server doesn't know about => not ready.
func TestIsReadyForReadUnkWrites(t *testing.T) {
	serverVc := &VectorClock{Self: "A", Clocks: make(map[string]int)}
	serverVc.Clocks["B"] = 8
	serverVc.Clocks["C"] = 13

	clientVc := VectorClock{Self: "B", Clocks: make(map[string]int)}
	clientVc.Clocks["B"] = 9
	clientVc.Clocks["C"] = 12

	if serverVc.IsReadyFor(clientVc, true) {
		t.Fatal("Unable to detect that the client knows of a write the server doesn't know of.")
	}
}

// 3. Else => ready
func TestIsReadyForReadKnown(t *testing.T) {
	serverVc := &VectorClock{Self: "A", Clocks: make(map[string]int)}
	serverVc.Clocks["B"] = 10
	serverVc.Clocks["C"] = 12
	serverVc.Clocks["D"] = 2

	clientVc := VectorClock{Self: "B", Clocks: make(map[string]int)}
	clientVc.Clocks["B"] = 9
	clientVc.Clocks["C"] = 12

	if !serverVc.IsReadyFor(clientVc, true) {
		t.Fatal("Unable to detect that all dependencies are satisfied.")
	}
}

// Writes:

// 1. If there are prior writes the client knows of that the server doesn't => not ready.

func TestIsReadyForWriteUnkClients(t *testing.T) {
	serverVc := &VectorClock{Self: "A", Clocks: make(map[string]int)}
	serverVc.Clocks["B"] = 9
	serverVc.Clocks["C"] = 12

	clientVc := VectorClock{Self: "B", Clocks: make(map[string]int)}
	clientVc.Clocks["B"] = 8
	clientVc.Clocks["C"] = 12
	serverVc.Clocks["D"] = 2

	if serverVc.IsReadyFor(clientVc, false) {
		t.Fatal("Unable to detect that the client knows of other clients that the server doesn't yet.")
	}
}

// 2. If there are prior clients the client knows of that the server doesn't => not ready.

func TestIsReadyForWriteUnkWrites(t *testing.T) {
	serverVc := &VectorClock{Self: "A", Clocks: make(map[string]int)}
	serverVc.Clocks["B"] = 8
	serverVc.Clocks["C"] = 12

	clientVc := VectorClock{Self: "B", Clocks: make(map[string]int)}
	clientVc.Clocks["B"] = 9
	clientVc.Clocks["C"] = 12

	if serverVc.IsReadyFor(clientVc, false) {
		t.Fatal("Unable to detect that the client knows of writes that the server doesn't yet.")
	}
}

// 3. If the client knows of exactly only the writes I know of => ready

func TestIsReadyForWriteExact(t *testing.T) {
	serverVc := &VectorClock{Self: "A", Clocks: make(map[string]int)}
	serverVc.Clocks["B"] = 9
	serverVc.Clocks["C"] = 12
	serverVc.Clocks["D"] = 1

	clientVc := VectorClock{Self: "B", Clocks: make(map[string]int)}
	clientVc.Clocks["B"] = 9
	clientVc.Clocks["C"] = 12

	if serverVc.IsReadyFor(clientVc, false) {
		t.Fatal("Unable to detect that the client knows only of the writes the server also knows about.")
	}
}

// 4. If the client knows less than me on all other clients, but exactly as much as me on its own writes => ready

func TestIsReadyForWriteKnown(t *testing.T) {
	serverVc := &VectorClock{Self: "A", Clocks: make(map[string]int)}
	serverVc.Clocks["B"] = 8
	serverVc.Clocks["C"] = 12
	serverVc.Clocks["D"] = 1

	clientVc := VectorClock{Self: "B", Clocks: make(map[string]int)}
	clientVc.Clocks["B"] = 8
	clientVc.Clocks["C"] = 10
	serverVc.Clocks["D"] = 2

	if serverVc.IsReadyFor(clientVc, false) {
		t.Fatal("Unable to detect that the client knows of exactly the same writes of its own and less of others.")
	}
}

// 5. If we know more about the client than itself - an old message is still floating around => not ready

func TestIsReadyForWriteOld(t *testing.T) {
	serverVc := &VectorClock{Self: "A", Clocks: make(map[string]int)}
	serverVc.Clocks["B"] = 8
	serverVc.Clocks["C"] = 12

	clientVc := VectorClock{Self: "B", Clocks: make(map[string]int)}
	clientVc.Clocks["B"] = 7
	clientVc.Clocks["C"] = 12

	if serverVc.IsReadyFor(clientVc, false) {
		t.Fatal("Unable to detect that this is an old message.")
	}
}

// Test Accept
// Accept takes the client's clock and updates the replica's and the client's clocks assuming the client's operation is accepted.

// Read:
// 1. client should get the same clock as the server (transferred causal dependencies) and server should remain untouched (no new writes).
func TestAcceptRead(t *testing.T) {
	serverVc := &VectorClock{Self: "A", Clocks: make(map[string]int)}
	serverVc.Clocks["D"] = 10
	serverVc.Clocks["B"] = 5
	serverVc.Clocks["C"] = 10

	clientVc := &VectorClock{Self: "B", Clocks: make(map[string]int)}
	clientVc.Clocks["A"] = 9
	clientVc.Clocks["B"] = 5

	serverVc.Accept(clientVc, true)

	if serverVc.Clocks["D"] != 10 || serverVc.Clocks["B"] != 5 || serverVc.Clocks["C"] != 10 || len(serverVc.Clocks) != 3 || clientVc.Clocks["D"] != 10 || clientVc.Clocks["B"] != 5 || clientVc.Clocks["C"] != 10 || len(clientVc.Clocks) != 3 {
		t.Fatal("Failed to Accept Read Correctly. Post Read Clocks: clientVc.Clocks:", clientVc.Clocks, "serverVc.Clocks:", serverVc.Clocks)
	}
}

// Write:
// 1. client should get only its own entry incremented and the server should only have the client entry incremented.
func TestAcceptWrite(t *testing.T) {
	serverVc := &VectorClock{Self: "A", Clocks: make(map[string]int)}
	serverVc.Clocks["D"] = 10
	serverVc.Clocks["B"] = 5
	serverVc.Clocks["C"] = 10

	clientVc := &VectorClock{Self: "B", Clocks: make(map[string]int)}
	clientVc.Clocks["A"] = 9
	clientVc.Clocks["B"] = 5
	clientVc.Clocks["C"] = 10

	serverVc.Accept(clientVc, false)

	if serverVc.Clocks["D"] != 10 || serverVc.Clocks["B"] != 6 || serverVc.Clocks["C"] != 10 || len(serverVc.Clocks) != 3 || clientVc.Clocks["D"] != 10 || clientVc.Clocks["B"] != 6 || clientVc.Clocks["C"] != 10 || len(clientVc.Clocks) != 3 {
		t.Fatal("Failed to Accept Write Correctly. Post Write Clocks: clientVc.Clocks:", clientVc.Clocks, "serverVc.Clocks:", serverVc.Clocks)
	}

}
*/
