package seeder

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

type Seeder struct {
	client      *torrent.Client
	trackerLis  net.Listener
	trackerSrv  *http.Server
	trackerURL  string
	torrents    []*torrent.Torrent
	infoHashes  []metainfo.Hash
	magnetURIs  []string
	metainfoMap map[metainfo.Hash][]byte
	peers       map[metainfo.Hash][]peerEntry
	mu          sync.Mutex
	closeOnce   sync.Once
}

type Config struct {
	NumTorrents int
	FileSizeMB  int
	PieceLen    int
	ListenAddr  string
}

type peerEntry struct {
	ip   net.IP
	port int
	id   []byte
}

type generatedStorage struct {
	seed   uint64
	length int64
}

type generatedTorrent struct {
	seed   uint64
	length int64
}

type generatedPiece struct {
	torrent generatedTorrent
	piece   metainfo.Piece
}

func (s generatedStorage) OpenTorrent(ctx context.Context, info *metainfo.Info, ih metainfo.Hash) (storage.TorrentImpl, error) {
	gt := generatedTorrent{seed: s.seed, length: info.TotalLength()}
	return storage.TorrentImpl{
		Piece: func(p metainfo.Piece) storage.PieceImpl {
			return generatedPiece{torrent: gt, piece: p}
		},
		Close: func() error { return nil },
	}, nil
}

func (t generatedTorrent) ReadAt(b []byte, off int64) (int, error) {
	if off >= t.length {
		return 0, io.EOF
	}
	n := len(b)
	if off+int64(n) > t.length {
		n = int(t.length - off)
		b = b[:n]
	}
	for i := 0; i < n; i++ {
		b[i] = generateByte(t.seed, off+int64(i))
	}
	return n, nil
}

func (p generatedPiece) ReadAt(b []byte, off int64) (int, error) {
	return p.torrent.ReadAt(b, p.piece.Offset()+off)
}

func (p generatedPiece) WriteAt(b []byte, off int64) (int, error) {
	return 0, errors.New("generated storage is read-only")
}

func (p generatedPiece) MarkComplete() error {
	return nil
}

func (p generatedPiece) MarkNotComplete() error {
	return nil
}

func (p generatedPiece) Completion() storage.Completion {
	return storage.Completion{Ok: true, Complete: true}
}

func splitmix64(state uint64) uint64 {
	state += 0x9e3779b97f4a7c15
	z := state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func generateByte(seed uint64, offset int64) byte {
	blockOffset := offset / 8
	byteIndex := offset % 8
	state := seed ^ uint64(blockOffset)
	val := splitmix64(state)
	return byte(val >> (byteIndex * 8))
}

func New(cfg Config) (*Seeder, error) {
	listenAddr := cfg.ListenAddr
	if listenAddr == "" {
		listenAddr = "127.0.0.1:0"
	}
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, err
	}

	s := &Seeder{
		trackerLis:  lis,
		metainfoMap: make(map[metainfo.Hash][]byte),
		peers:       make(map[metainfo.Hash][]peerEntry),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/announce", s.handleAnnounce)
	s.trackerSrv = &http.Server{Handler: mux}
	go s.trackerSrv.Serve(lis)

	s.trackerURL = "http://" + lis.Addr().String() + "/announce"

	clientCfg := torrent.NewDefaultClientConfig()
	clientCfg.DefaultStorage = &generatedStorage{}
	clientCfg.Seed = true
	clientCfg.NoDHT = true
	clientCfg.DisablePEX = true
	clientCfg.DisableIPv6 = true
	clientCfg.ListenHost = torrent.LoopbackListenHost
	clientCfg.ListenPort = 0
	clientCfg.NoUpload = false
	clientCfg.AcceptPeerConnections = true
	clientCfg.AlwaysWantConns = true

	cl, err := torrent.NewClient(clientCfg)
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("new torrent client: %w", err)
	}
	s.client = cl
	return s, nil
}

func (s *Seeder) Start(cfg Config) error {
	num := cfg.NumTorrents
	if num <= 0 {
		num = 10
	}
	fileSize := cfg.FileSizeMB
	if fileSize <= 0 {
		fileSize = 64
	}
	pieceLen := cfg.PieceLen
	if pieceLen <= 0 {
		pieceLen = 256 * 1024
	}

	total := int64(fileSize * 1024 * 1024)

	for i := 0; i < num; i++ {
		seed := uint64(i) + 0x1234567890abcdef

		name := fmt.Sprintf("bench-file-%d.bin", i)
		pieces := computePiecesFromPRNG(seed, total, int64(pieceLen))

		info := metainfo.Info{
			PieceLength: int64(pieceLen),
			Name:        name,
			Length:      total,
			Pieces:      pieces,
		}
		infoBytes, err := bencode.Marshal(&info)
		if err != nil {
			return fmt.Errorf("marshal info %d: %w", i, err)
		}

		mi := metainfo.MetaInfo{
			Announce:  s.trackerURL,
			InfoBytes: infoBytes,
		}

		ts, err := torrent.TorrentSpecFromMetaInfoErr(&mi)
		if err != nil {
			return fmt.Errorf("spec %d: %w", i, err)
		}
		ts.DisableInitialPieceCheck = true
		ts.Storage = &generatedStorage{seed: seed, length: total}

		t, _, err := s.client.AddTorrentSpec(ts)
		if err != nil {
			return fmt.Errorf("add torrent %d: %w", i, err)
		}

		s.torrents = append(s.torrents, t)
		s.infoHashes = append(s.infoHashes, t.InfoHash())

		ih := t.InfoHash()
		magnet := mi.Magnet(&ih, &info)
		s.magnetURIs = append(s.magnetURIs, magnet.String())
		s.metainfoMap[t.InfoHash()] = infoBytes
	}
	return nil
}

func computePiecesFromPRNG(seed uint64, totalLength, pieceLen int64) []byte {
	numPieces := (totalLength + pieceLen - 1) / pieceLen
	pieces := make([]byte, numPieces*20)
	buf := make([]byte, pieceLen)

	for p := int64(0); p < numPieces; p++ {
		offset := p * pieceLen
		size := pieceLen
		if offset+size > totalLength {
			size = totalLength - offset
		}
		for i := int64(0); i < size; i++ {
			buf[i] = generateByte(seed, offset+i)
		}
		h := sha1.Sum(buf[:size])
		copy(pieces[p*20:], h[:])
	}
	return pieces
}

func parseQueryBytes(rawQuery, key string) ([]byte, error) {
	prefix := key + "="
	idx := strings.Index(rawQuery, prefix)
	if idx < 0 {
		return nil, fmt.Errorf("no %s in query", key)
	}
	val := rawQuery[idx+len(prefix):]
	if end := strings.IndexByte(val, '&'); end >= 0 {
		val = val[:end]
	}
	out := make([]byte, 0, len(val))
	for i := 0; i < len(val); {
		if val[i] == '%' {
			if i+2 < len(val) {
				var b byte
				fmt.Sscanf(val[i+1:i+3], "%x", &b)
				out = append(out, b)
				i += 3
			} else {
				return nil, fmt.Errorf("invalid %% sequence")
			}
		} else if val[i] == '+' {
			out = append(out, ' ')
			i++
		} else {
			out = append(out, val[i])
			i++
		}
	}
	return out, nil
}

func (s *Seeder) handleAnnounce(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("[TRACKER-REQ] Incoming announce request: %s (IP: %s)\n", r.URL.RawQuery, r.RemoteAddr)
	q := r.URL.Query()
	ihBytes, err := parseQueryBytes(r.URL.RawQuery, "info_hash")
	if err != nil || len(ihBytes) != 20 {
		fmt.Printf("[TRACKER-REQ] Bad info_hash error: %v, len: %d\n", err, len(ihBytes))
		http.Error(w, "bad info_hash", http.StatusBadRequest)
		return
	}
	var ih metainfo.Hash
	copy(ih[:], ihBytes)

	portStr := q.Get("port")
	eventStr := q.Get("event")
	ipStr := q.Get("ip")
	peerIDBytes, _ := parseQueryBytes(r.URL.RawQuery, "peer_id")
	peerIDRaw := string(peerIDBytes)

	var port int
	fmt.Sscanf(portStr, "%d", &port)

	host := ipStr
	if host == "" {
		h, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			host = h
		}
	}
	ip := net.ParseIP(host)
	if ip == nil {
		ip = net.IPv4(127, 0, 0, 1)
	}

	s.mu.Lock()
	if eventStr == "stopped" {
		existing := s.peers[ih]
		filtered := existing[:0]
		for _, p := range existing {
			if string(p.id) != peerIDRaw {
				filtered = append(filtered, p)
			}
		}
		s.peers[ih] = filtered
	} else {
		s.peers[ih] = appendPeers(s.peers[ih], peerEntry{
			ip:   ip,
			port: port,
			id:   []byte(peerIDRaw),
		})
	}
	peers := append([]peerEntry(nil), s.peers[ih]...)
	s.mu.Unlock()

	resp := map[string]any{
		"interval":   15,
		"complete":   len(peers),
		"incomplete": 0,
	}

	compact := r.URL.Query().Get("compact") == "1"
	if compact {
		var buf []byte
		for _, p := range peers {
			ip4 := p.ip.To4()
			if ip4 == nil {
				continue
			}
			buf = append(buf, ip4...)
			buf = append(buf, byte(p.port>>8), byte(p.port))
		}
		resp["peers"] = string(buf)
		fmt.Printf("[TRACKER-RESP] Compact peer list length: %d bytes (decoded %d peers)\n", len(buf), len(peers))
	} else {
		peerList := []map[string]any{}
		for _, p := range peers {
			peerList = append(peerList, map[string]any{
				"ip":      p.ip.String(),
				"port":    p.port,
				"peer id": string(p.id),
			})
		}
		resp["peers"] = peerList
		fmt.Printf("[TRACKER-RESP] Non-compact peer list: %v\n", peerList)
	}

	fmt.Printf("[TRACKER-RESP] Sending tracker response to %s: complete=%d peers=%v\n", r.RemoteAddr, len(peers), resp["peers"])

	if err := bencode.NewEncoder(w).Encode(resp); err != nil {
		fmt.Printf("[TRACKER-RESP] Encode error: %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func appendPeers(existing []peerEntry, p peerEntry) []peerEntry {
	for _, e := range existing {
		if string(e.id) == string(p.id) {
			return existing
		}
	}
	return append(existing, p)
}

func (s *Seeder) TrackerURL() string                { return s.trackerURL }
func (s *Seeder) InfoHashes() []metainfo.Hash       { return s.infoHashes }
func (s *Seeder) MagnetURIs() []string              { return s.magnetURIs }
func (s *Seeder) MetainfoBytes(ih metainfo.Hash) []byte { return s.metainfoMap[ih] }
func (s *Seeder) Client() *torrent.Client           { return s.client }
func (s *Seeder) Torrents() []*torrent.Torrent      { return s.torrents }

func (s *Seeder) Close() error {
	s.closeOnce.Do(func() {
		if s.client != nil {
			s.client.Close()
		}
		if s.trackerSrv != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 0)
			defer cancel()
			_ = s.trackerSrv.Shutdown(ctx)
		}
		if s.trackerLis != nil {
			_ = s.trackerLis.Close()
		}
	})
	return nil
}

var _ io.Closer = (*Seeder)(nil)
var _ storage.ClientImpl = (*generatedStorage)(nil)
