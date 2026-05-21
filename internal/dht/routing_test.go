package dht_test

import (
	"testing"

	"github.com/smartass08/aria2go/internal/dht"
)

func makeNodeID(b byte) dht.NodeID {
	var id dht.NodeID
	id[0] = b
	return id
}

func makeNodeInfo(id dht.NodeID, ip byte, port uint16) dht.NodeInfo {
	var nip [4]byte
	nip[0] = 127
	nip[1] = 0
	nip[2] = 0
	nip[3] = ip
	return dht.NodeInfo{ID: id, IP: nip, Port: port}
}

func TestNewRoutingTable(t *testing.T) {
	localID := makeNodeID(0x00)
	rt := dht.NewRoutingTable(localID)
	if rt.NodeCount() != 0 {
		t.Errorf("empty routing table should have 0 nodes, got %d", rt.NodeCount())
	}
	if rt.NumBuckets() != 1 {
		t.Errorf("expected 1 bucket, got %d", rt.NumBuckets())
	}
}

func TestAddNode_SameAsLocal(t *testing.T) {
	localID := makeNodeID(0x00)
	rt := dht.NewRoutingTable(localID)
	if rt.AddNode(makeNodeInfo(localID, 1, 6881)) {
		t.Error("should not add node with same ID as local")
	}
}

func TestAddNode_Simple(t *testing.T) {
	localID := makeNodeID(0x00)
	rt := dht.NewRoutingTable(localID)

	n := makeNodeInfo(makeNodeID(0x01), 1, 6881)
	if !rt.AddNode(n) {
		t.Error("should add node to empty bucket")
	}
	if rt.NodeCount() != 1 {
		t.Errorf("node count = %d, want 1", rt.NodeCount())
	}
}

func TestAddNode_Duplicate(t *testing.T) {
	localID := makeNodeID(0x00)
	rt := dht.NewRoutingTable(localID)

	n := makeNodeInfo(makeNodeID(0x01), 1, 6881)
	rt.AddNode(n)

	n2 := makeNodeInfo(makeNodeID(0x01), 1, 6881)
	if !rt.AddNode(n2) {
		t.Error("should re-add duplicate node (moves to tail)")
	}
	if rt.NodeCount() != 1 {
		t.Errorf("duplicate should not increase count, got %d", rt.NodeCount())
	}
}

func TestAddNode_FillBucket(t *testing.T) {
	localID := makeNodeID(0xFF)
	rt := dht.NewRoutingTable(localID)

	for i := byte(0); i < 8; i++ {
		n := makeNodeInfo(makeNodeID(i), i+1, 6881)
		if !rt.AddNode(n) {
			t.Errorf("failed to add node %d", i)
		}
	}
	if rt.NodeCount() != 8 {
		t.Errorf("count = %d, want 8", rt.NodeCount())
	}
}

func TestAddNode_BucketFull_NoSplit(t *testing.T) {
	// Local ID 0xFF in right half [0x80-0xFF], all test nodes in left half [0x00-0x7F]
	// After first split, left bucket holds test nodes (full), right holds local.
	// Since local is not in left bucket range, splitAllowed returns false.
	localID := makeNodeID(0xFF)
	rt := dht.NewRoutingTable(localID)

	for i := byte(1); i <= 8; i++ {
		n := makeNodeInfo(makeNodeID(i), i, 6881)
		if !rt.AddNode(n) {
			t.Errorf("failed to add node %d", i)
		}
	}
	if rt.NodeCount() != 8 {
		t.Fatalf("count = %d, want 8", rt.NodeCount())
	}

	// 9th node also in left half — bucket full, can't split, no bad nodes
	if rt.AddNode(makeNodeInfo(makeNodeID(9), 9, 6881)) {
		t.Error("should not add node beyond K when no bad nodes and local not in range")
	}
}

func TestAddNode_BucketFull_ReplaceBad(t *testing.T) {
	localID := makeNodeID(0xFF)
	rt := dht.NewRoutingTable(localID)

	nodes := make([]dht.NodeInfo, 8)
	for i := byte(0); i < 8; i++ {
		nodes[i] = makeNodeInfo(makeNodeID(i), i+1, 6881)
		rt.AddNode(nodes[i])
	}
	if rt.NodeCount() != 8 {
		t.Fatalf("count = %d, want 8", rt.NodeCount())
	}

	newNode := makeNodeInfo(makeNodeID(0x55), 100, 9999)
	if rt.AddNode(newNode) {
		t.Error("should not add to full bucket with all good nodes")
	}

	rt.RemoveNode(nodes[0].ID)

	rt.AddNode(newNode)
	if rt.NodeCount() != 8 {
		t.Errorf("count = %d, want 8 after replacement", rt.NodeCount())
	}
}

func TestAddNode_BucketSplitting(t *testing.T) {
	// Local at 0x80. First 4 nodes go to left half, 4 to right half.
	// After first split, both halves have 4 nodes (less than K=8).
	// The 9th node is added to whichever half it falls into.
	localID := makeNodeID(0x80)
	rt := dht.NewRoutingTable(localID)

	for i := byte(0); i < 4; i++ {
		rt.AddNode(makeNodeInfo(makeNodeID(i), i+1, 6881))
	}
	for i := byte(0); i < 4; i++ {
		rt.AddNode(makeNodeInfo(makeNodeID(0xFC+i), i+5, 6881))
	}
	if rt.NodeCount() != 8 {
		t.Fatalf("count = %d, want 8 before split test", rt.NodeCount())
	}

	// This triggers first split: left [0x00-0x7F] has 4 nodes, right [0x80-0xFF] has 4 nodes
	// New node 0x04 falls in left half — left has 4 nodes < K so it fits
	if !rt.AddNode(makeNodeInfo(makeNodeID(4), 10, 6881)) {
		t.Error("should add node after bucket split")
	}
	if rt.NodeCount() != 9 {
		t.Errorf("node count = %d, want 9 after split", rt.NodeCount())
	}
	if rt.NumBuckets() <= 1 {
		t.Error("bucket should have split")
	}
}

func TestRemoveNode(t *testing.T) {
	localID := makeNodeID(0x00)
	rt := dht.NewRoutingTable(localID)
	n := makeNodeInfo(makeNodeID(0x01), 1, 6881)
	rt.AddNode(n)

	if rt.NodeCount() != 1 {
		t.Fatalf("count = %d, want 1", rt.NodeCount())
	}

	rt.RemoveNode(n.ID)
	if rt.NodeCount() != 0 {
		t.Errorf("count = %d, want 0 after remove", rt.NodeCount())
	}
}

func TestGetClosestNodes_Empty(t *testing.T) {
	localID := makeNodeID(0x00)
	rt := dht.NewRoutingTable(localID)

	nodes := rt.GetClosestNodes(makeNodeID(0xFF), 8)
	if len(nodes) != 0 {
		t.Errorf("empty table should return 0 nodes, got %d", len(nodes))
	}
}

func TestGetClosestNodes_Basic(t *testing.T) {
	localID := makeNodeID(0x00)
	rt := dht.NewRoutingTable(localID)

	for i := byte(1); i <= 8; i++ {
		rt.AddNode(makeNodeInfo(makeNodeID(i), i, 6881))
	}

	closest := rt.GetClosestNodes(makeNodeID(0x01), 4)
	if len(closest) != 4 {
		t.Errorf("len = %d, want 4", len(closest))
	}
}

func TestGetClosestNodes_Count(t *testing.T) {
	localID := makeNodeID(0x00)
	rt := dht.NewRoutingTable(localID)

	for i := byte(1); i <= 5; i++ {
		rt.AddNode(makeNodeInfo(makeNodeID(i), i, 6881))
	}

	closest := rt.GetClosestNodes(makeNodeID(0xFF), 10)
	if len(closest) != 5 {
		t.Errorf("len = %d, want 5 (less than count param)", len(closest))
	}
}

func TestNodeCount(t *testing.T) {
	localID := makeNodeID(0x00)
	rt := dht.NewRoutingTable(localID)

	if rt.NodeCount() != 0 {
		t.Errorf("count = %d, want 0", rt.NodeCount())
	}

	rt.AddNode(makeNodeInfo(makeNodeID(1), 1, 6881))
	rt.AddNode(makeNodeInfo(makeNodeID(2), 2, 6882))

	if rt.NodeCount() != 2 {
		t.Errorf("count = %d, want 2", rt.NodeCount())
	}
}

func TestRandomNodeID(t *testing.T) {
	id := dht.RandomNodeID()
	if len(id) != 20 {
		t.Errorf("RandomNodeID length = %d, want 20", len(id))
	}
	var zero dht.NodeID
	if id == zero {
		t.Log("random node ID is all zeros (statistically unlikely)")
	}
}

func TestXORDistance_Basic(t *testing.T) {
	a := makeNodeID(0x00)
	b := makeNodeID(0x01)
	d := dht.XORDistance(a, b)
	if d.BitLen() == 0 && a == b {
		t.Error("XORDistance should be non-zero for different IDs")
	}
	d2 := dht.XORDistance(a, a)
	if d2.Sign() != 0 {
		t.Error("XORDistance of identical IDs should be 0")
	}
}

func TestRoutingTable_AddGoodNode(t *testing.T) {
	localID := makeNodeID(0x00)
	rt := dht.NewRoutingTable(localID)

	for i := byte(1); i < 10; i++ {
		n := makeNodeInfo(makeNodeID(i), i, 6881)
		if i <= 8 {
			if !rt.AddNode(n) {
				t.Errorf("failed to add node %d", i)
			}
		} else {
			if !rt.AddGoodNode(n) {
				t.Errorf("AddGoodNode should cache when bucket full, got false for node %d", i)
			}
		}
	}
}

func TestRoutingTable_Normalize(t *testing.T) {
	// Local at 0x80. All test nodes in left half [0x00-0x7F].
	// After first split, left bucket has all test nodes. Local is not
	// in left range so the left bucket can't split further.
	// K=8 nodes max in left bucket; 9th node can't be added.
	localID := makeNodeID(0x80)
	rt := dht.NewRoutingTable(localID)

	for i := range 10 {
		id := makeNodeID(byte(i))
		n := makeNodeInfo(id, byte(i+1), 6881)
		rt.AddNode(n)
	}

	// Node 0x80 == local is rejected, 9 others added but only K=8 fit
	if rt.NodeCount() != 8 {
		t.Errorf("count = %d, want 8 (K capacity)", rt.NodeCount())
	}
}

func TestRoutingTable_AddTwiceDifferentAddresses(t *testing.T) {
	localID := makeNodeID(0x00)
	rt := dht.NewRoutingTable(localID)

	id := makeNodeID(0x01)
	n1 := makeNodeInfo(id, 1, 6881)
	n2 := makeNodeInfo(id, 2, 6882)

	rt.AddNode(n1)
	rt.AddNode(n2)

	if rt.NodeCount() != 1 {
		t.Error("same ID with different addresses should be treated as same node")
	}
}

func TestRoutingTable_LargeTable(t *testing.T) {
	localID := makeNodeID(0x00)
	rt := dht.NewRoutingTable(localID)

	for i := range 50 {
		id := makeNodeID(byte(i + 1))
		n := makeNodeInfo(id, byte((i%254)+1), uint16(6881+(i%1000)))
		rt.AddNode(n)
	}
}

func TestRoutingTable_GetAfterMultipleSplits(t *testing.T) {
	localID := dht.ZeroNodeID()
	rt := dht.NewRoutingTable(localID)

	for i := range 30 {
		b := byte(0xFF - i)
		rt.AddNode(makeNodeInfo(makeNodeID(b), byte(i+1), 6881))
	}

	closest := rt.GetClosestNodes(makeNodeID(0xFF), 8)
	if len(closest) > 8 {
		t.Errorf("GetClosestNodes returned %d nodes, max should be 8", len(closest))
	}
	if len(closest) < 8 && rt.NodeCount() >= 8 {
		t.Errorf("GetClosestNodes returned only %d nodes from %d total", len(closest), rt.NodeCount())
	}
}
