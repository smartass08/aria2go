package peer

import (
	"bytes"
	"net"
	"testing"
)

func TestEncodeAndParseExtendedHandshake(t *testing.T) {
	payload, err := EncodeExtendedHandshakeKeys("aria2go/test", 6881, 32768, map[int]uint8{
		ExtensionUTPex:      8,
		ExtensionUTMetadata: 9,
	})
	if err != nil {
		t.Fatalf("EncodeExtendedHandshakeKeys() error = %v", err)
	}

	msg, err := ParseExtendedHandshake(payload)
	if err != nil {
		t.Fatalf("ParseExtendedHandshake() error = %v", err)
	}

	if msg.ClientVersion != "aria2go/test" {
		t.Fatalf("ClientVersion = %q, want aria2go/test", msg.ClientVersion)
	}
	if msg.TCPPort != 6881 {
		t.Fatalf("TCPPort = %d, want 6881", msg.TCPPort)
	}
	if msg.MetadataSize != 32768 {
		t.Fatalf("MetadataSize = %d, want 32768", msg.MetadataSize)
	}
	if got := msg.Extensions[ExtensionNameUTPex]; got != 8 {
		t.Fatalf("ut_pex id = %d, want 8", got)
	}
	if got := msg.Extensions[ExtensionNameUTMetadata]; got != 9 {
		t.Fatalf("ut_metadata id = %d, want 9", got)
	}
}

func TestParseExtendedHandshakeIgnoresOversizedMetadata(t *testing.T) {
	payload, err := EncodeExtendedHandshake(ExtendedHandshake{
		MetadataSize: MaxMetadataSize + 1,
		Extensions:   map[string]uint8{ExtensionNameUTMetadata: 9},
	})
	if err != nil {
		t.Fatalf("EncodeExtendedHandshake() error = %v", err)
	}

	msg, err := ParseExtendedHandshake(payload)
	if err != nil {
		t.Fatalf("ParseExtendedHandshake() error = %v", err)
	}
	if msg.MetadataSize != 0 {
		t.Fatalf("MetadataSize = %d, want 0 for oversized value", msg.MetadataSize)
	}
}

func TestParseUTMetadataData(t *testing.T) {
	raw := []byte("metadata-bytes")
	payload, err := EncodeUTMetadataData(2, 65536, raw)
	if err != nil {
		t.Fatalf("EncodeUTMetadataData() error = %v", err)
	}

	msg, err := ParseUTMetadata(payload)
	if err != nil {
		t.Fatalf("ParseUTMetadata() error = %v", err)
	}

	if msg.MessageType != UTMetadataData {
		t.Fatalf("MessageType = %d, want %d", msg.MessageType, UTMetadataData)
	}
	if msg.Piece != 2 {
		t.Fatalf("Piece = %d, want 2", msg.Piece)
	}
	if msg.TotalSize != 65536 {
		t.Fatalf("TotalSize = %d, want 65536", msg.TotalSize)
	}
	if !bytes.Equal(msg.Data, raw) {
		t.Fatalf("Data = %q, want %q", msg.Data, raw)
	}
}

func TestMarshalAndUnmarshalExtendedMessage(t *testing.T) {
	wire := MarshalExtended(9, []byte("payload"))
	msg, err := DecodeMessage(wire)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}

	id, payload, err := UnmarshalExtended(msg)
	if err != nil {
		t.Fatalf("UnmarshalExtended() error = %v", err)
	}
	if id != 9 {
		t.Fatalf("id = %d, want 9", id)
	}
	if string(payload) != "payload" {
		t.Fatalf("payload = %q, want payload", payload)
	}
}

func TestMarshalAndParseUTPex(t *testing.T) {
	wire, err := MarshalUTPex(7, []PEXPeer{
		{IP: net.ParseIP("127.0.0.1"), Port: 6881, Seeder: true},
	}, []PEXPeer{
		{IP: net.ParseIP("2001:db8::1"), Port: 51413},
	})
	if err != nil {
		t.Fatalf("MarshalUTPex() error = %v", err)
	}

	msg, err := DecodeMessage(wire)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}

	extID, pex, err := ParseUTPex(msg)
	if err != nil {
		t.Fatalf("ParseUTPex() error = %v", err)
	}
	if extID != 7 {
		t.Fatalf("extID = %d, want 7", extID)
	}
	if len(pex.Added) != 1 || len(pex.Dropped) != 1 {
		t.Fatalf("added=%d dropped=%d, want 1/1", len(pex.Added), len(pex.Dropped))
	}
	if !pex.Added[0].Seeder {
		t.Fatal("expected added peer seeder flag to round-trip")
	}
	if !pex.Dropped[0].IP.Equal(net.ParseIP("2001:db8::1")) {
		t.Fatalf("dropped peer IP = %v, want 2001:db8::1", pex.Dropped[0].IP)
	}
}

func TestUnmarshalPortMessage(t *testing.T) {
	msg := NewMessage(MsgPort, []byte{0x1a, 0xe1})
	port, err := UnmarshalPort(msg)
	if err != nil {
		t.Fatalf("UnmarshalPort() error = %v", err)
	}
	if port != 6881 {
		t.Fatalf("port = %d, want 6881", port)
	}
}
