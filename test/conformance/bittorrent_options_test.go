package conformance

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smartass08/aria2go/internal/bencode"
)

type btMultiFile struct {
	Path []string
	Data []byte
}

func TestBitTorrent_ShowFilesMultiFileParity(t *testing.T) {
	SkipIfNoRef(t)

	files := []btMultiFile{
		{Path: []string{"sub", "file1.bin"}, Data: protocolPayload("bt-show-files-1", 500)},
		{Path: []string{"file2.bin"}, Data: protocolPayload("bt-show-files-2", 700)},
	}
	torrentData, _, _, err := protocolTorrentMultiFile("http://127.0.0.1:9/announce", "mydir", files, 600)
	if err != nil {
		t.Fatalf("build torrent: %v", err)
	}

	dir := t.TempDir()
	torrentPath := filepath.Join(dir, "multi.torrent")
	if err := os.WriteFile(torrentPath, torrentData, 0o644); err != nil {
		t.Fatalf("write torrent: %v", err)
	}

	args := append(protocolBaseArgs(t.TempDir()), "--show-files=true", torrentPath)
	ref := protocolRun(t, true, args)
	impl := protocolRun(t, false, args)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref show-files", ref)
	protocolRequireExitZero(t, "impl show-files", impl)
	assertStableTextEqual(t, "bittorrent show-files stdout",
		withoutShowFilesBanner(ref.Stdout),
		withoutShowFilesBanner(impl.Stdout),
	)
}

func withoutShowFilesBanner(stdout string) string {
	lines := strings.Split(stdout, "\n")
	out := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(line, ">>> Printing the contents of file ") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func TestBitTorrent_SelectFileMultiFileParity(t *testing.T) {
	SkipIfNoRef(t)

	files := btOptionFixtureFiles()
	refBT := startProtocolBTMultiFileFixture(t, "bt-select", files, 4*1024)
	implBT := startProtocolBTMultiFileFixture(t, "bt-select", files, 4*1024)

	refDir := t.TempDir()
	implDir := t.TempDir()
	refTorrent := refBT.writeTorrentFile(t, refDir)
	implTorrent := implBT.writeTorrentFile(t, implDir)

	ref := protocolRun(t, true, bittorrentOptionArgs(refDir, refTorrent, "--select-file=2"))
	impl := protocolRun(t, false, bittorrentOptionArgs(implDir, implTorrent, "--select-file=2"))

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref select-file", ref)
	protocolRequireExitZero(t, "impl select-file", impl)

	protocolRequireFile(t, filepath.Join(refDir, "bt-select", "beta.bin"), files[1].Data)
	protocolRequireFile(t, filepath.Join(implDir, "bt-select", "beta.bin"), files[1].Data)
	assertBTPathStateParity(t, refDir, implDir,
		filepath.Join("bt-select", "alpha.bin"),
		filepath.Join("bt-select", "gamma.bin"),
	)
}

func TestBitTorrent_IndexOutMultiFileParity(t *testing.T) {
	SkipIfNoRef(t)

	files := btOptionFixtureFiles()
	refBT := startProtocolBTMultiFileFixture(t, "bt-index-out", files, 4*1024)
	implBT := startProtocolBTMultiFileFixture(t, "bt-index-out", files, 4*1024)

	refDir := t.TempDir()
	implDir := t.TempDir()
	refTorrent := refBT.writeTorrentFile(t, refDir)
	implTorrent := implBT.writeTorrentFile(t, implDir)

	extra := []string{
		"--index-out=1=renamed/alpha.out",
		"--index-out=3=gamma.out",
	}
	ref := protocolRun(t, true, bittorrentOptionArgs(refDir, refTorrent, extra...))
	impl := protocolRun(t, false, bittorrentOptionArgs(implDir, implTorrent, extra...))

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref index-out", ref)
	protocolRequireExitZero(t, "impl index-out", impl)

	for _, check := range []struct {
		rel  string
		data []byte
	}{
		{rel: filepath.Join("renamed", "alpha.out"), data: files[0].Data},
		{rel: filepath.Join("bt-index-out", "beta.bin"), data: files[1].Data},
		{rel: "gamma.out", data: files[2].Data},
	} {
		protocolRequireFile(t, filepath.Join(refDir, check.rel), check.data)
		protocolRequireFile(t, filepath.Join(implDir, check.rel), check.data)
	}
	assertBTPathStateParity(t, refDir, implDir,
		filepath.Join("bt-index-out", "alpha.bin"),
		filepath.Join("bt-index-out", "gamma.bin"),
	)
}

func btOptionFixtureFiles() []btMultiFile {
	return []btMultiFile{
		{Path: []string{"alpha.bin"}, Data: protocolPayload("bt-option-alpha", 4*1024)},
		{Path: []string{"beta.bin"}, Data: protocolPayload("bt-option-beta", 4*1024)},
		{Path: []string{"gamma.bin"}, Data: protocolPayload("bt-option-gamma", 4*1024)},
	}
}

func bittorrentOptionArgs(dir, torrentPath string, extra ...string) []string {
	args := bittorrentDownloadArgs(dir, torrentPath)
	args = append(args[:len(args)-1], extra...)
	args = append(args, torrentPath)
	return args
}

func assertBTPathStateParity(t *testing.T, refDir, implDir string, rels ...string) {
	t.Helper()

	for _, rel := range rels {
		refPath := filepath.Join(refDir, rel)
		implPath := filepath.Join(implDir, rel)
		refData, refErr := os.ReadFile(refPath)
		implData, implErr := os.ReadFile(implPath)
		if os.IsNotExist(refErr) && os.IsNotExist(implErr) {
			continue
		}
		if os.IsNotExist(refErr) != os.IsNotExist(implErr) {
			t.Fatalf("%s existence mismatch: refErr=%v implErr=%v", rel, refErr, implErr)
		}
		if refErr != nil {
			t.Fatalf("read ref %s: %v", refPath, refErr)
		}
		if implErr != nil {
			t.Fatalf("read impl %s: %v", implPath, implErr)
		}
		if !bytes.Equal(refData, implData) {
			t.Fatalf("%s data mismatch: ref=%d bytes impl=%d bytes", rel, len(refData), len(implData))
		}
	}
}

func startProtocolBTMultiFileFixture(t *testing.T, name string, files []btMultiFile, pieceLength int) *protocolBTFixture {
	t.Helper()

	peerLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bt peer listen: %v", err)
	}
	peerPort := peerLn.Addr().(*net.TCPAddr).Port

	var torrentData []byte
	var infoHash [20]byte
	var payload []byte
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	torrentData, infoHash, payload, err = protocolTorrentMultiFile(tracker.URL+"/announce", name, files, pieceLength)
	if err != nil {
		tracker.Close()
		_ = peerLn.Close()
		t.Fatalf("build torrent fixture: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	f := &protocolBTFixture{
		TorrentData: append([]byte(nil), torrentData...),
		InfoHash:    infoHash,
		Name:        name,
		payload:     payload,
		piece:       pieceLength,
		peerLn:      peerLn,
		tracker:     tracker,
		cancel:      cancel,
	}
	go f.servePeer(ctx)
	t.Cleanup(f.Close)
	return f
}

func protocolTorrentMultiFile(announce, name string, files []btMultiFile, pieceLength int) ([]byte, [20]byte, []byte, error) {
	if pieceLength <= 0 {
		return nil, [20]byte{}, nil, fmt.Errorf("piece length must be positive")
	}
	var payload []byte
	fileValues := make([]bencode.Value, 0, len(files))
	for _, file := range files {
		payload = append(payload, file.Data...)
		pathValues := make([]bencode.Value, 0, len(file.Path))
		for _, part := range file.Path {
			pathValues = append(pathValues, bencode.NewString(part))
		}
		fd := bencode.NewDict()
		fd.Set("length", bencode.NewInt(int64(len(file.Data))))
		fd.Set("path", bencode.NewList(pathValues...))
		fileValues = append(fileValues, fd)
	}

	var pieces []byte
	for off := 0; off < len(payload); off += pieceLength {
		end := off + pieceLength
		if end > len(payload) {
			end = len(payload)
		}
		sum := sha1.Sum(payload[off:end])
		pieces = append(pieces, sum[:]...)
	}

	info := bencode.NewDict()
	info.Set("files", bencode.NewList(fileValues...))
	info.Set("name", bencode.NewString(name))
	info.Set("piece length", bencode.NewInt(int64(pieceLength)))
	info.Set("pieces", bencode.NewString(string(pieces)))

	top := bencode.NewDict()
	top.Set("announce", bencode.NewString(announce))
	top.Set("info", info)

	torrentData, err := bencode.Marshal(top)
	if err != nil {
		return nil, [20]byte{}, nil, err
	}
	infoData, err := bencode.Marshal(info)
	if err != nil {
		return nil, [20]byte{}, nil, err
	}
	return torrentData, sha1.Sum(infoData), payload, nil
}
