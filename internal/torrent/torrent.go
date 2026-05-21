package torrent

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/smartass08/aria2go/internal/bencode"
	"github.com/smartass08/aria2go/internal/core"
)

var (
	ErrInvalidTorrent = core.NewError(core.ExitTorrentParseError, "invalid torrent file")
	ErrNoInfo         = core.NewError(core.ExitTorrentParseError, "no info dictionary in torrent")
	ErrNotV2          = core.NewError(core.ExitTorrentParseError, "torrent is not BitTorrent v2")
)

var metaInfoPool = sync.Pool{
	New: func() any { return &MetaInfo{} },
}

func getMetaInfo() *MetaInfo {
	return metaInfoPool.Get().(*MetaInfo)
}

func putMetaInfo(m *MetaInfo) {
	*m = MetaInfo{}
	metaInfoPool.Put(m)
}

// MetaInfo represents a parsed .torrent file (BEP 3 metainfo).
type MetaInfo struct {
	Announce     string
	AnnounceList [][]string
	CreationDate int64
	Comment      string
	CreatedBy    string
	Encoding     string
	Info         Info
	URLList      []string
	Nodes        []NodeInfo
	Raw          []byte
	infoRaw      []byte // cached raw info dict bytes; sub-slice of Raw
}

// Info is the info dictionary from a .torrent file.
type Info struct {
	Name        string
	PieceLength int64
	Pieces      []byte
	Length      int64
	Files       []FileInfo
	Private     bool
	MetaVersion int64
	FileTree    map[string]interface{}
}

// FileInfo describes one file in a multi-file torrent.
type FileInfo struct {
	Length int64
	Path   []string
	MD5Sum string
}

// NodeInfo describes a DHT bootstrap node (host, port).
type NodeInfo struct {
	Host string
	Port int
}

// Load parses .torrent data into a MetaInfo.
func Load(data []byte) (_ *MetaInfo, err error) {
	var raw map[string]bencode.Value
	if err = bencode.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("torrent: %w", err)
	}
	if end, scanErr := scanBencodeValue(data, 0); scanErr != nil {
		return nil, fmt.Errorf("torrent: %w: %v", ErrInvalidTorrent, scanErr)
	} else if end != len(data) {
		return nil, fmt.Errorf("%w: trailing data after top-level dictionary", ErrInvalidTorrent)
	}

	m := getMetaInfo()
	defer func() {
		if err != nil {
			putMetaInfo(m)
		}
	}()

	m.Raw = make([]byte, len(data))
	copy(m.Raw, data)

	// Cache raw info dict bytes for InfoHash/InfoHashV2.
	infoRaw, infoErr := findRawValue(m.Raw, "info")
	if infoErr == nil {
		m.infoRaw = infoRaw
	}

	if a, ok := raw["announce"]; ok {
		sv, ok := a.(bencode.StringVal)
		if !ok {
			return nil, fmt.Errorf("%w: announce must be a string", ErrInvalidTorrent)
		}
		m.Announce = strings.TrimSpace(sv.S)
	}

	m.AnnounceList = parseAnnounceList(raw, m.Announce)

	if cd, ok := raw["creation date"]; ok {
		iv, ok := cd.(bencode.IntVal)
		if !ok {
			return nil, fmt.Errorf("%w: creation date must be an integer", ErrInvalidTorrent)
		}
		m.CreationDate = iv.I
	}

	// Prefer comment.utf-8 over comment (BEP 3). aria2 always encodes both.
	if cu8, ok := raw["comment.utf-8"]; ok {
		if sv, ok := cu8.(bencode.StringVal); ok {
			m.Comment = encodeNonUTF8(sv.S)
		}
	} else if c, ok := raw["comment"]; ok {
		if sv, ok := c.(bencode.StringVal); ok {
			m.Comment = encodeNonUTF8(sv.S)
		}
	}

	if cb, ok := raw["created by"]; ok {
		sv, ok := cb.(bencode.StringVal)
		if !ok {
			return nil, fmt.Errorf("%w: created by must be a string", ErrInvalidTorrent)
		}
		m.CreatedBy = encodeNonUTF8(sv.S)
	}

	if enc, ok := raw["encoding"]; ok {
		sv, ok := enc.(bencode.StringVal)
		if !ok {
			return nil, fmt.Errorf("%w: encoding must be a string", ErrInvalidTorrent)
		}
		m.Encoding = sv.S
	}

	m.URLList = parseURLList(raw)

	m.Nodes = parseNodes(raw)

	infoVal, ok := raw["info"]
	if !ok {
		return nil, fmt.Errorf("%w: missing info dictionary", ErrNoInfo)
	}
	infoDict, ok := infoVal.(*bencode.DictVal)
	if !ok {
		return nil, fmt.Errorf("%w: info must be a dictionary", ErrInvalidTorrent)
	}

	if err := parseInfo(&m.Info, infoDict); err != nil {
		return nil, err
	}

	return m, nil
}

func parseAnnounceList(raw map[string]bencode.Value, announce string) [][]string {
	al, ok := raw["announce-list"]
	if !ok {
		if announce != "" {
			return [][]string{{announce}}
		}
		return nil
	}
	lv, ok := al.(bencode.ListVal)
	if !ok {
		return nil
	}
	result := make([][]string, 0, len(lv.L))
	for _, tierVal := range lv.L {
		tierList, ok := tierVal.(bencode.ListVal)
		if !ok {
			continue // aria2 skips non-list tiers
		}
		tier := make([]string, 0, len(tierList.L))
		for _, urlVal := range tierList.L {
			sv, ok := urlVal.(bencode.StringVal)
			if !ok {
				continue // aria2 skips non-string URLs
			}
			stripped := strings.TrimSpace(sv.S)
			if stripped != "" {
				tier = append(tier, stripped)
			}
		}
		if len(tier) > 0 {
			result = append(result, tier)
		}
	}
	return result
}

// parseURLList handles http-seeding url-list (BEP 19). Supports both string
// and list forms, matching aria2's extractUrlList.
func parseURLList(raw map[string]bencode.Value) []string {
	v, ok := raw["url-list"]
	if !ok {
		return nil
	}
	var result []string
	switch val := v.(type) {
	case bencode.StringVal:
		result = append(result, encodeNonUTF8(val.S))
	case bencode.ListVal:
		result = make([]string, 0, len(val.L))
		for _, elem := range val.L {
			if sv, ok := elem.(bencode.StringVal); ok {
				result = append(result, encodeNonUTF8(sv.S))
			}
		}
	}
	return result
}

// parseNodes handles DHT bootstrap nodes. Each entry is a list of [host, port].
// Matching aria2's extractNodes: validates host is non-empty, port is in range 1-65535.
func parseNodes(raw map[string]bencode.Value) []NodeInfo {
	v, ok := raw["nodes"]
	if !ok {
		return nil
	}
	lv, ok := v.(bencode.ListVal)
	if !ok {
		return nil
	}
	result := make([]NodeInfo, 0, len(lv.L))
	for _, elem := range lv.L {
		addrPair, ok := elem.(bencode.ListVal)
		if !ok || len(addrPair.L) != 2 {
			continue
		}
		hostSV, ok := addrPair.L[0].(bencode.StringVal)
		if !ok {
			continue
		}
		host := strings.TrimSpace(hostSV.S)
		if host == "" {
			continue
		}
		portIV, ok := addrPair.L[1].(bencode.IntVal)
		if !ok {
			continue
		}
		port := int(portIV.I)
		if port <= 0 || port >= 65536 {
			continue
		}
		result = append(result, NodeInfo{Host: encodeNonUTF8(host), Port: port})
	}
	return result
}

func parseInfo(info *Info, d *bencode.DictVal) error {
	vals := d.Values

	// Name: prefer name.utf-8 over name (BEP 3). aria2 always encodes both.
	if nameVal, ok := vals["name.utf-8"]; ok {
		if sv, ok := nameVal.(bencode.StringVal); ok {
			info.Name = encodeNonUTF8(sv.S)
		}
	}
	if info.Name == "" {
		if nameVal, ok := vals["name"]; ok {
			if sv, ok := nameVal.(bencode.StringVal); ok {
				info.Name = encodeNonUTF8(sv.S)
			}
		}
	}

	if detectDirTraversal(info.Name) {
		return fmt.Errorf("%w: directory traversal detected in name %q", ErrInvalidTorrent, info.Name)
	}

	// Piece length is required.
	plVal, ok := vals["piece length"]
	if !ok {
		return fmt.Errorf("%w: missing piece length in info", ErrInvalidTorrent)
	}
	plIV, ok := plVal.(bencode.IntVal)
	if !ok {
		return fmt.Errorf("%w: piece length must be an integer", ErrInvalidTorrent)
	}
	if plIV.I < 0 {
		return fmt.Errorf("%w: piece length must be non-negative, got %d", ErrInvalidTorrent, plIV.I)
	}
	info.PieceLength = plIV.I

	// Pieces (concatenated SHA-1 hashes) is required.
	piecesVal, ok := vals["pieces"]
	if !ok {
		return fmt.Errorf("%w: missing pieces in info", ErrInvalidTorrent)
	}
	piecesSV, ok := piecesVal.(bencode.StringVal)
	if !ok {
		return fmt.Errorf("%w: pieces must be a string", ErrInvalidTorrent)
	}
	info.Pieces = []byte(piecesSV.S)
	if len(info.Pieces)%20 != 0 {
		return fmt.Errorf("%w: pieces length must be a multiple of 20", ErrInvalidTorrent)
	}

	// Private flag.
	if priv, ok := vals["private"]; ok {
		if iv, ok := priv.(bencode.IntVal); ok && iv.I == 1 {
			info.Private = true
		}
	}

	// Meta version (BEP 52 for v2 torrents).
	if mv, ok := vals["meta version"]; ok {
		iv, ok := mv.(bencode.IntVal)
		if !ok {
			return fmt.Errorf("%w: meta version must be an integer", ErrInvalidTorrent)
		}
		info.MetaVersion = iv.I
	}

	// Determine single-file vs multi-file mode.
	lengthVal, hasLength := vals["length"]
	_, hasFiles := vals["files"]

	if hasLength && hasFiles {
		return fmt.Errorf("%w: info has both length and files", ErrInvalidTorrent)
	}

	if hasFiles {
		if err := parseMultiFile(info, d); err != nil {
			return err
		}
	} else {
		if err := parseSingleFile(info, lengthVal, hasLength); err != nil {
			return err
		}
	}

	// Validate piece count matches total size (matching aria2 line 498-501).
	if info.PieceLength > 0 && len(info.Pieces) > 0 {
		expectedPieces := (info.Length + info.PieceLength - 1) / info.PieceLength
		actualPieces := int64(len(info.Pieces) / 20)
		if expectedPieces != actualPieces {
			return fmt.Errorf("%w: piece count mismatch: expected %d, got %d",
				ErrInvalidTorrent, expectedPieces, actualPieces)
		}
	}

	return nil
}

func parseSingleFile(info *Info, lengthVal bencode.Value, hasLength bool) error {
	if !hasLength {
		return fmt.Errorf("%w: missing length in single-file info", ErrInvalidTorrent)
	}
	iv, ok := lengthVal.(bencode.IntVal)
	if !ok {
		return fmt.Errorf("%w: length must be an integer", ErrInvalidTorrent)
	}
	if iv.I < 0 {
		return fmt.Errorf("%w: length must be non-negative, got %d", ErrInvalidTorrent, iv.I)
	}
	info.Length = iv.I
	return nil
}

func parseMultiFile(info *Info, d *bencode.DictVal) error {
	fv, _ := d.Values["files"]
	fl, ok := fv.(bencode.ListVal)
	if !ok {
		return fmt.Errorf("%w: files must be a list", ErrInvalidTorrent)
	}
	info.Files = make([]FileInfo, 0, len(fl.L))
	var totalSize int64
	for _, feVal := range fl.L {
		fd, ok := feVal.(*bencode.DictVal)
		if !ok {
			continue // aria2 skips non-dict entries
		}

		fi := FileInfo{}
		fdv := fd.Values

		flv, ok := fdv["length"]
		if !ok {
			return fmt.Errorf("%w: files entry missing length", ErrInvalidTorrent)
		}
		fli, ok := flv.(bencode.IntVal)
		if !ok {
			return fmt.Errorf("%w: files entry length must be an integer", ErrInvalidTorrent)
		}
		if fli.I < 0 {
			return fmt.Errorf("%w: files entry length must be non-negative, got %d", ErrInvalidTorrent, fli.I)
		}
		fi.Length = fli.I

		// Overflow check (matching aria2 line 239-241).
		if totalSize > math.MaxInt64-fi.Length {
			return fmt.Errorf("%w: total torrent size exceeds maximum", ErrInvalidTorrent)
		}
		totalSize += fi.Length

		// Path: prefer path.utf-8 over path (BEP 3).
		pathVal, pathOK := fdv["path.utf-8"]
		if !pathOK {
			pathVal, pathOK = fdv["path"]
		}
		if !pathOK {
			return fmt.Errorf("%w: files entry missing path", ErrInvalidTorrent)
		}
		pathL, ok := pathVal.(bencode.ListVal)
		if !ok {
			return fmt.Errorf("%w: files entry path must be a list", ErrInvalidTorrent)
		}
		if len(pathL.L) == 0 {
			return fmt.Errorf("%w: files entry path must not be empty", ErrInvalidTorrent)
		}
		fi.Path = make([]string, 0, len(pathL.L))
		for _, pc := range pathL.L {
			sv, ok := pc.(bencode.StringVal)
			if !ok {
				return fmt.Errorf("%w: path component must be a string", ErrInvalidTorrent)
			}
			encoded := encodeNonUTF8(sv.S)
			if detectDirTraversal(encoded) {
				return fmt.Errorf("%w: directory traversal detected in path %q", ErrInvalidTorrent, encoded)
			}
			fi.Path = append(fi.Path, encoded)
		}

		if md5v, ok := fdv["md5sum"]; ok {
			sv, ok := md5v.(bencode.StringVal)
			if ok {
				fi.MD5Sum = sv.S
			}
		}

		info.Files = append(info.Files, fi)
	}
	info.Length = totalSize
	return nil
}

// detectDirTraversal checks for path components and patterns that would
// escape the intended download directory. Matches aria2's util::detectDirTraversal.
func detectDirTraversal(path string) bool {
	if path == "" {
		return false
	}
	b := []byte(path)
	for _, c := range b {
		if c < 0x20 || c == 0x7f {
			return true
		}
	}
	if b[0] == '/' {
		return true
	}
	if bytes.HasPrefix(b, []byte("./")) || bytes.HasPrefix(b, []byte("../")) {
		return true
	}
	if bytes.Contains(b, []byte("/../")) || bytes.Contains(b, []byte("/./")) {
		return true
	}
	if bytes.HasSuffix(b, []byte("/")) || bytes.HasSuffix(b, []byte("/.")) || bytes.HasSuffix(b, []byte("/..")) {
		return true
	}
	// Check each component for "." or "..".
	start := 0
	for i := range path {
		if path[i] == '/' {
			component := path[start:i]
			if component == ".." || component == "." {
				return true
			}
			start = i + 1
		}
	}
	last := path[start:]
	return last == "." || last == ".."
}

// encodeNonUTF8 replaces invalid UTF-8 byte sequences with their %XX
// percent-encoded form. Matches aria2's util::encodeNonUtf8.
func encodeNonUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var buf strings.Builder
	buf.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			fmt.Fprintf(&buf, "%%%02X", s[i])
			i++
		} else {
			buf.WriteString(s[i : i+size])
			i += size
		}
	}
	return buf.String()
}

// InfoHash computes the SHA1 hash of the info dictionary bytes exactly as
// they appear in the original .torrent file. Uses the cached raw info dict
// bytes extracted during Load to avoid re-scanning the bencoded data.
func (m *MetaInfo) InfoHash() ([20]byte, error) {
	var zero [20]byte
	if len(m.infoRaw) == 0 {
		return zero, fmt.Errorf("torrent: infohash: info dict not available")
	}
	return sha1.Sum(m.infoRaw), nil
}

// InfoHashV2 computes the SHA256 hash for BTv2 info dicts. Returns an error
// if the torrent is not a v2 torrent (MetaVersion != 2).
func (m *MetaInfo) InfoHashV2() (core.InfoHashV2, error) {
	var zero core.InfoHashV2
	if m.Info.MetaVersion != 2 {
		return zero, ErrNotV2
	}
	if len(m.infoRaw) == 0 {
		return zero, fmt.Errorf("torrent: infohashv2: info dict not available")
	}
	return sha256.Sum256(m.infoRaw), nil
}

// TotalSize returns the total size of all files in bytes.
func (m *MetaInfo) TotalSize() int64 {
	return m.Info.Length
}

// NumPieces returns the number of pieces in the torrent.
func (m *MetaInfo) NumPieces() int {
	return len(m.Info.Pieces) / 20
}

// PieceLen returns the standard piece length in bytes.
func (m *MetaInfo) PieceLen() int64 {
	return m.Info.PieceLength
}

// findRawValue finds a key in a top-level bencoded dictionary and returns the
// raw bencoded bytes of its value. The data must be a valid bencoded dict.
func findRawValue(data []byte, key string) ([]byte, error) {
	if len(data) == 0 || data[0] != 'd' {
		return nil, fmt.Errorf("%w: not a bencoded dictionary", ErrInvalidTorrent)
	}

	pos := 1
	for pos < len(data) {
		if data[pos] == 'e' {
			break
		}

		kStart := pos
		colonIdx := bytes.IndexByte(data[kStart:], ':')
		if colonIdx < 0 {
			return nil, fmt.Errorf("%w: invalid dict key", ErrInvalidTorrent)
		}
		colonPos := kStart + colonIdx

		keyLen, err := parseDecimal(data[kStart:colonPos])
		if err != nil {
			return nil, fmt.Errorf("%w: invalid key length", ErrInvalidTorrent)
		}

		keyStart := colonPos + 1
		keyEnd := keyStart + keyLen
		if keyEnd > len(data) {
			return nil, fmt.Errorf("%w: key exceeds data bounds", ErrInvalidTorrent)
		}

		currentKey := string(data[keyStart:keyEnd])
		pos = keyEnd

		valEnd, err := scanBencodeValue(data, pos)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidTorrent, err)
		}

		if currentKey == key {
			return data[pos:valEnd], nil
		}

		pos = valEnd
	}

	return nil, fmt.Errorf("torrent: key %q not found in dictionary", key)
}

func scanBencodeValue(data []byte, pos int) (int, error) {
	if pos >= len(data) {
		return 0, errors.New("unexpected end of data")
	}

	switch {
	case data[pos] == 'i':
		eIdx := bytes.IndexByte(data[pos+1:], 'e')
		if eIdx < 0 {
			return 0, errors.New("unterminated integer")
		}
		return pos + 1 + eIdx + 1, nil

	case data[pos] >= '0' && data[pos] <= '9':
		colonIdx := bytes.IndexByte(data[pos:], ':')
		if colonIdx < 0 {
			return 0, errors.New("invalid string")
		}
		length, err := parseDecimal(data[pos : pos+colonIdx])
		if err != nil {
			return 0, err
		}
		end := pos + colonIdx + 1 + length
		if end > len(data) {
			return 0, errors.New("string exceeds data bounds")
		}
		return end, nil

	case data[pos] == 'l' || data[pos] == 'd':
		depth := 1
		pos++
		for depth > 0 && pos < len(data) {
			switch {
			case data[pos] == 'l' || data[pos] == 'd':
				depth++
				pos++
			case data[pos] == 'e':
				depth--
				pos++
			case data[pos] == 'i':
				eIdx := bytes.IndexByte(data[pos+1:], 'e')
				if eIdx < 0 {
					return 0, errors.New("unterminated integer in nested")
				}
				pos += eIdx + 2
			case data[pos] >= '0' && data[pos] <= '9':
				colonIdx := bytes.IndexByte(data[pos:], ':')
				if colonIdx < 0 {
					return 0, errors.New("invalid string in nested")
				}
				length, err := parseDecimal(data[pos : pos+colonIdx])
				if err != nil {
					return 0, err
				}
				next := pos + colonIdx + 1 + length
				if next > len(data) {
					return 0, errors.New("string exceeds data bounds")
				}
				pos = next
			default:
				return 0, fmt.Errorf("unexpected byte 0x%02x", data[pos])
			}
		}
		if depth != 0 {
			return 0, errors.New("unterminated list/dict")
		}
		return pos, nil

	default:
		return 0, fmt.Errorf("unexpected byte 0x%02x", data[pos])
	}
}

func parseDecimal(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, errors.New("empty decimal")
	}
	i, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return 0, err
	}
	if i > int64(int(^uint(0)>>1)) {
		return 0, errors.New("decimal exceeds int range")
	}
	return int(i), nil
}
