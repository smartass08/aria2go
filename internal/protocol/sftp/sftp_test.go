package sftp

import (
	"context"
	"encoding/binary"
	"io"
	"testing"
)

func TestEncodeFPacket(t *testing.T) {
	tests := []struct {
		name    string
		typ     byte
		id      uint32
		hasID   bool
		payload []byte
		wantLen int
	}{
		{
			name:    "INIT",
			typ:     sshFxpInit,
			id:      0,
			hasID:   false,
			payload: []byte{0, 0, 0, 3},
			wantLen: 9, // 4(length) + 1(type) + 4(payload)
		},
		{
			name:    "OPEN",
			typ:     sshFxpOpen,
			id:      1,
			hasID:   true,
			payload: []byte{0, 0, 0, 5, '/', 't', 'e', 's', 't', 0, 0, 0, 1, 0, 0, 0, 0},
			wantLen: 26, // 4 + 1 + 4 + 17
		},
		{
			name:    "READ",
			typ:     sshFxpRead,
			id:      2,
			hasID:   true,
			payload: []byte{0, 0, 0, 4, 'h', 'n', 'd', 'l', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x10, 0, 0},
			wantLen: 30, // 4 + 1 + 4 + 21
		},
		{
			name:    "CLOSE",
			typ:     sshFxpClose,
			id:      3,
			hasID:   true,
			payload: []byte{0, 0, 0, 4, 'h', 'n', 'd', 'l'},
			wantLen: 17, // 4 + 1 + 4 + 8
		},
		{
			name:    "STAT_empty_path",
			typ:     sshFxpStat,
			id:      4,
			hasID:   true,
			payload: []byte{0, 0, 0, 0},
			wantLen: 13, // 4 + 1 + 4 + 4
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkt := encodeFPacket(tt.typ, tt.id, tt.payload, tt.hasID)
			if len(pkt) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(pkt), tt.wantLen)
			}

			length := binary.BigEndian.Uint32(pkt[:4])
			if int(length)+4 != len(pkt) {
				t.Errorf("length field = %d, want %d (excludes self: %d+4=%d)", length, len(pkt)-4, length, len(pkt))
			}

			if pkt[4] != tt.typ {
				t.Errorf("type = %d, want %d", pkt[4], tt.typ)
			}

			if tt.hasID {
				id := binary.BigEndian.Uint32(pkt[5:9])
				if id != tt.id {
					t.Errorf("id = %d, want %d", id, tt.id)
				}
				if string(pkt[9:]) != string(tt.payload) {
					t.Errorf("payload mismatch")
				}
			} else {
				if string(pkt[5:]) != string(tt.payload) {
					t.Errorf("payload mismatch")
				}
			}
		})
	}
}

func TestDecodeFPacket_Roundtrip(t *testing.T) {
	payload := []byte{0, 0, 0, 5, '/', 't', 'e', 's', 't', 0, 0, 0, 1, 0, 0, 0, 0}
	pkt := encodeFPacket(sshFxpOpen, 7, payload, true)

	typ, id, got, err := decodeFPacket(pkt, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if typ != sshFxpOpen {
		t.Errorf("type = %d, want %d", typ, sshFxpOpen)
	}
	if id != 7 {
		t.Errorf("id = %d, want 7", id)
	}
	if string(got) != string(payload) {
		t.Errorf("payload mismatch")
	}
}

func TestDecodeFPacket_INIT_Roundtrip(t *testing.T) {
	payload := []byte{0, 0, 0, 3}
	pkt := encodeFPacket(sshFxpInit, 0, payload, false)

	typ, id, got, err := decodeFPacket(pkt, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if typ != sshFxpInit {
		t.Errorf("type = %d, want %d", typ, sshFxpInit)
	}
	if id != 0 {
		t.Errorf("id = %d, want 0 (INIT has no id)", id)
	}
	if string(got) != string(payload) {
		t.Errorf("payload mismatch")
	}
}

func TestDecodeFPacket_ShortPacket(t *testing.T) {
	_, _, _, err := decodeFPacket([]byte{0, 0, 0, 5, 1}, true)
	if err == nil {
		t.Error("expected error for short packet")
	}

	_, _, _, err = decodeFPacket([]byte{0, 0, 0, 10}, false)
	if err == nil {
		t.Error("expected error for short packet without id")
	}
}

func TestParseATTRS(t *testing.T) {
	var buf []byte
	buf = binary.BigEndian.AppendUint32(buf, attrSize|attrPermissions) // flags: SIZE | PERMISSIONS
	buf = binary.BigEndian.AppendUint64(buf, 12345)                    // size (8 bytes)
	buf = binary.BigEndian.AppendUint32(buf, 0100777)                  // permissions (4 bytes) - regular file

	fi, err := parseATTRS(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fi.Size != 12345 {
		t.Errorf("size = %d, want 12345", fi.Size)
	}
	if fi.IsDir {
		t.Error("expected IsDir=false for regular file")
	}
}

func TestParseATTRS_Dir(t *testing.T) {
	var buf []byte
	buf = binary.BigEndian.AppendUint32(buf, attrPermissions)
	buf = binary.BigEndian.AppendUint32(buf, 0o040755) // permissions (directory)

	fi, err := parseATTRS(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fi.Size != 0 {
		t.Errorf("size = %d, want 0", fi.Size)
	}
	if !fi.IsDir {
		t.Error("expected IsDir=true for directory")
	}
}

func TestParseATTRS_Short(t *testing.T) {
	_, err := parseATTRS([]byte{0, 0, 0})
	if err == nil {
		t.Error("expected error for short attrs")
	}
}

func TestBuildINIT(t *testing.T) {
	pkt := buildINIT()
	typ, id, payload, err := decodeFPacket(pkt, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if typ != sshFxpInit {
		t.Errorf("type = %d, want %d", typ, sshFxpInit)
	}
	if id != 0 {
		t.Errorf("id = %d, want 0 (INIT has no request id)", id)
	}
	version := binary.BigEndian.Uint32(payload)
	if version != 3 {
		t.Errorf("version = %d, want 3", version)
	}
}

func TestBuildOPEN(t *testing.T) {
	pkt := buildOPEN(1, "/tmp/file", sshFxfRead)
	typ, id, payload, err := decodeFPacket(pkt, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if typ != sshFxpOpen {
		t.Errorf("type = %d, want %d", typ, sshFxpOpen)
	}
	if id != 1 {
		t.Errorf("id = %d, want 1", id)
	}

	pos := 0
	pathLen := binary.BigEndian.Uint32(payload[pos:])
	pos += 4
	path := string(payload[pos : pos+int(pathLen)])
	pos += int(pathLen)
	if path != "/tmp/file" {
		t.Errorf("path = %q, want %q", path, "/tmp/file")
	}
	pflags := binary.BigEndian.Uint32(payload[pos:])
	if pflags != sshFxfRead {
		t.Errorf("pflags = %#x, want %#x", pflags, sshFxfRead)
	}
}

func TestBuildREAD(t *testing.T) {
	h := []byte{1, 2, 3, 4}
	pkt := buildREAD(2, h, 1024, 65536)
	typ, id, payload, err := decodeFPacket(pkt, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if typ != sshFxpRead {
		t.Errorf("type = %d, want %d", typ, sshFxpRead)
	}
	if id != 2 {
		t.Errorf("id = %d, want 2", id)
	}

	pos := 0
	hlen := binary.BigEndian.Uint32(payload[pos:])
	pos += 4
	gotHandle := string(payload[pos : pos+int(hlen)])
	pos += int(hlen)
	if gotHandle != string(h) {
		t.Errorf("handle mismatch")
	}
	offset := binary.BigEndian.Uint64(payload[pos:])
	pos += 8
	if offset != 1024 {
		t.Errorf("offset = %d, want 1024", offset)
	}
	length := binary.BigEndian.Uint32(payload[pos:])
	if length != 65536 {
		t.Errorf("length = %d, want 65536", length)
	}
}

func TestBuildCLOSE(t *testing.T) {
	h := []byte("handle1234")
	pkt := buildCLOSE(3, h)
	typ, id, payload, err := decodeFPacket(pkt, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if typ != sshFxpClose {
		t.Errorf("type = %d, want %d", typ, sshFxpClose)
	}
	if id != 3 {
		t.Errorf("id = %d, want 3", id)
	}

	hlen := binary.BigEndian.Uint32(payload[:4])
	gotHandle := string(payload[4 : 4+int(hlen)])
	if gotHandle != string(h) {
		t.Errorf("handle mismatch")
	}
}

func TestBuildSTAT(t *testing.T) {
	pkt := buildSTAT(4, "/path/to/file")
	typ, id, payload, err := decodeFPacket(pkt, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if typ != sshFxpStat {
		t.Errorf("type = %d, want %d", typ, sshFxpStat)
	}
	if id != 4 {
		t.Errorf("id = %d, want 4", id)
	}

	pathLen := binary.BigEndian.Uint32(payload[:4])
	path := string(payload[4 : 4+int(pathLen)])
	if path != "/path/to/file" {
		t.Errorf("path = %q, want %q", path, "/path/to/file")
	}
}

func TestParseVERSION(t *testing.T) {
	var buf []byte
	buf = binary.BigEndian.AppendUint32(buf, 3)                   // version
	buf = append(buf, 0, 0, 0, 2, 'k', '1', 0, 0, 0, 2, 'v', '1') // extensions

	version, err := parseVERSION(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != 3 {
		t.Errorf("version = %d, want 3", version)
	}
}

func TestParseHANDLE(t *testing.T) {
	var buf []byte
	buf = binary.BigEndian.AppendUint32(buf, 4)
	buf = append(buf, 'h', 'n', 'd', 'l')

	handle, err := parseHANDLE(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(handle) != "hndl" {
		t.Errorf("handle = %q, want %q", string(handle), "hndl")
	}
}

func TestParseDATA(t *testing.T) {
	var buf []byte
	buf = binary.BigEndian.AppendUint32(buf, 4)
	buf = append(buf, 'd', 'a', 't', 'a')

	data, err := parseDATA(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "data" {
		t.Errorf("data = %q, want %q", string(data), "data")
	}
}

func TestParseSTATUS(t *testing.T) {
	var buf []byte
	buf = binary.BigEndian.AppendUint32(buf, sshFxOk) // code
	buf = binary.BigEndian.AppendUint32(buf, 0)       // msg len=0
	buf = binary.BigEndian.AppendUint32(buf, 0)       // lang len=0

	code, msg, err := parseSTATUS(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != sshFxOk {
		t.Errorf("code = %d, want %d", code, sshFxOk)
	}
	if msg != "" {
		t.Errorf("msg = %q, want empty", msg)
	}
}

func TestParseSTATUS_EOF(t *testing.T) {
	var buf []byte
	buf = binary.BigEndian.AppendUint32(buf, sshFxEOF)
	buf = binary.BigEndian.AppendUint32(buf, 7)
	buf = append(buf, "no data"...)

	code, msg, err := parseSTATUS(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != sshFxEOF {
		t.Errorf("code = %d, want %d", code, sshFxEOF)
	}
	if msg != "no data" {
		t.Errorf("msg = %q, want %q", msg, "no data")
	}
	if !isEOF(code) {
		t.Error("expected isEOF=true for SSH_FX_EOF")
	}
}

func TestParseSTATUS_TruncatedMessage(t *testing.T) {
	var buf []byte
	buf = binary.BigEndian.AppendUint32(buf, sshFxFailure)
	buf = binary.BigEndian.AppendUint32(buf, 8)
	buf = append(buf, "short"...)

	_, _, err := parseSTATUS(buf)
	if err == nil {
		t.Fatal("expected error for truncated STATUS message")
	}
}

func TestOpenFileRejectsNegativeOffset(t *testing.T) {
	s := &Session{}
	_, err := s.OpenFile(context.Background(), "/file", -1)
	if err == nil {
		t.Fatal("expected error for negative offset")
	}
}

func TestFileReaderZeroLengthRead(t *testing.T) {
	r := &fileReader{}
	n, err := r.Read(nil)
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}

	r.closeSent = true
	n, err = r.Read(nil)
	if n != 0 {
		t.Errorf("closed n = %d, want 0", n)
	}
	if err != io.EOF {
		t.Fatalf("closed err = %v, want io.EOF", err)
	}
}

func TestStatusCodes(t *testing.T) {
	if isEOF(sshFxOk) {
		t.Error("SSH_FX_OK is not EOF")
	}
	if !isEOF(sshFxEOF) {
		t.Error("SSH_FX_EOF is EOF")
	}
	if isErr(sshFxOk) {
		t.Error("SSH_FX_OK is not error")
	}
	if !isErr(sshFxFailure) {
		t.Error("SSH_FX_FAILURE is error")
	}
	if !isErr(sshFxPermissionDenied) {
		t.Error("SSH_FX_PERMISSION_DENIED is error")
	}
	if !isErr(sshFxNoSuchFile) {
		t.Error("SSH_FX_NO_SUCH_FILE is error")
	}
}
