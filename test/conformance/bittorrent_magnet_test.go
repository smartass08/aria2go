package conformance

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smartass08/aria2go/internal/bencode"
	"github.com/smartass08/aria2go/internal/core"
	magnetpkg "github.com/smartass08/aria2go/internal/magnet"
	btpeer "github.com/smartass08/aria2go/internal/protocol/bittorrent/peer"
	"github.com/smartass08/aria2go/internal/torrent"
)

type protocolBTMagnetFixture struct {
	TorrentData []byte
	InfoRaw     []byte
	InfoHash    [20]byte
	Name        string

	payload []byte
	piece   int
	peerLn  net.Listener
	tracker *httptest.Server
	cancel  context.CancelFunc
}

func startProtocolBTMagnetFixture(t *testing.T, name string, payload []byte, pieceLength int) *protocolBTMagnetFixture {
	t.Helper()

	peerLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bt peer listen: %v", err)
	}
	peerPort := peerLn.Addr().(*net.TCPAddr).Port

	var torrentData []byte
	var infoRaw []byte
	var infoHash [20]byte
	trackerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/announce" {
			http.NotFound(w, r)
			return
		}
		resp, err := protocolTrackerResponse(peerPort)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(resp)
	}))

	torrentData, infoRaw, infoHash, err = buildProtocolMagnetTorrent(trackerSrv.URL+"/announce", name, payload, pieceLength)
	if err != nil {
		trackerSrv.Close()
		_ = peerLn.Close()
		t.Fatalf("build torrent fixture: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	f := &protocolBTMagnetFixture{
		TorrentData: append([]byte(nil), torrentData...),
		InfoRaw:     append([]byte(nil), infoRaw...),
		InfoHash:    infoHash,
		Name:        name,
		payload:     append([]byte(nil), payload...),
		piece:       pieceLength,
		peerLn:      peerLn,
		tracker:     trackerSrv,
		cancel:      cancel,
	}
	go f.servePeer(ctx)
	t.Cleanup(f.Close)
	return f
}

func buildProtocolMagnetTorrent(announce, name string, data []byte, pieceLength int) ([]byte, []byte, [20]byte, error) {
	if pieceLength <= 0 {
		return nil, nil, [20]byte{}, fmt.Errorf("piece length must be positive")
	}
	var pieces []byte
	for off := 0; off < len(data); off += pieceLength {
		end := off + pieceLength
		if end > len(data) {
			end = len(data)
		}
		sum := sha1.Sum(data[off:end])
		pieces = append(pieces, sum[:]...)
	}

	info := bencode.NewDict()
	info.Set("length", bencode.NewInt(int64(len(data))))
	info.Set("name", bencode.NewString(name))
	info.Set("piece length", bencode.NewInt(int64(pieceLength)))
	info.Set("pieces", bencode.NewString(string(pieces)))

	top := bencode.NewDict()
	top.Set("announce", bencode.NewString(announce))
	top.Set("created by", bencode.NewString("aria2go conformance fixture"))
	top.Set("info", info)

	torrentData, err := bencode.Marshal(top)
	if err != nil {
		return nil, nil, [20]byte{}, err
	}
	infoRaw, err := bencode.Marshal(info)
	if err != nil {
		return nil, nil, [20]byte{}, err
	}
	return torrentData, infoRaw, sha1.Sum(infoRaw), nil
}

func (f *protocolBTMagnetFixture) Close() {
	f.cancel()
	_ = f.peerLn.Close()
	f.tracker.Close()
}

func (f *protocolBTMagnetFixture) magnetURI() string {
	var infoHash core.InfoHashV1
	copy(infoHash[:], f.InfoHash[:])
	return (&magnetpkg.Magnet{
		InfoHashV1:  &infoHash,
		DisplayName: f.Name,
		Trackers:    []string{f.tracker.URL + "/announce"},
	}).String()
}

func (f *protocolBTMagnetFixture) servePeer(ctx context.Context) {
	for {
		conn, err := f.peerLn.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
		go f.handlePeer(ctx, conn)
	}
}

func (f *protocolBTMagnetFixture) handlePeer(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	var hs [68]byte
	if _, err := io.ReadFull(conn, hs[:]); err != nil {
		return
	}
	if hs[0] != 19 || string(hs[1:20]) != "BitTorrent protocol" {
		return
	}
	if !bytes.Equal(hs[28:48], f.InfoHash[:]) {
		return
	}

	var resp [68]byte
	resp[0] = 19
	copy(resp[1:20], "BitTorrent protocol")
	reserved := btpeer.MakeReserved(false, true, false)
	copy(resp[20:28], reserved[:])
	copy(resp[28:48], f.InfoHash[:])
	copy(resp[48:68], []byte("-AG0001-magnetpeerXX"))
	if _, err := conn.Write(resp[:]); err != nil {
		return
	}

	extHandshake, err := btpeer.EncodeExtendedHandshakeKeys("magnet-fixture", 0, len(f.InfoRaw), map[int]uint8{
		btpeer.ExtensionUTMetadata: 3,
	})
	if err != nil {
		return
	}
	if _, err := conn.Write(btpeer.MarshalExtended(btpeer.ExtensionHandshakeID, extHandshake)); err != nil {
		return
	}

	bitfieldSent := false
	clientMetadataID := uint8(0)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var lenBuf [4]byte
		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			return
		}
		msgLen := binary.BigEndian.Uint32(lenBuf[:])
		if msgLen == 0 {
			continue
		}
		payload := make([]byte, msgLen)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return
		}
		if len(payload) == 0 {
			continue
		}

		switch payload[0] {
		case byte(btpeer.MsgBitfield), byte(btpeer.MsgInterested):
			if bitfieldSent {
				continue
			}
			if err := f.writeBitfield(conn); err != nil {
				return
			}
			if _, err := conn.Write([]byte{0, 0, 0, 1, 1}); err != nil {
				return
			}
			bitfieldSent = true
		case byte(btpeer.MsgExtended):
			if len(payload) < 2 {
				continue
			}
			if payload[1] == 0 {
				hs, err := btpeer.ParseExtendedHandshake(payload[2:])
				if err != nil {
					return
				}
				clientMetadataID = hs.Extensions[btpeer.ExtensionNameUTMetadata]
				if hs.MetadataSize > 0 && !bitfieldSent {
					if err := f.writeBitfield(conn); err != nil {
						return
					}
					if _, err := conn.Write([]byte{0, 0, 0, 1, 1}); err != nil {
						return
					}
					bitfieldSent = true
				}
				continue
			}
			if payload[1] != 3 {
				continue
			}
			msg, err := btpeer.ParseUTMetadata(payload[2:])
			if err != nil {
				return
			}
			if msg.MessageType != btpeer.UTMetadataRequest {
				continue
			}
			if clientMetadataID == 0 {
				return
			}
			if err := f.writeMetadataPiece(conn, clientMetadataID, msg.Piece); err != nil {
				return
			}
		case byte(btpeer.MsgRequest):
			if len(payload) < 13 {
				return
			}
			index := binary.BigEndian.Uint32(payload[1:5])
			begin := binary.BigEndian.Uint32(payload[5:9])
			length := binary.BigEndian.Uint32(payload[9:13])
			if err := f.writePiece(conn, index, begin, length); err != nil {
				return
			}
		}
	}
}

func (f *protocolBTMagnetFixture) writeMetadataPiece(w io.Writer, extID uint8, piece int) error {
	start := piece * btpeer.MetadataPieceSize
	if start < 0 || start >= len(f.InfoRaw) {
		return nil
	}
	end := start + btpeer.MetadataPieceSize
	if end > len(f.InfoRaw) {
		end = len(f.InfoRaw)
	}
	payload, err := btpeer.EncodeUTMetadataData(piece, len(f.InfoRaw), f.InfoRaw[start:end])
	if err != nil {
		return err
	}
	_, err = w.Write(btpeer.MarshalExtended(extID, payload))
	return err
}

func (f *protocolBTMagnetFixture) writeBitfield(w io.Writer) error {
	numPieces := (len(f.payload) + f.piece - 1) / f.piece
	bitfield := make([]byte, (numPieces+7)/8)
	for i := 0; i < numPieces; i++ {
		bitfield[i/8] |= 1 << (7 - (i % 8))
	}
	msg := make([]byte, 4+1+len(bitfield))
	binary.BigEndian.PutUint32(msg[:4], uint32(1+len(bitfield)))
	msg[4] = byte(btpeer.MsgBitfield)
	copy(msg[5:], bitfield)
	_, err := w.Write(msg)
	return err
}

func (f *protocolBTMagnetFixture) writePiece(w io.Writer, index, begin, length uint32) error {
	offset := int64(index)*int64(f.piece) + int64(begin)
	if offset < 0 || offset >= int64(len(f.payload)) {
		return nil
	}
	end := offset + int64(length)
	if end > int64(len(f.payload)) {
		end = int64(len(f.payload))
	}
	block := f.payload[offset:end]
	msg := make([]byte, 4+1+8+len(block))
	binary.BigEndian.PutUint32(msg[:4], uint32(9+len(block)))
	msg[4] = byte(btpeer.MsgPiece)
	binary.BigEndian.PutUint32(msg[5:9], index)
	binary.BigEndian.PutUint32(msg[9:13], begin)
	copy(msg[13:], block)
	_, err := w.Write(msg)
	return err
}

func bittorrentMagnetArgs(dir, magnetURI string, extra ...string) []string {
	args := protocolBaseArgs(dir)
	args = append(args,
		"--enable-peer-exchange=false",
		"--bt-max-peers=1",
		"--seed-time=0",
	)
	args = append(args, extra...)
	args = append(args, magnetURI)
	return args
}

func TestBitTorrent_MagnetMetadataDownloadParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("bittorrent-magnet-download", 48*1024+777)
	const (
		name        = "bt-magnet.bin"
		pieceLength = 16 * 1024
	)

	refFixture := startProtocolBTMagnetFixture(t, name, payload, pieceLength)
	implFixture := startProtocolBTMagnetFixture(t, name, payload, pieceLength)

	refDir := t.TempDir()
	implDir := t.TempDir()

	ref := protocolRun(t, true, bittorrentMagnetArgs(refDir, refFixture.magnetURI()))
	impl := protocolRun(t, false, bittorrentMagnetArgs(implDir, implFixture.magnetURI()))

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref bittorrent magnet", ref)
	protocolRequireExitZero(t, "impl bittorrent magnet", impl)
	protocolRequireFile(t, filepath.Join(refDir, name), payload)
	protocolRequireFile(t, filepath.Join(implDir, name), payload)
}

func TestBitTorrent_MagnetMetadataOnlySaveParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("bittorrent-magnet-metadata-only", 24*1024+333)
	const (
		name        = "bt-metadata-only.bin"
		pieceLength = 16 * 1024
	)

	refFixture := startProtocolBTMagnetFixture(t, name, payload, pieceLength)
	implFixture := startProtocolBTMagnetFixture(t, name, payload, pieceLength)

	refDir := t.TempDir()
	implDir := t.TempDir()

	extra := []string{"--bt-metadata-only=true", "--bt-save-metadata=true"}
	ref := protocolRun(t, true, bittorrentMagnetArgs(refDir, refFixture.magnetURI(), extra...))
	impl := protocolRun(t, false, bittorrentMagnetArgs(implDir, implFixture.magnetURI(), extra...))

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref bittorrent magnet metadata-only", ref)
	protocolRequireExitZero(t, "impl bittorrent magnet metadata-only", impl)

	for _, tc := range []struct {
		dir      string
		infoHash [20]byte
		name     string
	}{
		{dir: refDir, infoHash: refFixture.InfoHash, name: "ref"},
		{dir: implDir, infoHash: implFixture.InfoHash, name: "impl"},
	} {
		savedPath := filepath.Join(tc.dir, fmt.Sprintf("%x.torrent", tc.infoHash[:]))
		data, err := os.ReadFile(savedPath)
		if err != nil {
			t.Fatalf("%s saved metadata read: %v", tc.name, err)
		}
		meta, err := torrent.Load(data)
		if err != nil {
			t.Fatalf("%s saved metadata parse: %v", tc.name, err)
		}
		gotHash, err := meta.InfoHash()
		if err != nil {
			t.Fatalf("%s saved metadata infohash: %v", tc.name, err)
		}
		if gotHash != tc.infoHash {
			t.Fatalf("%s saved metadata infohash = %x, want %x", tc.name, gotHash, tc.infoHash)
		}
		if _, err := os.Stat(filepath.Join(tc.dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s payload file should not exist in metadata-only mode; err=%v", tc.name, err)
		}
	}
}

func TestBitTorrent_MagnetPauseMetadataParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("bittorrent-magnet-pause-metadata", 32*1024+111)
	const (
		name        = "bt-pause-metadata.bin"
		pieceLength = 16 * 1024
	)

	refFixture := startProtocolBTMagnetFixture(t, name, payload, pieceLength)
	implFixture := startProtocolBTMagnetFixture(t, name, payload, pieceLength)

	refDir := t.TempDir()
	implDir := t.TempDir()
	refPort := findFreePort(t)
	implPort := findFreePort(t)

	commonArgs := []string{
		"--no-conf=true",
		"--allow-overwrite=true",
		"--auto-file-renaming=false",
		"--file-allocation=none",
		"--quiet=true",
		"--show-console-readout=false",
		"--summary-interval=0",
		"--no-netrc=true",
		"--check-certificate=false",
		"--no-proxy=127.0.0.1,localhost",
		"--enable-dht=false",
		"--enable-dht6=false",
		"--bt-enable-lpd=false",
		"--enable-peer-exchange=false",
		"--bt-max-peers=1",
		"--seed-time=0",
		"--pause-metadata=true",
	}

	refArgs := append([]string{"--dir=" + refDir}, commonArgs...)
	implArgs := append([]string{"--dir=" + implDir}, commonArgs...)

	refSrv := startRPCRef(t, refPort, refArgs...)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, implPort, implArgs...)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	refParent := rpcResultString(t, rpcCallOK(t, refPort, "aria2.addUri", []any{[]string{refFixture.magnetURI()}}))
	implParent := rpcResultString(t, rpcCallOK(t, implPort, "aria2.addUri", []any{[]string{implFixture.magnetURI()}}))

	waitForRPCStatus(t, refPort, refParent, "complete")
	waitForRPCStatus(t, implPort, implParent, "complete")

	refParentStatus := mustJSONMap(t, "ref magnet parent status", rpcCallOK(t, refPort, "aria2.tellStatus", []any{refParent}).Result)
	implParentStatus := mustJSONMap(t, "impl magnet parent status", rpcCallOK(t, implPort, "aria2.tellStatus", []any{implParent}).Result)

	refChildren := mustJSONStringSlice(t, "ref magnet followedBy", refParentStatus["followedBy"])
	implChildren := mustJSONStringSlice(t, "impl magnet followedBy", implParentStatus["followedBy"])
	if len(refChildren) != 1 || len(implChildren) != 1 {
		t.Fatalf("followedBy mismatch: ref=%v impl=%v", refChildren, implChildren)
	}

	waitForRPCStatus(t, refPort, refChildren[0], "paused")
	waitForRPCStatus(t, implPort, implChildren[0], "paused")

	refChildStatus := mustStringMap(t, "ref magnet child status", rpcCallOK(t, refPort, "aria2.tellStatus", []any{refChildren[0], []string{"status", "belongsTo", "following"}}).Result)
	implChildStatus := mustStringMap(t, "impl magnet child status", rpcCallOK(t, implPort, "aria2.tellStatus", []any{implChildren[0], []string{"status", "belongsTo", "following"}}).Result)

	if refChildStatus["status"] != "paused" || implChildStatus["status"] != "paused" {
		t.Fatalf("child status mismatch: ref=%v impl=%v", refChildStatus, implChildStatus)
	}
	if refChildStatus["following"] != refParent {
		t.Fatalf("ref child linkage mismatch: parent=%s child=%v", refParent, refChildStatus)
	}
	if refBelongsTo, ok := refChildStatus["belongsTo"]; ok && refBelongsTo != refParent {
		t.Fatalf("ref child belongsTo mismatch: parent=%s child=%v", refParent, refChildStatus)
	}
	if implChildStatus["following"] != implParent {
		t.Fatalf("impl child linkage mismatch: parent=%s child=%v", implParent, implChildStatus)
	}
	if implBelongsTo, ok := implChildStatus["belongsTo"]; ok && implBelongsTo != implParent {
		t.Fatalf("impl child belongsTo mismatch: parent=%s child=%v", implParent, implChildStatus)
	}

	if _, err := os.Stat(filepath.Join(refDir, name)); !os.IsNotExist(err) {
		t.Fatalf("ref paused child should not have payload file yet: %v", err)
	}
	if _, err := os.Stat(filepath.Join(implDir, name)); !os.IsNotExist(err) {
		t.Fatalf("impl paused child should not have payload file yet: %v", err)
	}
}

func mustJSONStringSlice(t *testing.T, label string, raw json.RawMessage) []string {
	t.Helper()

	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatalf("unmarshal %s string slice: %v (raw=%s)", label, err, string(raw))
	}
	return values
}
