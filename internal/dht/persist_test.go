package dht

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func persistNodeID(b byte) NodeID {
	var id NodeID
	id[0] = b
	return id
}

func persistNodeInfo(id NodeID, ip [4]byte, port uint16) NodeInfo {
	return NodeInfo{ID: id, IP: ip, Port: port}
}

func TestSaveLoadRoutingTableFileV3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dht.dat")
	localID := persistNodeID(0x42)
	nodes := []NodeInfo{
		persistNodeInfo(persistNodeID(0x01), [4]byte{192, 0, 2, 1}, 6881),
		persistNodeInfo(persistNodeID(0x02), [4]byte{198, 51, 100, 2}, 51413),
	}

	if err := saveRoutingTableFile(path, localID, nodes); err != nil {
		t.Fatalf("saveRoutingTableFile() error = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(raw) != 56+56*len(nodes) {
		t.Fatalf("file size = %d, want %d", len(raw), 56+56*len(nodes))
	}
	if got := [8]byte(raw[:8]); got != dhtFileHeaderV3 {
		t.Fatalf("header = %x, want %x", got, dhtFileHeaderV3)
	}
	if got := binary.BigEndian.Uint32(raw[48:52]); got != uint32(len(nodes)) {
		t.Fatalf("node count = %d, want %d", got, len(nodes))
	}

	loaded, err := loadRoutingTableFile(path)
	if err != nil {
		t.Fatalf("loadRoutingTableFile() error = %v", err)
	}
	if loaded.localID != localID {
		t.Fatalf("localID = %x, want %x", loaded.localID, localID)
	}
	if len(loaded.nodes) != len(nodes) {
		t.Fatalf("loaded nodes = %d, want %d", len(loaded.nodes), len(nodes))
	}
	for i := range nodes {
		if loaded.nodes[i] != nodes[i] {
			t.Fatalf("node[%d] = %+v, want %+v", i, loaded.nodes[i], nodes[i])
		}
	}
}

func TestLoadRoutingTableFileV2AndSkipsInvalidNodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dht.dat")
	localID := persistNodeID(0x90)

	var raw []byte
	raw = append(raw, dhtFileHeaderV2[:]...)
	raw = binary.BigEndian.AppendUint32(raw, 1234)
	raw = append(raw, 0, 0, 0, 0)

	localRecord := make([]byte, dhtFileLocalRecordLen)
	copy(localRecord[8:28], localID[:])
	raw = append(raw, localRecord...)
	raw = binary.BigEndian.AppendUint32(raw, 3)
	raw = append(raw, 0, 0, 0, 0)

	badLen := make([]byte, dhtFileNodeEntryLen)
	badLen[0] = CompactLenIPv6
	raw = append(raw, badLen...)

	zeroIP := make([]byte, dhtFileNodeEntryLen)
	zeroIP[0] = CompactLenIPv4
	zeroID := persistNodeID(0x01)
	copy(zeroIP[32:52], zeroID[:])
	binary.BigEndian.PutUint16(zeroIP[12:14], 6881)
	raw = append(raw, zeroIP...)

	good := make([]byte, dhtFileNodeEntryLen)
	good[0] = CompactLenIPv4
	copy(good[8:12], []byte{203, 0, 113, 9})
	binary.BigEndian.PutUint16(good[12:14], 6889)
	goodID := persistNodeID(0x44)
	copy(good[32:52], goodID[:])
	raw = append(raw, good...)

	if err := os.WriteFile(path, raw, 0o666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	loaded, err := loadRoutingTableFile(path)
	if err != nil {
		t.Fatalf("loadRoutingTableFile() error = %v", err)
	}
	if loaded.localID != localID {
		t.Fatalf("localID = %x, want %x", loaded.localID, localID)
	}
	if len(loaded.nodes) != 1 {
		t.Fatalf("loaded nodes = %d, want 1", len(loaded.nodes))
	}
	want := persistNodeInfo(goodID, [4]byte{203, 0, 113, 9}, 6889)
	if loaded.nodes[0] != want {
		t.Fatalf("loaded node = %+v, want %+v", loaded.nodes[0], want)
	}
}

func TestNewServerLoadsPersistedRoutingTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dht.dat")
	localID := persistNodeID(0x51)
	node := persistNodeInfo(persistNodeID(0x52), [4]byte{10, 0, 0, 9}, 6881)
	if err := saveRoutingTableFile(path, localID, []NodeInfo{node}); err != nil {
		t.Fatalf("saveRoutingTableFile() error = %v", err)
	}

	srv, err := NewServer(Config{Addr: "127.0.0.1:0", PersistTo: path})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if srv.cfg.NodeID != localID {
		t.Fatalf("server node ID = %x, want persisted %x", srv.cfg.NodeID, localID)
	}
	if srv.rt.NodeCount() != 1 {
		t.Fatalf("routing node count = %d, want 1", srv.rt.NodeCount())
	}
	got := srv.rt.snapshotGoodNodes()
	if len(got) != 1 || got[0] != node {
		t.Fatalf("routing nodes = %+v, want [%+v]", got, node)
	}
}

func TestNewServerIgnoresCorruptPersistedRoutingTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dht.dat")
	if err := os.WriteFile(path, []byte("not dht"), 0o666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	srv, err := NewServer(Config{Addr: "127.0.0.1:0", PersistTo: path})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if srv.cfg.NodeID == (NodeID{}) {
		t.Fatal("server should generate a node ID after corrupt persistence")
	}
	if srv.rt.NodeCount() != 0 {
		t.Fatalf("routing node count = %d, want 0", srv.rt.NodeCount())
	}
}
