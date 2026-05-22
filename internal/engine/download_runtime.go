package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/core"
	"github.com/smartass08/aria2go/internal/disk"
	"github.com/smartass08/aria2go/internal/hash"
)

const feedbackSpeedThreshold = 20 * 1024

type downloadIntegrity struct {
	wholeKind   hash.Kind
	wholeDigest []byte
	pieceKind   hash.Kind
	pieceHashes [][]byte
	pieceLen    int64
}

func (i downloadIntegrity) hasWholeChecksum() bool {
	return i.wholeKind != "" && len(i.wholeDigest) == i.wholeKind.Size()
}

func (i downloadIntegrity) hasPieceHashes() bool {
	return i.pieceKind != "" && i.pieceLen > 0 && len(i.pieceHashes) > 0
}

func (i downloadIntegrity) controlPieceLength(fallback int64) int64 {
	if i.hasPieceHashes() {
		return i.pieceLen
	}
	return fallback
}

func parseDownloadIntegrity(opts *config.Options) (downloadIntegrity, error) {
	var integrity downloadIntegrity
	if opts == nil || opts.Checksum == "" {
		return integrity, nil
	}
	kind, digest, err := hash.ParseChecksumSpec(opts.Checksum)
	if err != nil {
		return integrity, err
	}
	integrity.wholeKind = kind
	integrity.wholeDigest = digest
	return integrity, nil
}

func applyMetalinkIntegrity(base downloadIntegrity, entry metalinkDownloadEntry) downloadIntegrity {
	integrity := base
	if entry.PieceHashKind != "" && entry.PieceLength > 0 && len(entry.Pieces) > 0 {
		integrity.pieceKind = entry.PieceHashKind
		integrity.pieceLen = entry.PieceLength
		integrity.pieceHashes = cloneByteSlices(entry.Pieces)
		return integrity
	}
	if len(entry.Hashes) == 0 {
		return integrity
	}
	if kind, digest, ok := strongestMetalinkHash(entry.Hashes); ok {
		integrity.wholeKind = kind
		integrity.wholeDigest = append([]byte(nil), digest...)
	}
	return integrity
}

func strongestMetalinkHash(hashes map[hash.Kind][]byte) (hash.Kind, []byte, bool) {
	order := []hash.Kind{hash.SHA512, hash.SHA384, hash.SHA256, hash.SHA224, hash.SHA1, hash.MD5}
	for _, kind := range order {
		if digest, ok := hashes[kind]; ok && len(digest) == kind.Size() {
			return kind, digest, true
		}
	}
	return "", nil, false
}

func (e *Engine) loadServerStats(cfg *config.Options) error {
	if cfg == nil || cfg.ServerStatIf == "" || e.serverStats == nil {
		return nil
	}
	f, err := os.Open(cfg.ServerStatIf)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	if err := e.serverStats.Load(f); err != nil {
		return err
	}
	timeout := time.Duration(parseInt(cfg.ServerStatTimeout)) * time.Second
	if timeout > 0 {
		e.serverStats.RemoveStale(timeout)
	}
	return nil
}

func (e *Engine) saveServerStats() error {
	if e.cfg == nil || e.cfg.ServerStatOf == "" || e.serverStats == nil {
		return nil
	}
	f, err := os.Create(e.cfg.ServerStatOf)
	if err != nil {
		return err
	}
	defer f.Close()
	return e.serverStats.Save(f)
}

func (e *Engine) applyRequestGroupRuntimeOptions(rg *requestGroup, opts *config.Options) error {
	if rg == nil {
		return nil
	}
	integrity, err := parseDownloadIntegrity(opts)
	if err != nil {
		return err
	}
	rg.integrity = integrity
	rg.downloadLimit = applyThrottle(rg.downloadLimit, parseSize(opts.MaxDownloadLimit))
	rg.uploadLimit = applyThrottle(rg.uploadLimit, parseSize(opts.MaxUploadLimit))
	return nil
}

func (e *Engine) applyRequestGroupRuntimeOptionPatch(rg *requestGroup, opts *config.Options) {
	if rg == nil || opts == nil {
		return
	}
	if opts.MaxDownloadLimit != "" {
		rg.downloadLimit = applyThrottle(rg.downloadLimit, parseSize(opts.MaxDownloadLimit))
	}
	if opts.MaxUploadLimit != "" {
		rg.uploadLimit = applyThrottle(rg.uploadLimit, parseSize(opts.MaxUploadLimit))
	}
}

func applyThrottle(current *Throttle, rate int64) *Throttle {
	if current == nil {
		if rate <= 0 {
			return nil
		}
		return NewThrottle(rate)
	}
	if rate <= 0 {
		current.Stop()
		return nil
	}
	current.SetRate(rate)
	return current
}

func stopThrottle(t *Throttle) {
	if t != nil {
		t.Stop()
	}
}

func chooseAllocator(opts *config.Options, size int64) disk.Allocator {
	if opts == nil || size <= 0 {
		return disk.AllocatorNone{}
	}
	if limit := parseSize(opts.NoFileAllocationLimit); limit > 0 && size < limit {
		return disk.AllocatorNone{}
	}
	switch strings.ToLower(opts.FileAllocation) {
	case "none":
		return disk.AllocatorNone{}
	case "trunc":
		return disk.AllocatorTrunc{}
	case "falloc":
		return disk.AllocatorFalloc{}
	case "prealloc":
		return &disk.AllocatorPrealloc{}
	default:
		return &disk.AllocatorPrealloc{}
	}
}

type speedGuard struct {
	limit       int64
	start       time.Time
	startupIdle time.Duration
	host        string
	sampleStart time.Time
	sampleBytes int64
}

func newSpeedGuard(limit int64, startupIdle time.Duration, host string) *speedGuard {
	if limit <= 0 {
		return nil
	}
	now := time.Now()
	return &speedGuard{
		limit:       limit,
		start:       now,
		startupIdle: startupIdle,
		host:        host,
		sampleStart: now,
	}
}

func (g *speedGuard) Add(n int) error {
	if g == nil || n <= 0 {
		return nil
	}
	now := time.Now()
	lifetime := now.Sub(g.start)
	if lifetime <= 0 {
		g.start = now
		g.sampleStart = now
		g.sampleBytes = 0
		return nil
	}
	if lifetime < g.startupIdle {
		g.sampleStart = now
		g.sampleBytes = 0
		return nil
	}
	if g.sampleStart.IsZero() {
		g.sampleStart = now
	}
	g.sampleBytes += int64(n)
	elapsed := now.Sub(g.sampleStart)
	if elapsed < time.Second {
		return nil
	}
	speed := int64(float64(g.sampleBytes) / elapsed.Seconds())
	g.sampleStart = now
	g.sampleBytes = 0
	if speed <= g.limit {
		return fmt.Errorf("too slow downloading speed: %d <= %d(B/s), host:%s", speed, g.limit, g.host)
	}
	return nil
}

func (e *Engine) effectiveLowestSpeedLimit(rg *requestGroup, uris []string) int64 {
	if rg == nil || rg.opts == nil {
		return 0
	}
	lowest := parseSize(rg.opts.LowestSpeedLimit)
	if lowest <= 0 || !strings.EqualFold(rg.opts.URISelector, "adaptive") {
		return lowest
	}
	maxSpeed := e.serverStatMaxDownloadSpeed(uris)
	if maxSpeed > 0 && lowest > maxSpeed/4 {
		return maxSpeed / 4
	}
	if maxSpeed == 0 && lowest > 4*1024 {
		return 4 * 1024
	}
	return lowest
}

func (e *Engine) serverStatMaxDownloadSpeed(uris []string) int64 {
	var maxSpeed int64
	for _, rawURI := range uris {
		host, proto := uriHostProto(rawURI)
		if host == "" || proto == "" || e.serverStats == nil {
			continue
		}
		if stat := e.serverStats.Find(host, proto); stat != nil && stat.IsOK() {
			speed := stat.DownloadSpeed()
			if sc := stat.SingleConnectionAvgSpeed(); sc > speed {
				speed = sc
			}
			if mc := stat.MultiConnectionAvgSpeed(); mc > speed {
				speed = mc
			}
			if speed > maxSpeed {
				maxSpeed = speed
			}
		}
	}
	return maxSpeed
}

func (e *Engine) usedHosts(exclude core.GID) []string {
	if e == nil || !e.cfg.SelectLeastUsedHost {
		return nil
	}
	counts := make(map[string]int)
	e.queuesMu.Lock()
	active := append([]core.GID(nil), e.active...)
	e.queuesMu.Unlock()
	for _, gid := range active {
		if gid == exclude {
			continue
		}
		rg, ok := e.groups.getLocked(gid)
		if !ok {
			continue
		}
		for host, count := range rg.activeHosts {
			counts[host] += count
		}
		e.groups.unlock(gid)
	}
	if len(counts) == 0 {
		return nil
	}
	type hostCount struct {
		host  string
		count int
	}
	ordered := make([]hostCount, 0, len(counts))
	for host, count := range counts {
		ordered = append(ordered, hostCount{host: host, count: count})
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].count != ordered[j].count {
			return ordered[i].count < ordered[j].count
		}
		return ordered[i].host < ordered[j].host
	})
	result := make([]string, 0, len(ordered))
	for _, entry := range ordered {
		result = append(result, entry.host)
	}
	return result
}

func (e *Engine) selectDownloadURIs(rg *requestGroup, count int) []string {
	if rg == nil || count <= 0 || len(rg.uris) == 0 {
		return nil
	}
	available := filterBlockedURIs(rg.uris, rg.resumeBlockedURIs)
	selected := make([]string, 0, count)
	activeHosts := make(map[string]int)
	usedHosts := e.usedHosts(rg.gid)
	maxPerHost := rg.opts.MaxConnectionPerServer
	if maxPerHost <= 0 {
		maxPerHost = 1
	}
	reused := false

	for len(selected) < count {
		next := e.selectOneURI(rg, available, usedHosts, activeHosts, maxPerHost)
		if next == "" {
			if reused || !rg.opts.ReuseURI {
				break
			}
			available = reusableURIs(rg.uris, rg.resumeBlockedURIs, activeHosts, maxPerHost, e.serverStats)
			reused = true
			continue
		}
		selected = append(selected, next)
		host, _ := uriHostProto(next)
		if host != "" {
			activeHosts[host]++
		}
		available = removeURI(available, next)
	}
	if len(selected) == 0 {
		return append(selected, rg.uris[0])
	}
	return selected
}

func reusableURIs(uris []string, blocked map[string]struct{}, activeHosts map[string]int, maxPerHost int, stats *ServerStatMan) []string {
	reused := make([]string, 0, len(uris))
	for _, rawURI := range uris {
		if blockedURI(blocked, rawURI) {
			continue
		}
		host, proto := uriHostProto(rawURI)
		if host == "" || activeHosts[host] >= maxPerHost {
			continue
		}
		if stats != nil {
			if stat := stats.Find(host, proto); stat != nil && stat.IsError() {
				continue
			}
		}
		reused = append(reused, rawURI)
	}
	return reused
}

func filterBlockedURIs(uris []string, blocked map[string]struct{}) []string {
	if len(uris) == 0 {
		return nil
	}
	if len(blocked) == 0 {
		return append([]string(nil), uris...)
	}
	filtered := make([]string, 0, len(uris))
	for _, rawURI := range uris {
		if blockedURI(blocked, rawURI) {
			continue
		}
		filtered = append(filtered, rawURI)
	}
	return filtered
}

func blockedURI(blocked map[string]struct{}, rawURI string) bool {
	if len(blocked) == 0 {
		return false
	}
	_, ok := blocked[rawURI]
	return ok
}

func (e *Engine) selectOneURI(rg *requestGroup, available, usedHosts []string, activeHosts map[string]int, maxPerHost int) string {
	switch strings.ToLower(rg.opts.URISelector) {
	case "inorder":
		return firstUsableURI(available, activeHosts, maxPerHost)
	case "adaptive":
		if uri := e.selectAdaptiveURI(available, activeHosts, maxPerHost); uri != "" {
			return uri
		}
		return firstUsableURI(available, activeHosts, maxPerHost)
	default:
		if uri := e.selectFeedbackURI(available, usedHosts, activeHosts, maxPerHost); uri != "" {
			return uri
		}
		return firstUsableURI(available, activeHosts, maxPerHost)
	}
}

func (e *Engine) selectAdaptiveURI(available []string, activeHosts map[string]int, maxPerHost int) string {
	bestURI := ""
	bestSpeed := int64(0)
	for _, rawURI := range available {
		host, proto := uriHostProto(rawURI)
		if host == "" || activeHosts[host] >= maxPerHost {
			continue
		}
		if e.serverStats == nil {
			if bestURI == "" {
				bestURI = rawURI
			}
			continue
		}
		stat := e.serverStats.Find(host, proto)
		if stat == nil || stat.IsError() {
			if bestURI == "" {
				bestURI = rawURI
			}
			continue
		}
		speed := stat.DownloadSpeed()
		if sc := stat.SingleConnectionAvgSpeed(); sc > speed {
			speed = sc
		}
		if mc := stat.MultiConnectionAvgSpeed(); mc > speed {
			speed = mc
		}
		if speed > bestSpeed {
			bestSpeed = speed
			bestURI = rawURI
		}
	}
	return bestURI
}

func (e *Engine) selectFeedbackURI(available, usedHosts []string, activeHosts map[string]int, maxPerHost int) string {
	type statURI struct {
		speed int64
		uri   string
	}
	fast := make([]statURI, 0, min(len(available), 10))
	normal := make([]string, 0, len(available))
	for _, rawURI := range available {
		host, proto := uriHostProto(rawURI)
		if host == "" || activeHosts[host] >= maxPerHost || containsString(usedHosts, host) {
			continue
		}
		if e.serverStats == nil {
			normal = append(normal, rawURI)
			continue
		}
		stat := e.serverStats.Find(host, proto)
		if stat == nil {
			normal = append(normal, rawURI)
			continue
		}
		if stat.IsError() {
			continue
		}
		if speed := stat.DownloadSpeed(); speed > feedbackSpeedThreshold {
			fast = append(fast, statURI{speed: speed, uri: rawURI})
			if len(fast) >= 10 {
				break
			}
			continue
		}
		normal = append(normal, rawURI)
	}
	if len(fast) > 0 {
		sort.SliceStable(fast, func(i, j int) bool { return fast[i].speed > fast[j].speed })
		return fast[0].uri
	}
	if len(normal) > 0 {
		return normal[0]
	}
	for _, preferredHost := range usedHosts {
		for _, rawURI := range available {
			host, proto := uriHostProto(rawURI)
			if host == "" || activeHosts[host] >= maxPerHost || host != preferredHost {
				continue
			}
			if e.serverStats != nil {
				if stat := e.serverStats.Find(host, proto); stat != nil && stat.IsError() {
					continue
				}
			}
			return rawURI
		}
	}
	return ""
}

func firstUsableURI(available []string, activeHosts map[string]int, maxPerHost int) string {
	for _, rawURI := range available {
		host, _ := uriHostProto(rawURI)
		if host == "" || activeHosts[host] < maxPerHost {
			return rawURI
		}
	}
	return ""
}

func removeURI(uris []string, target string) []string {
	for i, rawURI := range uris {
		if rawURI == target {
			return append(uris[:i], uris[i+1:]...)
		}
	}
	return uris
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func uriHostProto(rawURI string) (string, string) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return "", ""
	}
	return u.Hostname(), strings.ToLower(u.Scheme)
}

func hostUseMap(uris []string) map[string]int {
	if len(uris) == 0 {
		return nil
	}
	out := make(map[string]int)
	for _, rawURI := range uris {
		host, _ := uriHostProto(rawURI)
		if host != "" {
			out[host]++
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func downloadAverageSpeed(rg *requestGroup) int64 {
	if rg == nil || rg.created.IsZero() || rg.completedLength <= 0 {
		return 0
	}
	elapsed := time.Since(rg.created)
	if elapsed <= 0 {
		return 0
	}
	return int64(float64(rg.completedLength) / elapsed.Seconds())
}

func (e *Engine) recordServerStatSuccess(rawURI string, speed int64, connections int) {
	host, proto := uriHostProto(rawURI)
	if host == "" || proto == "" || e.serverStats == nil || speed <= 0 {
		return
	}
	stat := e.serverStats.Find(host, proto)
	if stat == nil {
		stat = NewServerStat(host, proto)
		e.serverStats.Add(stat)
	}
	stat.IncreaseCounter()
	stat.UpdateDownloadSpeed(speed)
	if connections <= 1 {
		stat.SetSingleConnectionAvgSpeed(speed)
		return
	}
	stat.SetMultiConnectionAvgSpeed(speed)
}

func (e *Engine) markServerStatError(rawURI string) {
	host, proto := uriHostProto(rawURI)
	if host == "" || proto == "" || e.serverStats == nil {
		return
	}
	stat := e.serverStats.Find(host, proto)
	if stat == nil {
		stat = NewServerStat(host, proto)
		e.serverStats.Add(stat)
	}
	stat.SetError()
}

func (e *Engine) verifyIntegrity(ctx context.Context, rg *requestGroup, adaptor disk.Adaptor, outPath string) (string, []int, error) {
	if rg == nil {
		return "", nil, nil
	}
	if rg.integrity.hasPieceHashes() {
		if adaptor == nil {
			return "piece", nil, fmt.Errorf("piece checksum verification requires random access")
		}
		adaptor.SetPieceCount(len(rg.integrity.pieceHashes))
		for i := range rg.integrity.pieceHashes {
			adaptor.MarkPiece(i, true)
		}
		verifier := disk.NewVerifier(adaptor, rg.integrity.pieceHashes, rg.integrity.pieceKind, rg.integrity.pieceLen)
		bad, err := verifier.Verify(ctx)
		if err != nil {
			return "piece", nil, err
		}
		if len(bad) > 0 {
			for _, idx := range bad {
				e.markControlPiece(rg, idx, false)
				adaptor.MarkPiece(idx, false)
			}
			return "piece", bad, core.ErrChecksumMismatch
		}
		return "piece", nil, nil
	}
	if rg.integrity.hasWholeChecksum() {
		digest, err := hash.SumFile(outPath, rg.integrity.wholeKind)
		if err != nil {
			return "whole", nil, err
		}
		if !bytes.Equal(digest, rg.integrity.wholeDigest) {
			return "whole", nil, core.ErrChecksumMismatch
		}
	}
	return "", nil, nil
}

func (e *Engine) verifyCompletedPieces(ctx context.Context, rg *requestGroup, adaptor disk.Adaptor, indices []int) error {
	if rg == nil || !rg.integrity.hasPieceHashes() || len(indices) == 0 {
		return nil
	}
	adaptor.SetPieceCount(len(rg.integrity.pieceHashes))
	for _, idx := range indices {
		ok, err := verifyPieceIndex(ctx, adaptor, rg.integrity.pieceKind, rg.integrity.pieceLen, rg.totalLength, idx, rg.integrity.pieceHashes[idx])
		if err != nil {
			return err
		}
		if !ok {
			e.markControlPiece(rg, idx, false)
			adaptor.MarkPiece(idx, false)
			return core.ErrChecksumMismatch
		}
		adaptor.MarkPiece(idx, true)
	}
	return nil
}

func (e *Engine) verifyExistingFileIntegrity(ctx context.Context, rg *requestGroup, outPath string, size int64) error {
	if rg == nil || size <= 0 || (!rg.integrity.hasWholeChecksum() && !rg.integrity.hasPieceHashes()) {
		return nil
	}
	adaptor, err := disk.NewSingleFile(outPath, size, disk.AllocatorNone{})
	if err != nil {
		return err
	}
	defer adaptor.Close()
	_, _, err = e.verifyIntegrity(ctx, rg, adaptor, outPath)
	return err
}

func verifyPieceIndex(ctx context.Context, adaptor disk.Adaptor, kind hash.Kind, pieceLen, totalSize int64, index int, expected []byte) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}
	start := int64(index) * pieceLen
	if pieceLen <= 0 || start >= totalSize {
		return false, fmt.Errorf("engine: invalid piece %d", index)
	}
	actualLen := pieceLen
	if start+actualLen > totalSize {
		actualLen = totalSize - start
	}
	buf := make([]byte, actualLen)
	n, err := adaptor.ReadAt(buf, start)
	if err != nil && !errors.Is(err, os.ErrClosed) && !errors.Is(err, context.Canceled) {
		return false, err
	}
	if int64(n) < actualLen {
		return false, ioErrUnexpectedEOF()
	}
	h, err := hash.New(kind)
	if err != nil {
		return false, err
	}
	defer hash.PoolPut(kind, h)
	h.Write(buf[:n])
	return bytes.Equal(h.Sum(nil), expected), nil
}

func ioErrUnexpectedEOF() error { return fmt.Errorf("unexpected EOF") }

func (e *Engine) allowIntegrityRetry(rg *requestGroup) bool {
	if rg == nil || rg.opts == nil {
		return false
	}
	limit := rg.opts.MaxTries
	if limit <= 0 {
		limit = 1
	}
	if rg.integrityRetry >= limit {
		return false
	}
	rg.integrityRetry++
	return true
}

func (e *Engine) shouldRetryRealtimePieceCheck(rg *requestGroup) bool {
	if rg == nil || rg.errCode != core.ExitChecksumError {
		return false
	}
	if rg.opts == nil || !rg.opts.RealtimeChunkChecksum || !rg.integrity.hasPieceHashes() {
		return false
	}
	return e.allowIntegrityRetry(rg)
}

func (e *Engine) resetControlState(rg *requestGroup, path string) {
	if rg == nil {
		return
	}
	e.removeControlFile(rg)
	rg.controlMu.Lock()
	rg.controlPath = path
	rg.controlInfo = nil
	rg.controlLoaded = false
	rg.controlPieceBytes = nil
	rg.controlMu.Unlock()
}
