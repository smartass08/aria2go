package dht

import (
	"crypto/rand"
	"crypto/sha256"
	"math/big"
	"sync"
	"sync/atomic"
	"time"
)

const (
	bucketK        = 8
	bucketCache    = 2
	badCondition   = 5
	nodeContactInt = 15 * time.Minute
)

func xorDistance(a, b NodeID) *big.Int {
	x := xorPool.Get().(*[NodeIDLength]byte)
	for i := range a {
		x[i] = a[i] ^ b[i]
	}
	d := new(big.Int).SetBytes(x[:])
	xorPool.Put(x)
	return d
}

var xorPool = sync.Pool{
	New: func() any { return new([NodeIDLength]byte) },
}

type routingNode struct {
	id          NodeID
	ip          [4]byte
	port        uint16
	condition   int
	lastContact time.Time
}

func newRoutingNode(n NodeInfo) *routingNode {
	return &routingNode{
		id:          n.ID,
		ip:          n.IP,
		port:        n.Port,
		condition:   0,
		lastContact: time.Now(),
	}
}

func (rn *routingNode) isBad() bool { return rn.condition >= badCondition }
func (rn *routingNode) isQuestionable() bool {
	return !rn.isBad() && time.Since(rn.lastContact) >= nodeContactInt
}
func (rn *routingNode) isGood() bool { return !rn.isBad() && !rn.isQuestionable() }
func (rn *routingNode) markGood()    { rn.condition = 0 }
func (rn *routingNode) markBad()     { rn.condition = badCondition }
func (rn *routingNode) timeout()     { rn.condition++ }
func (rn *routingNode) contact()     { rn.lastContact = time.Now() }

func (rn *routingNode) NodeInfo() NodeInfo {
	return NodeInfo{ID: rn.id, IP: rn.ip, Port: rn.port}
}

func (rn *routingNode) equal(other *routingNode) bool {
	return rn.id == other.id && rn.ip == other.ip && rn.port == other.port
}

type bucket struct {
	prefixLen  int
	minID      NodeID
	maxID      NodeID
	localID    NodeID
	nodes      []*routingNode
	cache      []*routingNode
	lastUpdate time.Time
}

func newBucket(localID NodeID) *bucket {
	b := &bucket{
		prefixLen:  0,
		localID:    localID,
		lastUpdate: time.Now(),
		nodes:      make([]*routingNode, 0, bucketK),
		cache:      make([]*routingNode, 0, bucketCache),
	}
	for i := range b.maxID {
		b.maxID[i] = 0xff
	}
	return b
}

func newBucketWithRange(prefixLen int, maxID, minID NodeID, localID NodeID) *bucket {
	return &bucket{
		prefixLen:  prefixLen,
		minID:      minID,
		maxID:      maxID,
		localID:    localID,
		lastUpdate: time.Now(),
		nodes:      make([]*routingNode, 0, bucketK),
		cache:      make([]*routingNode, 0, bucketCache),
	}
}

func (b *bucket) inRange(id NodeID) bool {
	for i := 0; i < NodeIDLength; i++ {
		if id[i] < b.minID[i] {
			return false
		}
		if id[i] > b.minID[i] {
			break
		}
	}
	for i := 0; i < NodeIDLength; i++ {
		if b.maxID[i] < id[i] {
			return false
		}
		if b.maxID[i] > id[i] {
			break
		}
	}
	return true
}

func (b *bucket) splitAllowed() bool {
	return b.prefixLen < NodeIDLength*8-1 && b.inRange(b.localID)
}

func (b *bucket) split() *bucket {
	byteIdx := b.prefixLen / 8
	bitIdx := 7 - (b.prefixLen % 8)

	rMax := b.maxID
	rMin := b.minID

	rMax[byteIdx] ^= 1 << bitIdx
	b.minID[byteIdx] ^= 1 << bitIdx
	b.prefixLen++

	rBucket := newBucketWithRange(b.prefixLen, rMax, rMin, b.localID)

	kept := make([]*routingNode, 0, len(b.nodes))
	for _, n := range b.nodes {
		if rBucket.inRange(n.id) {
			rBucket.addNode(n)
		} else {
			kept = append(kept, n)
		}
	}
	b.nodes = kept
	return rBucket
}

func (b *bucket) contains(id NodeID) bool {
	for _, n := range b.nodes {
		if n.id == id {
			return true
		}
	}
	return false
}

func (b *bucket) addNode(node *routingNode) bool {
	b.lastUpdate = time.Now()
	for i, n := range b.nodes {
		if n.id == node.id {
			b.nodes = append(b.nodes[:i], b.nodes[i+1:]...)
			b.nodes = append(b.nodes, node)
			return true
		}
	}
	if len(b.nodes) < bucketK {
		b.nodes = append(b.nodes, node)
		return true
	}
	if b.nodes[0].isBad() {
		b.nodes = b.nodes[1:]
		b.nodes = append(b.nodes, node)
		return true
	}
	return false
}

func (b *bucket) cacheNode(node *routingNode) {
	b.cache = append([]*routingNode{node}, b.cache...)
	if len(b.cache) > bucketCache {
		b.cache = b.cache[:bucketCache]
	}
}

func (b *bucket) dropNode(node *routingNode) bool {
	for i, n := range b.nodes {
		if n.id == node.id {
			if len(b.cache) > 0 {
				b.nodes = append(b.nodes[:i], b.nodes[i+1:]...)
				b.nodes = append(b.nodes, b.cache[0])
				b.cache = b.cache[1:]
			} else {
				b.nodes = append(b.nodes[:i], b.nodes[i+1:]...)
			}
			return true
		}
	}
	return false
}

func (b *bucket) goodNodes() []*routingNode {
	out := make([]*routingNode, 0, len(b.nodes))
	for _, n := range b.nodes {
		if !n.isBad() {
			out = append(out, n)
		}
	}
	return out
}

func (b *bucket) needsRefresh() bool {
	return len(b.nodes) < bucketK || time.Since(b.lastUpdate) >= 15*time.Minute
}

func (b *bucket) randomNodeID() NodeID {
	var id NodeID
	if b.prefixLen == 0 {
		_, _ = rand.Read(id[:])
		return id
	}
	_, _ = rand.Read(id[:])
	lastByteIdx := (b.prefixLen - 1) / 8
	copy(id[:lastByteIdx+1], b.minID[:lastByteIdx+1])
	return id
}

type bucketTreeNode struct {
	parent *bucketTreeNode
	left   *bucketTreeNode
	right  *bucketTreeNode
	b      *bucket
	minID  NodeID
	maxID  NodeID
}

func newBucketTreeLeaf(b *bucket) *bucketTreeNode {
	return &bucketTreeNode{
		b:     b,
		minID: b.minID,
		maxID: b.maxID,
	}
}

func (btn *bucketTreeNode) leaf() bool { return btn.b != nil }

func (btn *bucketTreeNode) inRange(id NodeID) bool {
	for i := 0; i < NodeIDLength; i++ {
		if id[i] < btn.minID[i] {
			return false
		}
		if id[i] > btn.minID[i] {
			break
		}
	}
	for i := 0; i < NodeIDLength; i++ {
		if btn.maxID[i] < id[i] {
			return false
		}
		if btn.maxID[i] > id[i] {
			break
		}
	}
	return true
}

func (btn *bucketTreeNode) dig(id NodeID) *bucketTreeNode {
	if btn.leaf() {
		return nil
	}
	if btn.left.inRange(id) {
		return btn.left
	}
	return btn.right
}

func (btn *bucketTreeNode) split() {
	rb := btn.b.split()
	btn.left = newBucketTreeLeaf(rb)
	btn.right = newBucketTreeLeaf(btn.b)
	btn.b = nil
	btn.left.parent = btn
	btn.right.parent = btn
	btn.minID = btn.left.minID
	btn.maxID = btn.right.maxID
}

func findTreeNode(root *bucketTreeNode, id NodeID) *bucketTreeNode {
	if root.leaf() {
		return root
	}
	return findTreeNode(root.dig(id), id)
}

func collectFromLeaf(nodes *[]*routingNode, btn *bucketTreeNode) {
	*nodes = append(*nodes, btn.b.goodNodes()...)
}

func collectDownwardLeftFirst(nodes *[]*routingNode, btn *bucketTreeNode) {
	if btn.leaf() {
		collectFromLeaf(nodes, btn)
	} else {
		collectDownwardLeftFirst(nodes, btn.left)
		if len(*nodes) < bucketK {
			collectDownwardLeftFirst(nodes, btn.right)
		}
	}
}

func collectDownwardRightFirst(nodes *[]*routingNode, btn *bucketTreeNode) {
	if btn.leaf() {
		collectFromLeaf(nodes, btn)
	} else {
		collectDownwardRightFirst(nodes, btn.right)
		if len(*nodes) < bucketK {
			collectDownwardRightFirst(nodes, btn.left)
		}
	}
}

func collectUpward(nodes *[]*routingNode, from *bucketTreeNode) {
	cur := from
	for {
		parent := cur.parent
		if parent == nil {
			break
		}
		if parent.left == cur {
			collectFromLeaf(nodes, parent.right)
		} else {
			collectFromLeaf(nodes, parent.left)
		}
		cur = parent
		if len(*nodes) >= bucketK {
			break
		}
	}
}

func findClosestKNodes(root *bucketTreeNode, target NodeID) []*routingNode {
	nodes := make([]*routingNode, 0, bucketK)
	leaf := findTreeNode(root, target)
	if leaf == root {
		collectFromLeaf(&nodes, leaf)
	} else {
		parent := leaf.parent
		if parent.left == leaf {
			collectDownwardLeftFirst(&nodes, parent)
		} else {
			collectDownwardRightFirst(&nodes, parent)
		}
		if len(nodes) < bucketK {
			collectUpward(&nodes, parent)
		}
	}
	if len(nodes) > bucketK {
		nodes = nodes[:bucketK]
	}
	return nodes
}

func enumerateBuckets(root *bucketTreeNode) []*bucket {
	buckets := make([]*bucket, 0, 8)
	var walk func(*bucketTreeNode)
	walk = func(btn *bucketTreeNode) {
		if btn.leaf() {
			buckets = append(buckets, btn.b)
		} else {
			walk(btn.left)
			walk(btn.right)
		}
	}
	walk(root)
	return buckets
}

type RoutingTable struct {
	mu        sync.RWMutex
	localID   NodeID
	root      *bucketTreeNode
	numBuck   atomic.Int32
	nodeCount atomic.Int64
}

func NewRoutingTable(id NodeID) *RoutingTable {
	b := newBucket(id)
	root := newBucketTreeLeaf(b)
	rt := &RoutingTable{
		localID: id,
		root:    root,
	}
	rt.numBuck.Store(1)
	return rt
}

func (rt *RoutingTable) AddNode(n NodeInfo) bool {
	return rt.addNodeInternal(n, false)
}

func (rt *RoutingTable) addNodeInternal(n NodeInfo, good bool) bool {
	if n.ID == rt.localID {
		return false
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	rn := newRoutingNode(n)
	btn := findTreeNode(rt.root, n.ID)
	for {
		b := btn.b
		existed := b.contains(n.ID)
		if b.addNode(rn) {
			if !existed {
				rt.nodeCount.Add(1)
			}
			return true
		}
		if b.splitAllowed() {
			btn.split()
			rt.numBuck.Add(1)
			if btn.left.inRange(n.ID) {
				btn = btn.left
			} else {
				btn = btn.right
			}
		} else {
			if good {
				b.cacheNode(rn)
			}
			return false
		}
	}
}

func (rt *RoutingTable) AddGoodNode(n NodeInfo) bool {
	return rt.addNodeInternal(n, true)
}

func (rt *RoutingTable) RemoveNode(id NodeID) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.removeNodeLocked(id)
}

func (rt *RoutingTable) removeNodeLocked(id NodeID) {
	btn := findTreeNode(rt.root, id)
	b := btn.b
	for _, n := range b.nodes {
		if n.id == id {
			if b.dropNode(n) {
				rt.nodeCount.Add(-1)
			}
			return
		}
	}
}

func (rt *RoutingTable) GetClosestNodes(target NodeID, count int) []NodeInfo {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	nodes := findClosestKNodes(rt.root, target)
	if count <= 0 {
		count = bucketK
	}
	if count > len(nodes) {
		count = len(nodes)
	}
	out := make([]NodeInfo, count)
	for i := range count {
		out[i] = nodes[i].NodeInfo()
	}
	return out
}

func (rt *RoutingTable) NodeCount() int {
	return int(rt.nodeCount.Load())
}

func (rt *RoutingTable) NumBuckets() int {
	return int(rt.numBuck.Load())
}

func (rt *RoutingTable) bucketFor(id NodeID) *bucket {
	btn := findTreeNode(rt.root, id)
	return btn.b
}

func (rt *RoutingTable) getNode(id NodeID, ip [4]byte, port uint16) *routingNode {
	b := rt.bucketFor(id)
	for _, n := range b.nodes {
		if n.id == id && n.ip == ip && n.port == port {
			return n
		}
	}
	return nil
}

func (rt *RoutingTable) markGood(id NodeID, ip [4]byte, port uint16) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	n := rt.getNode(id, ip, port)
	if n != nil {
		n.markGood()
	}
}

func (rt *RoutingTable) markBadAndRemove(id NodeID, ip [4]byte, port uint16) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	n := rt.getNode(id, ip, port)
	if n != nil {
		n.markBad()
		if rt.bucketFor(id).dropNode(n) {
			rt.nodeCount.Add(-1)
		}
	}
}

func (rt *RoutingTable) onTimeout(id NodeID, ip [4]byte, port uint16) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	n := rt.getNode(id, ip, port)
	if n != nil {
		n.timeout()
		if n.isBad() {
			if rt.bucketFor(id).dropNode(n) {
				rt.nodeCount.Add(-1)
			}
		}
	}
}

func (rt *RoutingTable) updateContact(id NodeID, ip [4]byte, port uint16) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.updateContactLocked(id, ip, port)
}

func (rt *RoutingTable) updateContactLocked(id NodeID, ip [4]byte, port uint16) {
	n := rt.getNode(id, ip, port)
	if n != nil {
		n.contact()
	}
}

func (rt *RoutingTable) allBuckets() []*bucket {
	return enumerateBuckets(rt.root)
}

func (rt *RoutingTable) snapshotGoodNodes() []NodeInfo {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	buckets := enumerateBuckets(rt.root)
	nodes := make([]NodeInfo, 0, rt.NodeCount())
	for _, b := range buckets {
		for _, n := range b.goodNodes() {
			nodes = append(nodes, n.NodeInfo())
		}
	}
	return nodes
}

// BatchUpdate performs addGoodNode + optional removeNode + updateContact under a single lock (#7).
func (rt *RoutingTable) BatchUpdate(good NodeInfo, removeID NodeID, contactID NodeID, contactIP [4]byte, contactPort uint16) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	rt.addNodeInternalLocked(good, true)

	if removeID != (NodeID{}) {
		rt.removeNodeLocked(removeID)
	}

	rt.updateContactLocked(contactID, contactIP, contactPort)
}

func (rt *RoutingTable) addNodeInternalLocked(n NodeInfo, good bool) bool {
	if n.ID == rt.localID {
		return false
	}

	rn := newRoutingNode(n)
	btn := findTreeNode(rt.root, n.ID)
	for {
		b := btn.b
		existed := b.contains(n.ID)
		if b.addNode(rn) {
			if !existed {
				rt.nodeCount.Add(1)
			}
			return true
		}
		if b.splitAllowed() {
			btn.split()
			rt.numBuck.Add(1)
			if btn.left.inRange(n.ID) {
				btn = btn.left
			} else {
				btn = btn.right
			}
		} else {
			if good {
				b.cacheNode(rn)
			}
			return false
		}
	}
}

func RandomNodeID() NodeID {
	var id NodeID
	_, _ = rand.Read(id[:])
	return id
}

func NodeIDFromBytes(b []byte) NodeID {
	var id NodeID
	h := sha256.Sum256(b)
	copy(id[:], h[:NodeIDLength])
	return id
}
