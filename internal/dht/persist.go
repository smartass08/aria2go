package dht

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

var (
	dhtFileHeaderV2 = [8]byte{0xa1, 0xa2, 0x02, 0, 0, 0, 0, 0x02}
	dhtFileHeaderV3 = [8]byte{0xa1, 0xa2, 0x02, 0, 0, 0, 0, 0x03}
)

const (
	dhtFileLocalRecordLen = 32
	dhtFileNodeEntryLen   = 56
)

type persistedRoutingTable struct {
	localID NodeID
	nodes   []NodeInfo
}

func loadRoutingTableFile(path string) (persistedRoutingTable, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return persistedRoutingTable{}, nil
	}
	if err != nil {
		return persistedRoutingTable{}, err
	}
	defer f.Close()

	var header [8]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return persistedRoutingTable{}, err
	}

	version := 0
	switch header {
	case dhtFileHeaderV3:
		version = 3
	case dhtFileHeaderV2:
		version = 2
	default:
		return persistedRoutingTable{}, fmt.Errorf("dht: bad routing table header")
	}

	if version == 2 {
		if _, err := readExact(f, 8); err != nil {
			return persistedRoutingTable{}, err
		}
	} else {
		if _, err := readExact(f, 8); err != nil {
			return persistedRoutingTable{}, err
		}
	}

	localRecord, err := readExact(f, dhtFileLocalRecordLen)
	if err != nil {
		return persistedRoutingTable{}, err
	}
	var table persistedRoutingTable
	copy(table.localID[:], localRecord[8:28])

	countBytes, err := readExact(f, 4)
	if err != nil {
		return persistedRoutingTable{}, err
	}
	nodeCount := binary.BigEndian.Uint32(countBytes)
	if _, err := readExact(f, 4); err != nil {
		return persistedRoutingTable{}, err
	}

	table.nodes = make([]NodeInfo, 0, min(int(nodeCount), bucketK*32))
	for range nodeCount {
		entry, err := readExact(f, dhtFileNodeEntryLen)
		if err != nil {
			return persistedRoutingTable{}, err
		}
		if entry[0] != CompactLenIPv4 {
			continue
		}
		compact := entry[8 : 8+CompactLenIPv4]
		ip := [4]byte(compact[:4])
		if ip == ([4]byte{}) {
			continue
		}
		parsedIP := net.IPv4(ip[0], ip[1], ip[2], ip[3])
		if parsedIP.String() == "" {
			continue
		}

		var id NodeID
		copy(id[:], entry[32:52])
		port := binary.BigEndian.Uint16(compact[4:6])
		if ValidatePort(port) != nil {
			continue
		}
		table.nodes = append(table.nodes, NodeInfo{ID: id, IP: ip, Port: port})
	}
	return table, nil
}

func saveRoutingTableFile(path string, localID NodeID, nodes []NodeInfo) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tempPath := path + "__temp"
	_ = os.Remove(tempPath)

	f, err := os.OpenFile(tempPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o666)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
			_ = os.Remove(tempPath)
		}
	}()

	if err := writeAll(f, dhtFileHeaderV3[:]); err != nil {
		return err
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(time.Now().Unix()))
	if err := writeAll(f, buf[:]); err != nil {
		return err
	}

	var localRecord [dhtFileLocalRecordLen]byte
	copy(localRecord[8:28], localID[:])
	if err := writeAll(f, localRecord[:]); err != nil {
		return err
	}

	binary.BigEndian.PutUint32(buf[:4], uint32(len(nodes)))
	clear(buf[4:])
	if err := writeAll(f, buf[:]); err != nil {
		return err
	}

	for _, node := range nodes {
		var entry [dhtFileNodeEntryLen]byte
		entry[0] = CompactLenIPv4
		copy(entry[8:12], node.IP[:])
		binary.BigEndian.PutUint16(entry[12:14], node.Port)
		copy(entry[32:52], node.ID[:])
		if err := writeAll(f, entry[:]); err != nil {
			return err
		}
	}

	if err := f.Close(); err != nil {
		return err
	}
	closed = true
	return os.Rename(tempPath, path)
}

func readExact(r io.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	return buf, err
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
