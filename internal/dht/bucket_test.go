package dht

import (
	"net"
	"testing"

	"github.com/smartass08/aria2go/internal/bencode"
)

func testNodeID(b byte) NodeID {
	var id NodeID
	id[0] = b
	return id
}

func testRoutingNode(b byte, ip byte, port uint16) *routingNode {
	rn := newRoutingNode(NodeInfo{ID: testNodeID(b), IP: [4]byte{192, 0, 2, ip}, Port: port})
	rn.markGood()
	return rn
}

func TestBucket_IsInRange_RootBucket(t *testing.T) {
	local := ZeroNodeID()
	b := newBucket(local)

	allZero := ZeroNodeID()
	if !b.inRange(allZero) {
		t.Error("root bucket should include zero ID")
	}

	var allFF NodeID
	for i := range allFF {
		allFF[i] = 0xff
	}
	if !b.inRange(allFF) {
		t.Error("root bucket should include all-FF ID")
	}

	mid := ZeroNodeID()
	mid[0] = 0x80
	if !b.inRange(mid) {
		t.Error("root bucket should include mid-range ID")
	}
}

func TestBucket_IsInRange_SubRange(t *testing.T) {
	local := ZeroNodeID()
	// Range [0x0101_00..., 0x0101_ff...], prefixLen=16
	var maxID, minID NodeID
	minID[0], minID[1] = 0x01, 0x01
	maxID[0], maxID[1] = 0x01, 0x01
	for i := 2; i < NodeIDLength; i++ {
		maxID[i] = 0xff
	}
	b := newBucketWithRange(16, maxID, minID, local)

	// min boundary
	min := minID
	if !b.inRange(min) {
		t.Error("min ID should be in range")
	}

	// max boundary
	max := maxID
	if !b.inRange(max) {
		t.Error("max ID should be in range")
	}

	// middle
	mid := minID
	mid[10] = 0x80
	if !b.inRange(mid) {
		t.Error("mid ID should be in range")
	}

	// below range
	below := minID
	below[0] = 0x00
	if b.inRange(below) {
		t.Error("ID below min should be out of range")
	}

	// above range
	above := maxID
	above[1] = 0x02
	if b.inRange(above) {
		t.Error("ID above max should be out of range")
	}
}

func TestBucket_SplitAllowed(t *testing.T) {
	// Root bucket: local is inside, prefixLen=0 < 159, splitAllowed=true
	local := ZeroNodeID()
	b := newBucket(local)
	if !b.splitAllowed() {
		t.Error("root bucket should allow split (local in range, prefixLen < 159)")
	}

	// Sub-range where local is NOT in range
	var maxID, minID NodeID
	minID[0] = 0xe0
	for i := range maxID {
		maxID[i] = 0xff
	}
	// local=0x00 not in [0xe0, 0xff]
	b2 := newBucketWithRange(3, maxID, minID, local)
	if b2.splitAllowed() {
		t.Error("bucket without local should not allow split")
	}

	// Same range but local IS in range
	local2 := ZeroNodeID()
	local2[0] = 0xe0
	local2[NodeIDLength-1] = 0x01
	b3 := newBucketWithRange(3, maxID, minID, local2)
	if !b3.splitAllowed() {
		t.Error("bucket with local in range should allow split")
	}
}

func TestBucket_Split(t *testing.T) {
	local := ZeroNodeID()
	b := newBucket(local)

	r := b.split()

	// After split: prefixLen should be 1 for both
	if b.prefixLen != 1 {
		t.Errorf("left bucket prefixLen = %d, want 1", b.prefixLen)
	}
	if r.prefixLen != 1 {
		t.Errorf("right bucket prefixLen = %d, want 1", r.prefixLen)
	}

	// Right bucket should be [0x00..00, 0x7f..ff]
	expectedRMax := ZeroNodeID()
	expectedRMax[0] = 0x7f
	for i := 1; i < NodeIDLength; i++ {
		expectedRMax[i] = 0xff
	}
	expectedRMin := ZeroNodeID()
	if r.maxID != expectedRMax {
		t.Errorf("right max = %x, want %x", r.maxID, expectedRMax)
	}
	if r.minID != expectedRMin {
		t.Errorf("right min = %x, want zeros", r.minID)
	}

	// Left bucket should be [0x80..00, 0xff..ff]
	expectedLMin := ZeroNodeID()
	expectedLMin[0] = 0x80
	expectedLMax := ZeroNodeID()
	for i := range expectedLMax {
		expectedLMax[i] = 0xff
	}
	if b.minID != expectedLMin {
		t.Errorf("left min = %x, want %x", b.minID, expectedLMin)
	}
	if b.maxID != expectedLMax {
		t.Errorf("left max = %x, want all FF", b.maxID)
	}

	// Max splits is 159
	b2 := newBucket(local)
	for i := 0; i < 159; i++ {
		if !b2.splitAllowed() {
			t.Fatalf("should allow split at iteration %d", i)
		}
		b2 = b2.split()
	}
	if b2.splitAllowed() {
		t.Error("bucket at prefixLen=159 should not allow split")
	}
	if b2.prefixLen != 159 {
		t.Errorf("prefixLen after 159 splits = %d, want 159", b2.prefixLen)
	}
	// At prefixLen=159, only the last bit of the last byte differs
	expectedMax := ZeroNodeID()
	expectedMax[NodeIDLength-1] = 0x01
	if b2.maxID != expectedMax {
		t.Errorf("deeply split max = %x, want last byte 0x01", b2.maxID)
	}
}

func TestBucket_AddNode_MoveToTail(t *testing.T) {
	local := ZeroNodeID()
	b := newBucket(local)

	nodes := make([]*routingNode, 4)
	for i := range 4 {
		nodes[i] = testRoutingNode(byte(0xf0+i), byte(i+1), uint16(6881+i))
		b.addNode(nodes[i])
	}

	if b.nodes[0] != nodes[0] {
		t.Error("first added node should be at head")
	}
	if b.nodes[3] != nodes[3] {
		t.Error("last added node should be at tail")
	}

	// Re-adding nodes[0] should move it to tail (duplicate add moves to tail)
	b.addNode(nodes[0])
	if b.nodes[3] != nodes[0] {
		t.Error("re-added node should move to tail")
	}
	if len(b.nodes) != 4 {
		t.Errorf("re-adding should not increase count, got %d", len(b.nodes))
	}
}

func TestBucket_AddNode_ReplaceBad(t *testing.T) {
	local := ZeroNodeID()
	b := newBucket(local)

	nodes := make([]*routingNode, bucketK)
	for i := range bucketK {
		nodes[i] = testRoutingNode(byte(0xf0+i), byte(i+1), uint16(6881+i))
		b.addNode(nodes[i])
	}

	if len(b.nodes) != bucketK {
		t.Fatalf("bucket size = %d, want %d", len(b.nodes), bucketK)
	}

	// Adding to full bucket with all good nodes should fail
	newNode := testRoutingNode(0xf8, 100, 9999)
	if b.addNode(newNode) {
		t.Error("should not add to full bucket with all good nodes")
	}

	// Mark head (oldest) as bad, then add should succeed
	nodes[0].markBad()
	if !b.addNode(newNode) {
		t.Error("should replace bad head node")
	}
	if b.nodes[bucketK-1] != newNode {
		t.Error("replacement node should be at tail")
	}
	if len(b.nodes) != bucketK {
		t.Errorf("size after replacement = %d, want %d", len(b.nodes), bucketK)
	}
}

func TestBucket_CacheNode(t *testing.T) {
	local := ZeroNodeID()
	b := newBucket(local)

	n1 := testRoutingNode(0x01, 1, 6881)
	n2 := testRoutingNode(0x02, 2, 6882)
	n3 := testRoutingNode(0x03, 3, 6883)

	b.cacheNode(n1)
	b.cacheNode(n2)
	if len(b.cache) != 2 {
		t.Errorf("cache size = %d, want 2", len(b.cache))
	}
	if b.cache[0] != n2 {
		t.Error("most recently cached node should be at front")
	}

	// Third cache pushes oldest (n1) out
	b.cacheNode(n3)
	if len(b.cache) != 2 {
		t.Errorf("cache size after 3rd add = %d, want 2 (CACHE_MAX=2)", len(b.cache))
	}
	if b.cache[0] != n3 {
		t.Error("n3 should be at cache front")
	}
	if b.cache[1] != n2 {
		t.Error("n2 should remain in cache")
	}
}

func TestBucket_DropNode_NoCache(t *testing.T) {
	local := ZeroNodeID()
	b := newBucket(local)

	nodes := make([]*routingNode, 4)
	for i := range 4 {
		nodes[i] = testRoutingNode(byte(0xf0+i), byte(i+1), uint16(6881+i))
		b.addNode(nodes[i])
	}

	b.dropNode(nodes[1])
	if len(b.nodes) != 3 {
		t.Errorf("size after drop = %d, want 3", len(b.nodes))
	}

	// Verify nodes[1] is removed
	for _, n := range b.nodes {
		if n.id == nodes[1].id {
			t.Error("dropped node should not be in bucket")
		}
	}
}

func TestBucket_DropNode_WithCache(t *testing.T) {
	local := ZeroNodeID()
	b := newBucket(local)

	nodes := make([]*routingNode, 4)
	for i := range 4 {
		nodes[i] = testRoutingNode(byte(0xf0+i), byte(i+1), uint16(6881+i))
		b.addNode(nodes[i])
	}

	cached1 := testRoutingNode(0x10, 10, 1000)
	cached2 := testRoutingNode(0x20, 20, 2000)
	b.cacheNode(cached1)
	b.cacheNode(cached2)

	b.dropNode(nodes[1])
	if len(b.nodes) != 4 {
		t.Errorf("size after drop with cache = %d, want 4 (replaced from cache)", len(b.nodes))
	}

	// Last node should be cached2 (front of cache before drop)
	if b.nodes[3].id != cached2.id {
		t.Errorf("replacement node id = %x, want cached2 (%x)", b.nodes[3].id, cached2.id)
	}
	if len(b.cache) != 1 {
		t.Errorf("cache size after consume = %d, want 1", len(b.cache))
	}
}

func TestBucket_GoodNodes(t *testing.T) {
	local := ZeroNodeID()
	b := newBucket(local)

	nodes := make([]*routingNode, 6)
	for i := range 6 {
		nodes[i] = testRoutingNode(byte(0xf0+i), byte(i+1), uint16(6881+i))
		b.addNode(nodes[i])
	}

	// Mark some as bad
	nodes[1].markBad()
	nodes[3].markBad()

	good := b.goodNodes()
	if len(good) != 4 {
		t.Errorf("good nodes = %d, want 4", len(good))
	}
	// Check that bad nodes are excluded
	for _, gn := range good {
		if gn.isBad() {
			t.Error("goodNodes should not include bad nodes")
		}
	}
	// Order should be preserved
	expectedPorts := []uint16{6881, 6883, 6885, 6886}
	for i, gn := range good {
		if gn.port != expectedPorts[i] {
			t.Errorf("good[%d] port = %d, want %d", i, gn.port, expectedPorts[i])
		}
	}
}

func TestBucket_GetRandomNodeID(t *testing.T) {
	local := ZeroNodeID()

	// Root bucket: random ID can be anything
	{
		b := newBucket(local)
		id := b.randomNodeID()
		if id == ZeroNodeID() {
			t.Log("random ID from root bucket is all zeros (unlikely)")
		}
	}

	// Sub-range bucket: prefix bytes must match minID
	{
		var maxID, minID NodeID
		minID[0], minID[1] = 0x01, 0x01
		for i := 2; i < NodeIDLength; i++ {
			maxID[i] = 0xff
		}
		b := newBucketWithRange(16, maxID, minID, local)

		for range 10 {
			id := b.randomNodeID()
			// First 2 bytes (16 bits) must match
			if id[0] != 0x01 || id[1] != 0x01 {
				t.Errorf("randomNodeID prefix = %02x%02x, want 0101", id[0], id[1])
			}
		}
	}

	// PrefixLen 0 means random
	{
		b := newBucket(local)
		id := b.randomNodeID()
		if !b.inRange(id) {
			t.Error("randomNodeID from root bucket should be in range")
		}
	}
}

func TestBucket_NeedsRefresh(t *testing.T) {
	local := ZeroNodeID()

	// New bucket with no nodes needs refresh
	b := newBucket(local)
	if !b.needsRefresh() {
		t.Error("new empty bucket should need refresh")
	}

	// Bucket with K=8 nodes is full and doesn't need immediate refresh
	for i := range bucketK {
		b.addNode(testRoutingNode(byte(0xf0+i), byte(i+1), uint16(6881+i)))
	}
	if b.needsRefresh() {
		t.Error("full bucket with recent update should not need refresh")
	}
}

func TestBucketTree_Dig(t *testing.T) {
	local := ZeroNodeID()
	for i := range local {
		local[i] = 0xff
	}

	root := newBucket(local)
	b2 := root.split()
	b3 := root.split()

	// Tree:
	//        +
	//   +----+----+
	//  b2          |
	//   0     +----+----+
	//        b3         root
	//        10         11
	//                   |
	//              localNode here

	// Root-only tree: dig returns nil (leaf node)
	leaf := newBucketTreeLeaf(b3)
	if leaf.dig(local) != nil {
		t.Error("dig on leaf should return nil")
	}

	// Two-level tree
	left := newBucketTreeLeaf(b2)
	right := newBucketTreeLeaf(root)
	inner := &bucketTreeNode{
		left:  left,
		right: right,
		minID: b2.minID,
		maxID: root.maxID,
	}

	// local=0xff..ff is in right (bucket with root)
	digResult := inner.dig(local)
	if digResult != right {
		t.Error("dig(local) should return right child")
	}
}

func TestBucketTree_FindTreeNode(t *testing.T) {
	local := ZeroNodeID()
	local[0] = 0xaa

	root := newBucket(local)
	b2 := root.split()
	b3 := root.split()
	b4 := b3.split()
	b5 := b3.split()

	// Tree:
	//           +
	//    +------+------+
	//   b2             |
	//   0       +------+------+
	//           |             b1(root)
	//     +-----+-----+      11
	//    b4           |
	//   100     +-----+-----+
	//          b5           b3
	//          1010         1011
	//           |
	//    localNode is here

	leaf5 := newBucketTreeLeaf(b5)
	leaf3 := newBucketTreeLeaf(b3)
	leaf4 := newBucketTreeLeaf(b4)
	leaf1 := newBucketTreeLeaf(root)
	leaf2 := newBucketTreeLeaf(b2)

	innerP1 := &bucketTreeNode{left: leaf5, right: leaf3, minID: b5.minID, maxID: b3.maxID}
	leaf5.parent = innerP1
	leaf3.parent = innerP1

	innerP2 := &bucketTreeNode{left: leaf4, right: innerP1, minID: b4.minID, maxID: innerP1.maxID}
	leaf4.parent = innerP2
	innerP1.parent = innerP2

	innerP3 := &bucketTreeNode{left: innerP2, right: leaf1, minID: innerP2.minID, maxID: leaf1.maxID}
	innerP2.parent = innerP3
	leaf1.parent = innerP3

	treeRoot := &bucketTreeNode{left: leaf2, right: innerP3, minID: leaf2.minID, maxID: innerP3.maxID}
	leaf2.parent = treeRoot
	innerP3.parent = treeRoot

	target := testNodeID(0x01)
	found := findTreeNode(treeRoot, target)
	if found != leaf2 {
		t.Error("findTreeNode(target=0x01...) should find leaf2")
	}

	found = findTreeNode(treeRoot, local)
	if found != leaf5 {
		t.Error("findTreeNode(local) should find leaf5")
	}

	targetFF := testNodeID(0xff)
	found = findTreeNode(treeRoot, targetFF)
	if found != leaf1 {
		t.Error("findTreeNode(target=0xff...) should find leaf1")
	}
}

func TestBucketTree_FindClosestKNodes(t *testing.T) {
	local := ZeroNodeID()
	local[0] = 0xaa

	root := newBucket(local)
	b2 := root.split()
	b3 := root.split()
	b4 := b3.split()
	b5 := b3.split()

	// Same tree as above, with parent pointers
	leaf5 := newBucketTreeLeaf(b5)
	leaf3 := newBucketTreeLeaf(b3)
	leaf4 := newBucketTreeLeaf(b4)
	leaf1 := newBucketTreeLeaf(root)
	leaf2 := newBucketTreeLeaf(b2)

	innerP1 := &bucketTreeNode{left: leaf5, right: leaf3, minID: b5.minID, maxID: b3.maxID}
	leaf5.parent = innerP1
	leaf3.parent = innerP1

	innerP2 := &bucketTreeNode{left: leaf4, right: innerP1, minID: b4.minID, maxID: innerP1.maxID}
	leaf4.parent = innerP2
	innerP1.parent = innerP2

	innerP3 := &bucketTreeNode{left: innerP2, right: leaf1, minID: innerP2.minID, maxID: leaf1.maxID}
	innerP2.parent = innerP3
	leaf1.parent = innerP3

	treeRoot := &bucketTreeNode{left: leaf2, right: innerP3, minID: leaf2.minID, maxID: innerP3.maxID}
	leaf2.parent = treeRoot
	innerP3.parent = treeRoot

	// Add 2 unique good nodes to each bucket using full random IDs
	addTwoNodes := func(b *bucket, basePort uint16) {
		for j := byte(0); j < 2; j++ {
			id := b.randomNodeID()
			id[NodeIDLength-1] ^= byte(j + 1)
			rn := newRoutingNode(NodeInfo{ID: id, IP: [4]byte{192, 0, 2, byte(j + 1)}, Port: basePort + uint16(j)})
			rn.markGood()
			b.addNode(rn)
		}
	}
	addTwoNodes(leaf2.b, 8001)
	addTwoNodes(leaf4.b, 8003)
	addTwoNodes(leaf5.b, 8005)
	addTwoNodes(leaf3.b, 8007)
	addTwoNodes(leaf1.b, 8009)

	// Target 0x80: closest should be b4 (100-prefix) then b5 (1010), then b3 (1011), then b1 (11)
	target := testNodeID(0x80)
	nodes := findClosestKNodes(treeRoot, target)
	if len(nodes) != bucketK {
		t.Errorf("findClosestKNodes count = %d, want %d", len(nodes), bucketK)
	}

	// First 2 should be from b4 (closest to 0x80)
	if !b4.inRange(nodes[0].id) {
		t.Error("closest node should be from b4 range")
	}
	if !b4.inRange(nodes[1].id) {
		t.Error("2nd closest should be from b4 range")
	}

	// With target 0xf0, closest is b1 (11-prefix)
	targetF0 := testNodeID(0xf0)
	nodesF0 := findClosestKNodes(treeRoot, targetF0)
	if len(nodesF0) != bucketK {
		t.Errorf("findClosestKNodes(f0) count = %d", len(nodesF0))
	}
	if !leaf1.b.inRange(nodesF0[0].id) {
		t.Error("closest to 0xf0 should be from root/rightmost bucket")
	}
}

func TestBucketTree_EnumerateBuckets(t *testing.T) {
	local := ZeroNodeID()
	local[0] = 0xaa

	root := newBucket(local)
	b2 := root.split()
	b3 := root.split()
	b4 := b3.split()
	b5 := b3.split()

	// Single leaf
	buckets := enumerateBuckets(newBucketTreeLeaf(b5))
	if len(buckets) != 1 {
		t.Errorf("enumerate single leaf = %d, want 1", len(buckets))
	}
	if buckets[0] != b5 {
		t.Error("single leaf should enumerate self")
	}

	// Full tree (left-first traversal)
	leaf5 := newBucketTreeLeaf(b5)
	leaf3 := newBucketTreeLeaf(b3)
	leaf4 := newBucketTreeLeaf(b4)
	leaf1 := newBucketTreeLeaf(root)
	leaf2 := newBucketTreeLeaf(b2)

	innerP1 := &bucketTreeNode{left: leaf5, right: leaf3, minID: b5.minID, maxID: b3.maxID}
	innerP2 := &bucketTreeNode{left: leaf4, right: innerP1, minID: b4.minID, maxID: innerP1.maxID}
	innerP3 := &bucketTreeNode{left: innerP2, right: leaf1, minID: innerP2.minID, maxID: leaf1.maxID}
	treeRoot := &bucketTreeNode{left: leaf2, right: innerP3, minID: leaf2.minID, maxID: innerP3.maxID}

	buckets = enumerateBuckets(treeRoot)
	if len(buckets) != 5 {
		t.Errorf("enumerate full tree = %d, want 5", len(buckets))
	}

	expectedOrder := []*bucket{b2, b4, b5, b3, root}
	for i, b := range buckets {
		if b != expectedOrder[i] {
			t.Errorf("bucket[%d] range prefixLen %d, want range for expected[%d]", i, b.prefixLen, i)
		}
	}
}

func TestBucketTree_RoundTripSplitAndFind(t *testing.T) {
	local := testNodeID(0x42)

	rt := NewRoutingTable(local)
	nodes := make([]NodeInfo, 20)
	for i := range 20 {
		nodes[i] = NodeInfo{
			ID:   testNodeID(byte(0x80 + i)),
			IP:   [4]byte{192, 0, 2, byte(i + 1)},
			Port: uint16(6881 + i),
		}
		rt.AddNode(nodes[i])
	}

	// After splits, should be able to find closest nodes
	closest := rt.GetClosestNodes(testNodeID(0xff), bucketK)
	if len(closest) > bucketK {
		t.Errorf("GetClosestNodes returned %d nodes, max %d", len(closest), bucketK)
	}

	// All nodes should be from the routing table
	for _, c := range closest {
		found := false
		for _, n := range nodes {
			if c.ID == n.ID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("closest node %x not in original set", c.ID[:8])
		}
	}
}

func TestRoutingNode_Conditioning(t *testing.T) {
	node := testRoutingNode(0x01, 1, 6881)

	if !node.isGood() {
		t.Error("new node should be good")
	}

	node.markBad()
	if !node.isBad() {
		t.Error("marked-bad node should be bad")
	}
	if node.isGood() {
		t.Error("bad node should not be good")
	}

	node.markGood()
	if !node.isGood() {
		t.Error("marked-good node should be good")
	}
	if node.isBad() {
		t.Error("good node should not be bad")
	}

	// Timeout increments condition
	node2 := testRoutingNode(0x02, 2, 6882)
	if node2.condition != 0 {
		t.Errorf("new node condition = %d, want 0", node2.condition)
	}
	node2.timeout()
	if node2.condition != 1 {
		t.Errorf("after 1 timeout condition = %d, want 1", node2.condition)
	}
	if node2.isBad() {
		t.Error("node with condition=1 should not be marked bad (threshold is 5)")
	}
}

func TestXORDistance_Ordering(t *testing.T) {
	// Replicate DHTIDCloser test: sort by distance to target
	target := testNodeID(0xa0)

	nodes := []NodeInfo{
		{ID: testNodeID(0xf0), IP: [4]byte{192, 0, 2, 1}, Port: 6881},
		{ID: testNodeID(0xb0), IP: [4]byte{192, 0, 2, 2}, Port: 6882},
		{ID: testNodeID(0xa0), IP: [4]byte{192, 0, 2, 3}, Port: 6883},
		{ID: testNodeID(0x80), IP: [4]byte{192, 0, 2, 4}, Port: 6884},
		{ID: testNodeID(0x00), IP: [4]byte{192, 0, 2, 5}, Port: 6885},
	}

	// Sort by XOR distance to target
	for i := 0; i < len(nodes); i++ {
		for j := i + 1; j < len(nodes); j++ {
			di := XORDistance(nodes[i].ID, target)
			dj := XORDistance(nodes[j].ID, target)
			if di.Cmp(dj) > 0 {
				nodes[i], nodes[j] = nodes[j], nodes[i]
			}
		}
	}

	// Expected order: same as DHTIDCloser test
	expected := []byte{0xa0, 0xb0, 0x80, 0xf0, 0x00}
	for i, n := range nodes {
		if n.ID[0] != expected[i] {
			t.Errorf("sorted[%d] ID[0]=%02x, want %02x", i, n.ID[0], expected[i])
		}
	}
}

func TestXORDistance_Symmetric(t *testing.T) {
	a := testNodeID(0x55)
	b := testNodeID(0xaa)
	d1 := XORDistance(a, b)
	d2 := XORDistance(b, a)
	if d1.Cmp(d2) != 0 {
		t.Error("XOR distance should be symmetric")
	}
}

func TestXORDistance_Self(t *testing.T) {
	a := testNodeID(0x42)
	d := XORDistance(a, a)
	if d.Sign() != 0 || d.BitLen() != 0 {
		t.Error("XOR distance to self should be 0")
	}
}

func TestCompactEncodingRoundTrip_Multiple(t *testing.T) {
	nodes := make([]NodeInfo, 3)
	nodes[0] = NodeInfo{
		ID:   testNodeID(0x01),
		IP:   [4]byte{127, 0, 0, 1},
		Port: 6881,
	}
	nodes[1] = NodeInfo{
		ID:   testNodeID(0x02),
		IP:   [4]byte{192, 168, 1, 1},
		Port: 6882,
	}
	nodes[2] = NodeInfo{
		ID:   testNodeID(0x03),
		IP:   [4]byte{10, 0, 0, 1},
		Port: 6883,
	}

	data := CompactNodes(nodes)
	if len(data) != 78 {
		t.Errorf("CompactNodes length = %d, want 78", len(data))
	}

	decoded, err := DecodeCompactNodes(data)
	if err != nil {
		t.Fatalf("DecodeCompactNodes error: %v", err)
	}
	if len(decoded) != 3 {
		t.Fatalf("decoded length = %d, want 3", len(decoded))
	}

	for i := range 3 {
		if decoded[i].ID != nodes[i].ID {
			t.Errorf("decoded[%d] ID mismatch", i)
		}
		if decoded[i].IP != nodes[i].IP {
			t.Errorf("decoded[%d] IP mismatch", i)
		}
		if decoded[i].Port != nodes[i].Port {
			t.Errorf("decoded[%d] Port mismatch", i)
		}
	}
}

func TestTokenGenerationAndValidation(t *testing.T) {
	var infoHash [20]byte
	infoHash[0] = 0x11
	ip := "192.168.0.1"
	port := uint16(6881)

	cfg := Config{
		Addr: "127.0.0.1:0",
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}

	token := srv.generateToken(infoHash, ip, port)
	if len(token) != 20 {
		t.Errorf("token length = %d, want 20 (SHA1 digest)", len(token))
	}

	if !srv.validateToken(token, infoHash, ip, port) {
		t.Error("validateToken should accept generated token")
	}

	// Wrong ip should fail
	if srv.validateToken(token, infoHash, "192.168.0.2", port) {
		t.Error("validateToken with wrong IP should fail")
	}

	// Wrong infoHash should fail
	var wrongHash [20]byte
	wrongHash[0] = 0x22
	if srv.validateToken(token, wrongHash, ip, port) {
		t.Error("validateToken with wrong infoHash should fail")
	}

	// Wrong port should fail
	if srv.validateToken(token, infoHash, ip, 6882) {
		t.Error("validateToken with wrong port should fail")
	}
}

func TestTokenSecretRotation(t *testing.T) {
	var infoHash [20]byte
	infoHash[0] = 0x11
	ip := "192.168.0.1"
	port := uint16(6881)

	cfg := Config{
		Addr: "127.0.0.1:0",
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}

	oldToken := srv.generateToken(infoHash, ip, port)
	if !srv.validateToken(oldToken, infoHash, ip, port) {
		t.Error("validateToken should accept freshly generated token")
	}

	// Update once: old token should still validate (secret rotated to slot 1)
	srv.updateTokenSecret()
	if !srv.validateToken(oldToken, infoHash, ip, port) {
		t.Error("validateToken should accept old token after one rotation")
	}

	// New token after rotation
	newToken := srv.generateToken(infoHash, ip, port)

	// Update again: old token should now be invalid, new token should still work
	srv.updateTokenSecret()
	if srv.validateToken(oldToken, infoHash, ip, port) {
		t.Error("validateToken should reject token after two rotations")
	}
	if !srv.validateToken(newToken, infoHash, ip, port) {
		t.Error("validateToken should accept token from previous secret")
	}
}

func TestPeerStorage(t *testing.T) {
	cfg := Config{
		Addr: "127.0.0.1:0",
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}

	// Initially empty
	if len(srv.peers) != 0 {
		t.Errorf("initial peers count = %d, want 0", len(srv.peers))
	}

	var infoHash [20]byte
	infoHash[0] = 0x01

	// Store a peer
	peer1 := &net.TCPAddr{IP: net.IPv4(192, 168, 0, 1), Port: 6881}
	srv.peersMu.Lock()
	srv.peers[infoHash] = append(srv.peers[infoHash], peer1)
	srv.peersMu.Unlock()

	if len(srv.peers[infoHash]) != 1 {
		t.Errorf("peer count = %d, want 1", len(srv.peers[infoHash]))
	}
	if srv.peers[infoHash][0].String() != "192.168.0.1:6881" {
		t.Errorf("peer = %s, want 192.168.0.1:6881", srv.peers[infoHash][0].String())
	}

	// Different infoHash
	var infoHash2 [20]byte
	infoHash2[0] = 0x02
	peer2 := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 6881}
	srv.peers[infoHash2] = append(srv.peers[infoHash2], peer2)

	if len(srv.peers[infoHash]) != 1 {
		t.Error("infoHash1 should still have 1 peer")
	}
	if len(srv.peers[infoHash2]) != 1 {
		t.Error("infoHash2 should have 1 peer")
	}
}

func TestPeerTokenStorage(t *testing.T) {
	cfg := Config{
		Addr: "127.0.0.1:0",
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}

	var infoHash [20]byte
	infoHash[0] = 0x01

	token := "testtoken12345"
	srv.storePeerToken(infoHash, "192.168.0.1", 6881, token)

	got := srv.getPeerToken(infoHash, "192.168.0.1", 6881)
	if got != token {
		t.Errorf("getPeerToken = %q, want %q", got, token)
	}

	// Different IP returns empty
	got2 := srv.getPeerToken(infoHash, "192.168.0.2", 6881)
	if got2 != "" {
		t.Errorf("getPeerToken with wrong IP = %q, want empty", got2)
	}
}

func TestMakeTrackingKey(t *testing.T) {
	cfg := Config{
		Addr: "127.0.0.1:0",
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}

	addr, _ := net.ResolveUDPAddr("udp", "192.168.0.1:6881")
	addr2, _ := net.ResolveUDPAddr("udp", "192.168.0.1:6882")
	addr3, _ := net.ResolveUDPAddr("udp", "192.168.0.2:6881")

	k1 := srv.makeTrackingKey("aabbccdd", addr)
	k2 := srv.makeTrackingKey("aabbccdd", addr)
	k3 := srv.makeTrackingKey("aabbccdd", addr2)
	k4 := srv.makeTrackingKey("eeffgghh", addr)
	k5 := srv.makeTrackingKey("aabbccdd", addr3)

	if k1 != k2 {
		t.Error("same txID+addr should produce same key")
	}
	if k1 == k3 {
		t.Error("different ports should produce different keys")
	}
	if k1 == k4 {
		t.Error("different txIDs should produce different keys")
	}
	if k1 == k5 {
		t.Error("different IPs should produce different keys")
	}
}

func TestNewTxID(t *testing.T) {
	cfg := Config{
		Addr: "127.0.0.1:0",
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}

	// Generate several txIDs, they should be hex-encoded 2 bytes = 4 chars
	for range 10 {
		txID := srv.newTxID()
		if len(txID) != 8 {
			t.Errorf("txID length = %d, want 8", len(txID))
		}
		// Should be valid hex
		for _, c := range txID {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("txID %q contains invalid hex char %c", txID, c)
			}
		}
	}
}

func TestMessageFactory_PingMessage(t *testing.T) {
	args := bencode.NewDict()
	args.Set("id", bencode.NewString("abcdefghij0123456789"))
	m := NewQuery(QPing, args)
	m.T = "aa"

	if m.Y != "q" {
		t.Errorf("Y = %q, want q", m.Y)
	}
	if m.Q != QPing {
		t.Errorf("Q = %q, want ping", m.Q)
	}
	if m.T != "aa" {
		t.Errorf("T = %q, want aa", m.T)
	}
	idV, ok := m.A.Get("id")
	if !ok {
		t.Fatal("missing id in args")
	}
	if idV.(bencode.StringVal).S != "abcdefghij0123456789" {
		t.Errorf("id = %q", idV.(bencode.StringVal).S)
	}

	// Marshal round-trip
	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	expected := "d1:ad2:id20:abcdefghij0123456789e1:q4:ping1:t2:aa1:v0:1:y1:qe"
	if string(data) != expected {
		t.Errorf("Marshal = %q, want %q", string(data), expected)
	}
	parsed, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if parsed.Q != QPing || parsed.T != "aa" {
		t.Error("round-trip ping fields mismatch")
	}
}

func TestMessageFactory_FindNodeMessage(t *testing.T) {
	args := bencode.NewDict()
	args.Set("id", bencode.NewString("abcdefghij0123456789"))
	args.Set("target", bencode.NewString("mnopqrstuvwxyz123456"))
	m := NewQuery(QFindNode, args)
	m.T = "bb"

	if m.Q != QFindNode {
		t.Errorf("Q = %q, want find_node", m.Q)
	}
	targetV, ok := m.A.Get("target")
	if !ok {
		t.Fatal("missing target in args")
	}
	if targetV.(bencode.StringVal).S != "mnopqrstuvwxyz123456" {
		t.Errorf("target = %q", targetV.(bencode.StringVal).S)
	}

	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	parsed, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if parsed.Q != QFindNode {
		t.Error("round-trip find_node fields mismatch")
	}
	tv, _ := parsed.A.Get("target")
	if tv.(bencode.StringVal).S != "mnopqrstuvwxyz123456" {
		t.Error("round-trip target field mismatch")
	}
}

func TestMessageFactory_GetPeersMessage(t *testing.T) {
	args := bencode.NewDict()
	args.Set("id", bencode.NewString("abcdefghij0123456789"))
	args.Set("info_hash", bencode.NewString("infohash_data_00000"))
	m := NewQuery(QGetPeers, args)
	m.T = "cc"

	if m.Q != QGetPeers {
		t.Errorf("Q = %q, want get_peers", m.Q)
	}
	ihV, ok := m.A.Get("info_hash")
	if !ok {
		t.Fatal("missing info_hash in args")
	}
	if ihV.(bencode.StringVal).S != "infohash_data_00000" {
		t.Errorf("info_hash = %q", ihV.(bencode.StringVal).S)
	}

	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	parsed, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if parsed.Q != QGetPeers {
		t.Error("round-trip get_peers fields mismatch")
	}
	ihv2, _ := parsed.A.Get("info_hash")
	if ihv2.(bencode.StringVal).S != "infohash_data_00000" {
		t.Error("round-trip info_hash mismatch")
	}
}

func TestMessageFactory_AnnouncePeerMessage(t *testing.T) {
	args := bencode.NewDict()
	args.Set("id", bencode.NewString("abcdefghij0123456789"))
	args.Set("info_hash", bencode.NewString("infohash_data_00000"))
	args.Set("port", bencode.NewInt(6881))
	args.Set("token", bencode.NewString("mytoken"))
	m := NewQuery(QAnnouncePeer, args)
	m.T = "dd"

	if m.Q != QAnnouncePeer {
		t.Errorf("Q = %q, want announce_peer", m.Q)
	}
	portV, ok := m.A.Get("port")
	if !ok {
		t.Fatal("missing port")
	}
	if portV.(bencode.IntVal).I != 6881 {
		t.Errorf("port = %d", portV.(bencode.IntVal).I)
	}
	tokenV, ok := m.A.Get("token")
	if !ok {
		t.Fatal("missing token")
	}
	if tokenV.(bencode.StringVal).S != "mytoken" {
		t.Errorf("token = %q", tokenV.(bencode.StringVal).S)
	}

	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	parsed, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if parsed.Q != QAnnouncePeer {
		t.Error("round-trip announce_peer fields mismatch")
	}
	pv, _ := parsed.A.Get("port")
	if pv.(bencode.IntVal).I != 6881 {
		t.Error("round-trip port mismatch")
	}
}

func TestMessageFactory_PingReplyMessage(t *testing.T) {
	r := bencode.NewDict()
	r.Set("id", bencode.NewString("abcdefghij0123456789"))
	m := NewResponse("aa", r)

	if m.Y != "r" {
		t.Errorf("Y = %q, want r", m.Y)
	}
	if m.T != "aa" {
		t.Errorf("T = %q, want aa", m.T)
	}
	idV, ok := m.R.Get("id")
	if !ok {
		t.Fatal("missing id")
	}
	if idV.(bencode.StringVal).S != "abcdefghij0123456789" {
		t.Errorf("id = %q", idV.(bencode.StringVal).S)
	}

	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	expected := "d1:rd2:id20:abcdefghij0123456789e1:t2:aa1:v0:1:y1:re"
	if string(data) != expected {
		t.Errorf("Marshal = %q, want %q", string(data), expected)
	}
	parsed, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if parsed.Y != "r" || parsed.T != "aa" {
		t.Error("round-trip reply fields mismatch")
	}
}

func TestMessageFactory_FindNodeReplyMessage(t *testing.T) {
	// Build compact node info for 8 nodes
	nodes := make([]NodeInfo, 8)
	for i := range 8 {
		nodes[i] = NodeInfo{
			ID:   testNodeID(byte(0xf0 + i)),
			IP:   [4]byte{192, 168, 0, byte(i + 1)},
			Port: uint16(6881 + i),
		}
	}
	compactData := CompactNodes(nodes)

	r := bencode.NewDict()
	r.Set("id", bencode.NewString("abcdefghij0123456789"))
	r.Set("nodes", bencode.NewString(string(compactData)))
	m := NewResponse("aa", r)

	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	parsed, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	nodesV, ok := parsed.R.Get("nodes")
	if !ok {
		t.Fatal("missing nodes in reply")
	}
	decoded, err := DecodeCompactNodes([]byte(nodesV.(bencode.StringVal).S))
	if err != nil {
		t.Fatalf("DecodeCompactNodes error: %v", err)
	}
	if len(decoded) != 8 {
		t.Errorf("decoded nodes = %d, want 8", len(decoded))
	}
	for i := range 8 {
		if decoded[i].Port != uint16(6881+i) {
			t.Errorf("decoded[%d] port = %d, want %d", i, decoded[i].Port, 6881+i)
		}
	}
}

func TestMessageFactory_GetPeersReplyMessage(t *testing.T) {
	r := bencode.NewDict()
	r.Set("id", bencode.NewString("abcdefghij0123456789"))
	r.Set("token", bencode.NewString("replytoken"))

	// Add values list (compact peer addrs)
	values := bencode.NewList(
		bencode.NewString(string([]byte{192, 168, 0, 1, 0x1a, 0xe1})), // 192.168.0.1:6881
		bencode.NewString(string([]byte{192, 168, 0, 2, 0x1a, 0xe2})), // 192.168.0.2:6882
	)
	r.Set("values", values)
	m := NewResponse("aa", r)

	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	parsed, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	tokenV, ok := parsed.R.Get("token")
	if !ok {
		t.Fatal("missing token")
	}
	if tokenV.(bencode.StringVal).S != "replytoken" {
		t.Errorf("token = %q", tokenV.(bencode.StringVal).S)
	}

	valsV, ok := parsed.R.Get("values")
	if !ok {
		t.Fatal("missing values")
	}
	valsList, ok := valsV.(bencode.ListVal)
	if !ok {
		t.Fatalf("values is %T", valsV)
	}
	if len(valsList.L) != 2 {
		t.Fatalf("values length = %d, want 2", len(valsList.L))
	}
}

func TestMessageFactory_ErrorReplyMessage(t *testing.T) {
	m := NewError("aa", 201, "Generic Error")

	if m.Y != "e" {
		t.Errorf("Y = %q, want e", m.Y)
	}
	if m.T != "aa" {
		t.Errorf("T = %q, want aa", m.T)
	}
	if m.E[0] != int64(201) {
		t.Errorf("E[0] = %v, want 201", m.E[0])
	}
	if m.E[1] != "Generic Error" {
		t.Errorf("E[1] = %q", m.E[1])
	}

	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	expected := "d1:eli201e13:Generic Errore1:t2:aa1:v0:1:y1:ee"
	if string(data) != expected {
		t.Errorf("Marshal = %q, want %q", string(data), expected)
	}

	parsed, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if parsed.Y != "e" || parsed.T != "aa" {
		t.Error("round-trip error fields mismatch")
	}
	if parsed.E[0] != int64(201) || parsed.E[1] != "Generic Error" {
		t.Error("round-trip error values mismatch")
	}
}

func TestNewTxID_Uniqueness(t *testing.T) {
	cfg := Config{
		Addr: "127.0.0.1:0",
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}

	seen := make(map[string]bool)
	for range 100 {
		id := srv.newTxID()
		if seen[id] {
			t.Errorf("duplicate txID generated: %q", id)
		}
		seen[id] = true
	}
}

func TestV6NodeInfo_RoundTrip(t *testing.T) {
	n := V6NodeInfo{
		ID:   testNodeID(0x10),
		IP:   [16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		Port: 6881,
	}
	compact := n.Compact()
	if compact[20] != 0x20 || compact[21] != 0x01 {
		t.Error("IPv6 bytes mismatch in compact")
	}
	decoded := DecodeCompactV6NodeInfo(compact)
	if decoded.ID != n.ID || decoded.IP != n.IP || decoded.Port != n.Port {
		t.Error("V6 round-trip mismatch")
	}
}
