package tracker

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/smartass08/aria2go/internal/bencode"
	httpdriver "github.com/smartass08/aria2go/internal/protocol/http"
)

// AnnounceHTTP sends an HTTP tracker announce request (BEP 3) and
// returns the parsed bencoded response.
func AnnounceHTTP(ctx context.Context, urlStr string, req AnnounceRequest, httpDriver *httpdriver.Driver) (*AnnounceResponse, error) {
	if err := req.ValidateEvent(); err != nil {
		return nil, err
	}

	announceURL, err := buildHTTPAnnounceURL(urlStr, req)
	if err != nil {
		return nil, fmt.Errorf("tracker: %w", err)
	}

	body, err := httpDriver.Download(ctx, announceURL, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("tracker: %w: %w", ErrNetwork, err)
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("tracker: %w: %w", ErrNetwork, err)
	}

	return parseHTTPAnnounceResponse(data)
}

// ScrapeHTTP sends an HTTP scrape request (BEP 48) and returns
// swarm metadata keyed by info hash.
func ScrapeHTTP(ctx context.Context, urlStr string, infoHashes [][20]byte, httpDriver *httpdriver.Driver) (map[[20]byte]ScrapeData, error) {
	scrapeURL, err := buildScrapeURL(urlStr, infoHashes)
	if err != nil {
		return nil, fmt.Errorf("tracker: %w", err)
	}

	body, err := httpDriver.Download(ctx, scrapeURL, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("tracker: %w: %w", ErrNetwork, err)
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("tracker: %w: %w", ErrNetwork, err)
	}

	return parseScrapeResponse(data)
}

func buildHTTPAnnounceURL(baseURL string, req AnnounceRequest) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid announce URL %q: %w", baseURL, err)
	}

	q := queryBuilder{raw: u.RawQuery}
	q.SetBytes("info_hash", req.InfoHash[:])
	q.SetBytes("peer_id", req.PeerID[:])
	q.Set("uploaded", strconv.FormatInt(req.Uploaded, 10))
	q.Set("downloaded", strconv.FormatInt(req.Downloaded, 10))
	q.Set("left", strconv.FormatInt(req.Left, 10))
	q.Set("compact", "1")
	q.Set("no_peer_id", "1")

	if req.Port != 0 {
		q.Set("port", strconv.Itoa(int(req.Port)))
	}
	if req.Event != "" {
		q.Set("event", req.Event)
	}
	if req.NumWant > 0 {
		q.Set("numwant", strconv.Itoa(req.NumWant))
	}
	q.SetBytes("key", []byte(httpKey(req.PeerID)))
	if req.TrackerID != "" {
		q.Set("trackerid", req.TrackerID)
	}
	switch req.CryptoSupport {
	case "supportcrypto":
		q.Set("supportcrypto", "1")
	case "requirecrypto":
		q.Set("requirecrypto", "1")
	}
	if req.ExternalIP != "" {
		q.Set("ip", req.ExternalIP)
	}

	u.RawQuery = q.String()
	return u.String(), nil
}

type queryBuilder struct {
	raw   string
	parts []string
}

func (q *queryBuilder) Set(key, value string) {
	q.SetBytes(key, []byte(value))
}

func (q *queryBuilder) SetBytes(key string, value []byte) {
	q.parts = append(q.parts, trackerQueryEscape([]byte(key))+"="+trackerQueryEscape(value))
}

func (q *queryBuilder) String() string {
	if q.raw == "" {
		return strings.Join(q.parts, "&")
	}
	if len(q.parts) == 0 {
		return q.raw
	}
	return q.raw + "&" + strings.Join(q.parts, "&")
}

func trackerQueryEscape(data []byte) string {
	var b strings.Builder
	for _, c := range data {
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte("0123456789ABCDEF"[c>>4])
			b.WriteByte("0123456789ABCDEF"[c&0x0f])
		}
	}
	return b.String()
}

func parseHTTPAnnounceResponse(data []byte) (*AnnounceResponse, error) {
	var v bencode.Value
	if err := bencode.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBencode, err)
	}

	d, ok := v.(*bencode.DictVal)
	if !ok {
		return nil, fmt.Errorf("%w: expected dict at top level", ErrBencode)
	}

	resp := &AnnounceResponse{}
	if val, ok := d.Get("failure reason"); ok {
		if sv, ok := val.(bencode.StringVal); ok {
			return nil, fmt.Errorf("%w: %s", ErrInvalidResp, sv.S)
		}
		return nil, ErrInvalidResp
	}

	if val, ok := d.Get("warning message"); ok {
		if sv, ok := val.(bencode.StringVal); ok {
			resp.WarningMessage = sv.S
		}
	}

	if val, ok := d.Get("interval"); ok {
		if iv, ok := val.(bencode.IntVal); ok && iv.I > 0 {
			resp.Interval = int32(iv.I)
		}
	}
	if val, ok := d.Get("min interval"); ok {
		if iv, ok := val.(bencode.IntVal); ok && iv.I > 0 {
			resp.MinInterval = int32(iv.I)
			if resp.MinInterval > resp.Interval {
				resp.MinInterval = resp.Interval
			}
		}
	}
	if resp.MinInterval == 0 {
		resp.MinInterval = resp.Interval
	}
	if val, ok := d.Get("tracker id"); ok {
		if sv, ok := val.(bencode.StringVal); ok {
			resp.TrackerID = sv.S
		}
	}
	if val, ok := d.Get("complete"); ok {
		if iv, ok := val.(bencode.IntVal); ok {
			resp.Complete = int32(iv.I)
		}
	}
	if val, ok := d.Get("incomplete"); ok {
		if iv, ok := val.(bencode.IntVal); ok {
			resp.Incomplete = int32(iv.I)
		}
	}

	if val, ok := d.Get("peers"); ok {
		peers, err := decodePeersValue(val, false)
		if err != nil {
			return nil, err
		}
		resp.Peers = peers
	}
	if val, ok := d.Get("peers6"); ok {
		peers, err := decodePeersValue(val, true)
		if err != nil {
			return nil, err
		}
		resp.Peers6 = peers
	}

	return resp, nil
}

func decodePeersValue(v bencode.Value, ipv6 bool) ([]PeerInfo, error) {
	switch val := v.(type) {
	case bencode.StringVal:
		return decodeCompactPeers([]byte(val.S), ipv6)
	case bencode.ListVal:
		var peers []PeerInfo
		for _, elem := range val.L {
			pi := decodePeerDict(elem)
			if pi != nil {
				peers = append(peers, *pi)
			}
		}
		return peers, nil
	}
	return nil, nil
}

func decodePeerDict(v bencode.Value) *PeerInfo {
	d, ok := v.(*bencode.DictVal)
	if !ok {
		return nil
	}

	ipVal, ok := d.Get("ip")
	if !ok {
		return nil
	}
	ipStr, ok := ipVal.(bencode.StringVal)
	if !ok {
		return nil
	}

	portVal, ok := d.Get("port")
	if !ok {
		return nil
	}
	portInt, ok := portVal.(bencode.IntVal)
	if !ok {
		return nil
	}
	if portInt.I <= 0 || portInt.I > 65535 {
		return nil
	}

	ip := net.ParseIP(ipStr.S)
	if ip == nil {
		return nil
	}

	return &PeerInfo{IP: ip, Port: uint16(portInt.I)}
}

func decodeCompactPeers(data []byte, ipv6 bool) ([]PeerInfo, error) {
	unit := 6
	if ipv6 {
		unit = 18
	}

	if len(data)%unit != 0 {
		return nil, fmt.Errorf("%w: compact peers length %d is not a multiple of %d", ErrInvalidResp, len(data), unit)
	}

	peers := make([]PeerInfo, 0, len(data)/unit)
	for i := 0; i < len(data); i += unit {
		if ipv6 {
			ip := make(net.IP, 16)
			copy(ip, data[i:i+16])
			port := binary.BigEndian.Uint16(data[i+16 : i+18])
			peers = append(peers, PeerInfo{IP: ip, Port: port})
		} else {
			ip := net.IPv4(data[i], data[i+1], data[i+2], data[i+3])
			port := binary.BigEndian.Uint16(data[i+4 : i+6])
			peers = append(peers, PeerInfo{IP: ip, Port: port})
		}
	}
	return peers, nil
}

func buildScrapeURL(baseURL string, infoHashes [][20]byte) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid announce URL %q: %w", baseURL, err)
	}

	path := u.Path
	if idx := strings.LastIndex(path, "/announce"); idx >= 0 {
		path = path[:idx] + "/scrape" + path[idx+len("/announce"):]
	} else if idx := strings.LastIndex(path, "announce"); idx >= 0 {
		path = path[:idx] + "scrape" + path[idx+len("announce"):]
	}
	u.Path = path
	u.RawPath = ""

	q := queryBuilder{raw: u.RawQuery}
	for _, ih := range infoHashes {
		q.SetBytes("info_hash", ih[:])
	}
	u.RawQuery = q.String()
	return u.String(), nil
}

func parseScrapeResponse(data []byte) (map[[20]byte]ScrapeData, error) {
	var v bencode.Value
	if err := bencode.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBencode, err)
	}

	d, ok := v.(*bencode.DictVal)
	if !ok {
		return nil, fmt.Errorf("%w: expected dict at top level", ErrBencode)
	}

	if val, ok := d.Get("failure reason"); ok {
		if sv, ok := val.(bencode.StringVal); ok {
			return nil, fmt.Errorf("%w: %s", ErrInvalidResp, sv.S)
		}
		return nil, ErrInvalidResp
	}

	filesVal, ok := d.Get("files")
	if !ok {
		return nil, fmt.Errorf("%w: missing 'files' key in scrape response", ErrBencode)
	}
	filesDict, ok := filesVal.(*bencode.DictVal)
	if !ok {
		return nil, fmt.Errorf("%w: 'files' is not a dict", ErrBencode)
	}

	result := make(map[[20]byte]ScrapeData)
	for _, key := range filesDict.Keys {
		if len(key) != 20 {
			return nil, fmt.Errorf("%w: scrape file key has length %d", ErrInvalidResp, len(key))
		}
		var ih [20]byte
		copy(ih[:], key)

		val, _ := filesDict.Get(key)
		entry, ok := val.(*bencode.DictVal)
		if !ok {
			return nil, fmt.Errorf("%w: scrape entry for %s is not a dict", ErrInvalidResp, hex.EncodeToString(ih[:]))
		}

		var sd ScrapeData
		v, ok := entry.Get("complete")
		if !ok {
			return nil, fmt.Errorf("%w: scrape entry for %s missing complete", ErrInvalidResp, hex.EncodeToString(ih[:]))
		}
		iv, ok := v.(bencode.IntVal)
		if !ok {
			return nil, fmt.Errorf("%w: scrape complete for %s is not an int", ErrInvalidResp, hex.EncodeToString(ih[:]))
		}
		sd.Complete = int32(iv.I)

		v, ok = entry.Get("incomplete")
		if !ok {
			return nil, fmt.Errorf("%w: scrape entry for %s missing incomplete", ErrInvalidResp, hex.EncodeToString(ih[:]))
		}
		iv, ok = v.(bencode.IntVal)
		if !ok {
			return nil, fmt.Errorf("%w: scrape incomplete for %s is not an int", ErrInvalidResp, hex.EncodeToString(ih[:]))
		}
		sd.Incomplete = int32(iv.I)

		v, ok = entry.Get("downloaded")
		if !ok {
			return nil, fmt.Errorf("%w: scrape entry for %s missing downloaded", ErrInvalidResp, hex.EncodeToString(ih[:]))
		}
		iv, ok = v.(bencode.IntVal)
		if !ok {
			return nil, fmt.Errorf("%w: scrape downloaded for %s is not an int", ErrInvalidResp, hex.EncodeToString(ih[:]))
		}
		sd.Downloaded = int32(iv.I)
		result[ih] = sd
	}

	return result, nil
}

func httpKey(peerID [20]byte) string {
	return string(peerID[12:])
}

// hexEncode returns a string of the info hash as hexadecimal, for logging.
func hexEncode(ih [20]byte) string {
	return hex.EncodeToString(ih[:])
}
