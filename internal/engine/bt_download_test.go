package engine

import (
	"testing"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/disk"
	btpeer "github.com/smartass08/aria2go/internal/protocol/bittorrent/peer"
	"github.com/smartass08/aria2go/internal/torrent"
)

func TestTorrentFilesToDiskEntries_SingleFile(t *testing.T) {
	files := []torrent.FileInfo{
		{Length: 1024, Path: []string{"foo.txt"}},
	}
	entries := torrentFilesToDiskEntries(files)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Name != "foo.txt" {
		t.Errorf("Name = %q, want foo.txt", entries[0].Name)
	}
	if entries[0].Length != 1024 {
		t.Errorf("Length = %d, want 1024", entries[0].Length)
	}
	if entries[0].Offset != 0 {
		t.Errorf("Offset = %d, want 0", entries[0].Offset)
	}
	if !entries[0].Requested {
		t.Error("Requested should be true")
	}
}

func TestTorrentFilesToDiskEntries_MultiFile(t *testing.T) {
	files := []torrent.FileInfo{
		{Length: 100, Path: []string{"a.txt"}},
		{Length: 200, Path: []string{"dir", "b.txt"}},
		{Length: 300, Path: []string{"c.txt"}},
	}
	entries := torrentFilesToDiskEntries(files)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[0].Offset != 0 {
		t.Errorf("entries[0].Offset = %d, want 0", entries[0].Offset)
	}
	if entries[1].Offset != 100 {
		t.Errorf("entries[1].Offset = %d, want 100", entries[1].Offset)
	}
	if entries[2].Offset != 300 {
		t.Errorf("entries[2].Offset = %d, want 300", entries[2].Offset)
	}
	if entries[1].Name != "dir/b.txt" {
		t.Errorf("entries[1].Name = %q, want dir/b.txt", entries[1].Name)
	}
}

func TestTorrentFilesToDiskEntries_Empty(t *testing.T) {
	entries := torrentFilesToDiskEntries(nil)
	if len(entries) != 0 {
		t.Errorf("got %d entries for nil input, want 0", len(entries))
	}
}

func TestAnnounceURLs_NoTrackers(t *testing.T) {
	meta := &torrent.MetaInfo{}
	urls := announceURLs(meta)
	if len(urls) != 0 {
		t.Errorf("got %d URLs for empty meta, want 0", len(urls))
	}
}

func TestAnnounceURLs_SingleAnnounce(t *testing.T) {
	meta := &torrent.MetaInfo{
		Announce: "http://tracker.example.com/announce",
	}
	urls := announceURLs(meta)
	if len(urls) != 1 {
		t.Fatalf("got %d URLs, want 1", len(urls))
	}
	if urls[0] != "http://tracker.example.com/announce" {
		t.Errorf("urls[0] = %q", urls[0])
	}
}

func TestAnnounceURLs_AnnounceList(t *testing.T) {
	meta := &torrent.MetaInfo{
		Announce: "http://tracker.example.com/announce",
		AnnounceList: [][]string{
			{"http://t1.example.com/ann", "http://t2.example.com/ann"},
			{"http://t3.example.com/ann"},
		},
	}
	urls := announceURLs(meta)
	if len(urls) != 3 {
		t.Fatalf("got %d URLs, want 3", len(urls))
	}
	if urls[0] != "http://t1.example.com/ann" {
		t.Errorf("urls[0] = %q", urls[0])
	}
	if urls[1] != "http://t2.example.com/ann" {
		t.Errorf("urls[1] = %q", urls[1])
	}
	if urls[2] != "http://t3.example.com/ann" {
		t.Errorf("urls[2] = %q", urls[2])
	}
}

func TestAnnounceURLs_AnnounceListEmpty(t *testing.T) {
	meta := &torrent.MetaInfo{
		Announce:     "http://tracker.example.com/announce",
		AnnounceList: [][]string{},
	}
	urls := announceURLs(meta)
	if len(urls) != 0 {
		t.Errorf("got %d URLs for empty announce-list, want 0", len(urls))
	}
}

func TestDownloadPlanBTPieceSetup(t *testing.T) {
	dc := NewDownloadPlan(256*1024, 1024*1024, "test.iso")
	if dc.PieceLength() != 256*1024 {
		t.Errorf("PieceLength = %d, want %d", dc.PieceLength(), 256*1024)
	}
	if dc.TotalLength() != 1024*1024 {
		t.Errorf("TotalLength = %d, want %d", dc.TotalLength(), 1024*1024)
	}
	numPieces := dc.GetNumPieces()
	if numPieces != 4 {
		t.Errorf("GetNumPieces = %d, want 4", numPieces)
	}
}

func TestBtSessionConfigIntegration(t *testing.T) {
	cfg := &config.Options{
		ListenPort:    "6881",
		DHTListenPort: "6882",
		Dir:           "/tmp/bt-test",
	}
	s := NewBtSession(cfg)
	if s == nil {
		t.Fatal("NewBtSession() returned nil")
	}
	if s.Port() != 6881 {
		t.Errorf("Port = %d, want 6881", s.Port())
	}
	if s.DHTPort() != 6882 {
		t.Errorf("DHTPort = %d, want 6882", s.DHTPort())
	}
}

func TestBtPeerConfigWiresPieceLength(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	meta := &torrent.MetaInfo{}
	meta.Info.PieceLength = 256 * 1024

	cfg := e.btPeerConfig(meta, nil)
	if cfg.PieceLength != meta.Info.PieceLength {
		t.Fatalf("PieceLength = %d, want %d", cfg.PieceLength, meta.Info.PieceLength)
	}
}

func TestBtRequestGroupRouting(t *testing.T) {
	rg := &requestGroup{
		torrent: []byte("dummy"),
		opts:    &config.Options{Dir: "/tmp/bt-test"},
	}
	if len(rg.torrent) == 0 {
		t.Error("requestGroup should accept torrent data")
	}
}

func TestDiskAdaptorPieces(t *testing.T) {
	files := []torrent.FileInfo{
		{Length: 1024, Path: []string{"a.txt"}},
	}
	entries := torrentFilesToDiskEntries(files)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	var fe disk.FileEntry = entries[0]
	if fe.Length != 1024 {
		t.Errorf("FileEntry.Length = %d, want 1024", fe.Length)
	}
}

func TestPeerState_HasPiece(t *testing.T) {
	ps := &peerState{
		pieces:   8,
		bitfield: []byte{0x80, 0x00},
	}
	if !ps.hasPiece(0) {
		t.Error("expected piece 0 to be set")
	}
	if ps.hasPiece(1) {
		t.Error("expected piece 1 to be clear")
	}
	if ps.hasPiece(7) {
		t.Error("expected piece 7 to be clear")
	}
}

func TestPeerState_SetPiece(t *testing.T) {
	ps := &peerState{
		pieces:   8,
		bitfield: []byte{0, 0},
	}
	ps.setPiece(0)
	if !ps.hasPiece(0) {
		t.Error("piece 0 should be set after setPiece")
	}
	ps.setPiece(7)
	if !ps.hasPiece(7) {
		t.Error("piece 7 should be set after setPiece")
	}
	ps.setPiece(3)
	if !ps.hasPiece(3) {
		t.Error("piece 3 should be set after setPiece")
	}
}

func TestPeerState_HasAllPieces(t *testing.T) {
	ps := &peerState{
		pieces:   8,
		bitfield: []byte{0xff},
	}
	if !ps.hasAllPieces() {
		t.Error("expected all pieces set for 8-piece field")
	}
}

func TestPeerState_HasAllPiecesPartial(t *testing.T) {
	ps := &peerState{
		pieces:   5,
		bitfield: []byte{0xf8},
	}
	if !ps.hasAllPieces() {
		t.Error("expected all 5 pieces set (0xf8 mask)")
	}

	ps.bitfield[0] = 0xe0
	if ps.hasAllPieces() {
		t.Error("expected not all pieces set (0xe0 mask for 5 pieces)")
	}
}

func TestPeerState_DLRate(t *testing.T) {
	ps := &peerState{}
	ps.dlBytes = 1000
	rate := ps.dlRate()
	if rate != 1000 {
		t.Errorf("dlRate = %d, want 1000 on first call", rate)
	}
	rate = ps.dlRate()
	if rate != 0 {
		t.Errorf("dlRate = %d, want 0 on second call (no new data)", rate)
	}
	ps.dlBytes = 2500
	rate = ps.dlRate()
	if rate != 1500 {
		t.Errorf("dlRate = %d, want 1500 (delta)", rate)
	}
}

func TestBTSwarm_Complete(t *testing.T) {
	swarm := &btSwarm{numPieces: 4}
	tmpDir := t.TempDir()
	sf, err := disk.NewSingleFile(tmpDir+"/test.bin", 1024, disk.AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}
	defer sf.Close()
	sf.SetPieceCount(4)
	swarm.adaptor = sf

	if swarm.complete() {
		t.Error("expected not complete with no pieces marked")
	}
	for i := 0; i < 4; i++ {
		sf.MarkPiece(i, true)
	}
	if !swarm.complete() {
		t.Error("expected complete after all pieces marked")
	}
}

func TestBTSwarm_MissingCount(t *testing.T) {
	swarm := &btSwarm{numPieces: 4}
	tmpDir := t.TempDir()
	sf, err := disk.NewSingleFile(tmpDir+"/test.bin", 1024, disk.AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}
	defer sf.Close()
	sf.SetPieceCount(4)
	swarm.adaptor = sf

	if n := swarm.missingCount(); n != 4 {
		t.Errorf("missingCount = %d, want 4", n)
	}
	sf.MarkPiece(0, true)
	sf.MarkPiece(2, true)
	if n := swarm.missingCount(); n != 2 {
		t.Errorf("missingCount = %d, want 2", n)
	}
}

func TestBTSwarm_EndgameMode(t *testing.T) {
	swarm := &btSwarm{numPieces: 15}
	tmpDir := t.TempDir()
	sf, err := disk.NewSingleFile(tmpDir+"/test.bin", 150*1024, disk.AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}
	defer sf.Close()
	sf.SetPieceCount(15)
	swarm.adaptor = sf

	for i := 0; i < 10; i++ {
		sf.MarkPiece(i, true)
	}
	if !swarm.endgameMode() {
		t.Error("expected endgame mode with 5 missing pieces")
	}

	for i := 10; i < 15; i++ {
		sf.MarkPiece(i, true)
	}
	if swarm.endgameMode() {
		t.Error("expected non-endgame mode with 0 missing pieces")
	}
}

func TestBTSwarm_ChoosePiece_RarestFirst(t *testing.T) {
	swarm := &btSwarm{numPieces: 4}
	tmpDir := t.TempDir()
	sf, err := disk.NewSingleFile(tmpDir+"/test.bin", 4*1024, disk.AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}
	defer sf.Close()
	sf.SetPieceCount(4)
	swarm.adaptor = sf

	p1 := &peerState{
		pieces:   4,
		bitfield: []byte{0xf0},
	}

	// Peer 2 has only piece 3
	p2 := &peerState{
		pieces:   4,
		bitfield: []byte{0x10},
	}
	swarm.peers = []*peerState{p1, p2}

	// p1 has all pieces, p2 has only piece 3.
	// Rarest piece from p1's perspective: piece 3 (only p2 has it, availability=0 in the loop)
	// Actually wait: the loop iterates over `swarm.peers` and checks if peer has the piece
	// and is NOT the requesting peer. For p1, it checks p2. p2 only has piece 3, so avail[3]=1.
	// All other pieces have avail=0 from other peers. So p1 should request piece 0 (rarest).

	for i := 0; i < 4; i++ {
		piece, ok := swarm.choosePiece(p1)
		if !ok {
			t.Fatalf("choosePiece returned no piece on iteration %d", i)
		}
		sf.MarkPiece(piece, true)
	}
}

func TestBTSwarm_HandleMsg_Bitfield(t *testing.T) {
	swarm := &btSwarm{numPieces: 8}
	p := &peerState{pieces: 0}
	msg := peerMsg{
		src: p,
		msg: btpeer.NewMessage(btpeer.MsgBitfield, []byte{0xaa}),
	}
	swarm.handleMsg(msg)

	if p.pieces != 8 {
		t.Errorf("pieces = %d, want 8", p.pieces)
	}
	if !p.hasPiece(0) {
		t.Error("expected piece 0 (bit 7 of 0xaa) to be set")
	}
	if p.hasPiece(1) {
		t.Error("expected piece 1 (bit 6 of 0xaa) to be clear")
	}
	if !p.hasPiece(4) {
		t.Error("expected piece 4 (bit 3 of 0xaa) to be set")
	}
	if !p.hasPiece(6) {
		t.Error("expected piece 6 (bit 1 of 0xaa) to be set")
	}
	if p.hasPiece(5) {
		t.Error("piece 5 (bit 2 of 0xaa) should be clear")
	}
}

func TestBTSwarm_HandleMsg_Have(t *testing.T) {
	swarm := &btSwarm{numPieces: 8}
	p := &peerState{pieces: 8, bitfield: make([]byte, 1)}
	msg := peerMsg{
		src: p,
		msg: btpeer.NewMessage(btpeer.MsgHave, []byte{0, 0, 0, 3}),
	}
	swarm.handleMsg(msg)
	if !p.hasPiece(3) {
		t.Error("expected piece 3 to be set after Have message")
	}
	if p.hasPiece(0) {
		t.Error("piece 0 should still be clear")
	}
}

func TestBTSwarm_HandleMsg_HaveAll(t *testing.T) {
	swarm := &btSwarm{numPieces: 10}
	p := &peerState{}
	msg := peerMsg{
		src: p,
		msg: btpeer.NewMessage(btpeer.MsgHaveAll, nil),
	}
	swarm.handleMsg(msg)
	if !p.hasAllPieces() {
		t.Error("expected all pieces set after HaveAll")
	}
	if p.pieces != 10 {
		t.Errorf("pieces = %d, want 10", p.pieces)
	}
}

func TestBTSwarm_HandleMsg_HaveNone(t *testing.T) {
	swarm := &btSwarm{numPieces: 8}
	p := &peerState{pieces: 8, bitfield: []byte{0xff}}
	msg := peerMsg{
		src: p,
		msg: btpeer.NewMessage(btpeer.MsgHaveNone, nil),
	}
	swarm.handleMsg(msg)
	for i := 0; i < 8; i++ {
		if p.hasPiece(i) {
			t.Errorf("piece %d should be clear after HaveNone", i)
		}
	}
}

func TestBTPieceSource(t *testing.T) {
	tmpDir := t.TempDir()
	sf, err := disk.NewSingleFile(tmpDir+"/test.bin", 1024, disk.AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}
	defer sf.Close()
	sf.SetPieceCount(4)
	sf.MarkPiece(1, true)
	sf.MarkPiece(3, true)

	ps := &btPieceSource{adaptor: sf, numPieces: 4}
	if ps.NumPieces() != 4 {
		t.Errorf("NumPieces = %d, want 4", ps.NumPieces())
	}
	if ps.Have(0) {
		t.Error("piece 0 should not be marked")
	}
	if !ps.Have(1) {
		t.Error("piece 1 should be marked")
	}
	if !ps.Have(3) {
		t.Error("piece 3 should be marked")
	}
	bf := ps.Bitfield()
	if len(bf) != 1 {
		t.Fatalf("bitfield length = %d, want 1", len(bf))
	}
	if bf[0] != 0x50 {
		t.Errorf("bitfield = %#x, want 0x50", bf[0])
	}
}

func TestBTMSEEncryption(t *testing.T) {
	tests := []struct {
		force  bool
		req    bool
		min    string
		expect string
	}{
		{false, false, "plain", "allow"},
		{true, false, "plain", "require"},
		{false, true, "plain", "require"},
		{true, true, "plain", "require"},
		{false, false, "arc4", "require"},
	}
	for _, tt := range tests {
		opts := &config.Options{
			BTForceEncryption: tt.force,
			BTRequireCrypto:   tt.req,
			BTMinCryptoLevel:  tt.min,
		}
		mode := btMSEEncryption(opts)
		got := "unknown"
		switch mode {
		case 1:
			got = "allow"
		case 2:
			got = "prefer"
		case 3:
			got = "require"
		}
		if got != tt.expect {
			t.Errorf("btMSEEncryption(force=%v, req=%v, min=%q) = %s, want %s", tt.force, tt.req, tt.min, got, tt.expect)
		}
	}
}

func TestVerification(t *testing.T) {
	testVerifyHelper(t, 4)
}

func testVerifyHelper(t *testing.T, numPieces int) {
	tmpDir := t.TempDir()
	pieceLen := int64(256)
	totalLen := pieceLen * int64(numPieces)
	sf, err := disk.NewSingleFile(tmpDir+"/test.bin", totalLen, disk.AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}
	defer sf.Close()
	sf.SetPieceCount(numPieces)

	for i := 0; i < numPieces; i++ {
		data := make([]byte, pieceLen)
		for j := range data {
			data[j] = byte(i)
		}
		n, err := sf.WriteAt(data, int64(i)*pieceLen)
		if err != nil {
			t.Fatalf("write piece %d: %v", i, err)
		}
		if n != len(data) {
			t.Fatalf("write piece %d: wrote %d, want %d", i, n, len(data))
		}
	}

	pieces := make([]byte, numPieces*20)
	for i := 0; i < numPieces; i++ {
		data := make([]byte, pieceLen)
		for j := range data {
			data[j] = byte(i)
		}
		h := sha1Sum(data)
		copy(pieces[i*20:(i+1)*20], h[:])
	}

	meta := &torrent.MetaInfo{}
	meta.Info.PieceLength = pieceLen
	meta.Info.Pieces = pieces
	meta.Info.Length = totalLen

	swarm := &btSwarm{
		adaptor:     sf,
		meta:        meta,
		numPieces:   numPieces,
		pieceLen:    pieceLen,
		pieceHashes: pieces,
	}

	for i := 0; i < numPieces; i++ {
		if err := swarm.verifyPiece(i); err != nil {
			t.Fatalf("verifyPiece(%d): %v", i, err)
		}
		if !sf.Have(i) {
			t.Errorf("piece %d should be marked after verification", i)
		}
	}
}
