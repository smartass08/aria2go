package engine

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
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
	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/core"
	magnetpkg "github.com/smartass08/aria2go/internal/magnet"
	btpeer "github.com/smartass08/aria2go/internal/protocol/bittorrent/peer"
	"github.com/smartass08/aria2go/internal/torrent"
)

type testMagnetFixture struct {
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

func startTestMagnetFixture(t *testing.T, name string, payload []byte, pieceLength int) *testMagnetFixture {
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
		resp, err := testTrackerResponse(peerPort)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(resp)
	}))

	torrentData, infoRaw, infoHash, err = buildTestMagnetTorrent(trackerSrv.URL+"/announce", name, payload, pieceLength)
	if err != nil {
		trackerSrv.Close()
		_ = peerLn.Close()
		t.Fatalf("build torrent fixture: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	f := &testMagnetFixture{
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

func (f *testMagnetFixture) Close() {
	f.cancel()
	_ = f.peerLn.Close()
	f.tracker.Close()
}

func (f *testMagnetFixture) MagnetURI(t *testing.T) string {
	t.Helper()
	var infoHash core.InfoHashV1
	copy(infoHash[:], f.InfoHash[:])
	m := &magnetpkg.Magnet{
		InfoHashV1:  &infoHash,
		DisplayName: f.Name,
		Trackers:    []string{f.tracker.URL + "/announce"},
	}
	return m.String()
}

func buildTestMagnetTorrent(announce, name string, data []byte, pieceLength int) ([]byte, []byte, [20]byte, error) {
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

func testTrackerResponse(peerPort int) ([]byte, error) {
	ip := net.ParseIP("127.0.0.1").To4()
	if ip == nil {
		return nil, fmt.Errorf("cannot encode loopback peer")
	}
	var compact [6]byte
	copy(compact[:4], ip)
	binary.BigEndian.PutUint16(compact[4:], uint16(peerPort))

	resp := bencode.NewDict()
	resp.Set("interval", bencode.NewInt(1800))
	resp.Set("peers", bencode.NewString(string(compact[:])))
	return bencode.Marshal(resp)
}

func (f *testMagnetFixture) servePeer(ctx context.Context) {
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

func (f *testMagnetFixture) handlePeer(ctx context.Context, conn net.Conn) {
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

func (f *testMagnetFixture) writeMetadataPiece(w io.Writer, extID uint8, piece int) error {
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

func (f *testMagnetFixture) writeBitfield(w io.Writer) error {
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

func (f *testMagnetFixture) writePiece(w io.Writer, index, begin, length uint32) error {
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

func TestRunMagnetDownloadMetadataOnlySavesTorrent(t *testing.T) {
	payload := []byte("magnet-metadata-only-payload")
	fixture := startTestMagnetFixture(t, "magnet.bin", payload, 16*1024)

	dir := t.TempDir()
	e, err := New(&config.Options{
		Dir:                    dir,
		BTMetadataOnly:         true,
		BTSaveMetadata:         true,
		BTMaxPeers:             1,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rg := &requestGroup{
		gid:    1,
		opts:   e.cfg,
		uris:   []string{fixture.MagnetURI(t)},
		state:  core.StatusActive,
		ctx:    ctx,
		cancel: cancel,
	}
	e.groups.set(rg.gid, rg)

	e.runMagnetDownload(ctx, rg, rg.uris[0])
	if rg.errCode != 0 {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	if len(rg.followedBy) != 0 {
		t.Fatalf("followedBy = %v, want none for metadata-only", rg.followedBy)
	}

	savedPath := savedTorrentPath(dir, fixture.InfoHash)
	saved, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("saved metadata read: %v", err)
	}
	meta, err := torrent.Load(saved)
	if err != nil {
		t.Fatalf("Load(saved) error = %v", err)
	}
	gotHash, err := meta.InfoHash()
	if err != nil {
		t.Fatalf("InfoHash(saved) error = %v", err)
	}
	if gotHash != fixture.InfoHash {
		t.Fatalf("saved infohash = %x, want %x", gotHash, fixture.InfoHash)
	}
	if _, err := os.Stat(filepath.Join(dir, fixture.Name)); !os.IsNotExist(err) {
		t.Fatalf("payload file should not exist in metadata-only mode; stat err = %v", err)
	}
}

func TestRunMagnetDownloadLoadsSavedMetadataTorrent(t *testing.T) {
	payload := []byte("magnet-load-saved-metadata-payload")
	fixture := startTestMagnetFixture(t, "saved.bin", payload, 16*1024)

	dir := t.TempDir()
	if err := os.WriteFile(savedTorrentPath(dir, fixture.InfoHash), fixture.TorrentData, 0o644); err != nil {
		t.Fatalf("write saved metadata: %v", err)
	}

	e, err := New(&config.Options{
		Dir:                    dir,
		BTLoadSavedMetadata:    true,
		BTMaxPeers:             1,
		SeedTime:               "0",
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rg := &requestGroup{
		gid:    1,
		opts:   e.cfg,
		uris:   []string{fixture.MagnetURI(t)},
		state:  core.StatusActive,
		ctx:    ctx,
		cancel: cancel,
	}
	e.groups.set(rg.gid, rg)

	e.runMagnetDownload(ctx, rg, rg.uris[0])
	if rg.errCode != 0 {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	if len(rg.followedBy) != 0 {
		t.Fatalf("followedBy = %v, want none when saved metadata is loaded directly", rg.followedBy)
	}

	got, err := os.ReadFile(filepath.Join(dir, fixture.Name))
	if err != nil {
		t.Fatalf("read payload: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload = %q, want %q", got, payload)
	}
}

func TestRunMagnetDownloadAddsPausedChild(t *testing.T) {
	payload := []byte("magnet-pause-metadata-payload")
	fixture := startTestMagnetFixture(t, "pause.bin", payload, 16*1024)

	dir := t.TempDir()
	e, err := New(&config.Options{
		Dir:                    dir,
		EnableRPC:              true,
		PauseMetadata:          true,
		BTMaxPeers:             1,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rg := &requestGroup{
		gid:    1,
		opts:   e.cfg,
		uris:   []string{fixture.MagnetURI(t)},
		state:  core.StatusActive,
		ctx:    ctx,
		cancel: cancel,
	}
	e.groups.set(rg.gid, rg)

	e.runMagnetDownload(ctx, rg, rg.uris[0])
	if rg.errCode != 0 {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	if len(rg.followedBy) != 1 {
		t.Fatalf("followedBy = %v, want single child", rg.followedBy)
	}

	childGID := rg.followedBy[0]
	child, ok := e.groups.getLocked(childGID)
	if !ok {
		t.Fatalf("child gid %s not found", childGID)
	}
	defer e.groups.unlock(childGID)

	if !child.pauseReq {
		t.Fatal("child pauseReq = false, want true")
	}
	if child.following != rg.gid {
		t.Fatalf("child.following = %s, want %s", child.following.Hex(), rg.gid.Hex())
	}
	if child.belongsTo != rg.gid {
		t.Fatalf("child.belongsTo = %s, want %s", child.belongsTo.Hex(), rg.gid.Hex())
	}
	if len(e.waiting) != 1 || e.waiting[0] != childGID {
		t.Fatalf("waiting = %v, want [%s]", e.waiting, childGID.Hex())
	}
}
