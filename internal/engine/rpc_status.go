package engine

import (
	"encoding/hex"
	"path/filepath"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/core"
	"github.com/smartass08/aria2go/internal/disk"
	btprogress "github.com/smartass08/aria2go/internal/protocol/bittorrent/progress"
	"github.com/smartass08/aria2go/internal/torrent"
	"github.com/smartass08/aria2go/internal/tracker"
)

type btStatusMetadata struct {
	announceList [][]string
	comment      string
	creationDate int64
	infoHash     string
	mode         string
	name         string
}

func cloneBittorrentMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		switch val := v.(type) {
		case [][]string:
			al := make([][]string, len(val))
			for i, tier := range val {
				t := make([]string, len(tier))
				copy(t, tier)
				al[i] = t
			}
			dst[k] = al
		case map[string]any:
			info := make(map[string]any, len(val))
			for ik, iv := range val {
				info[ik] = iv
			}
			dst[k] = info
		default:
			dst[k] = v
		}
	}
	return dst
}

func cloneStatusSnapshot(src Status) Status {
	dst := src
	if len(src.FollowedBy) > 0 {
		dst.FollowedBy = append([]core.GID(nil), src.FollowedBy...)
	}
	if len(src.Files) > 0 {
		dst.Files = make([]FileStatus, len(src.Files))
		for i, file := range src.Files {
			dst.Files[i] = file
			if len(file.URIs) > 0 {
				dst.Files[i].URIs = append([]URIStatus(nil), file.URIs...)
			}
		}
	}
	if src.Bittorrent != nil {
		dst.Bittorrent = cloneBittorrentMap(src.Bittorrent)
	}
	return dst
}

func cloneAnnounceList(src [][]string) [][]string {
	if src == nil {
		return nil
	}
	dst := make([][]string, 0, len(src))
	for _, tier := range src {
		uris := make([]string, len(tier))
		copy(uris, tier)
		dst = append(dst, uris)
	}
	return dst
}

func cloneBTStatusMetadata(src btStatusMetadata) btStatusMetadata {
	src.announceList = cloneAnnounceList(src.announceList)
	return src
}

func btStatusMetadataFromMeta(meta *torrent.MetaInfo, opts *config.Options) (btStatusMetadata, bool) {
	if meta == nil {
		return btStatusMetadata{}, false
	}

	mode := "single"
	if len(meta.Info.Files) > 0 {
		mode = "multi"
	}
	infoHash := ""
	if hash, err := meta.InfoHash(); err == nil {
		infoHash = hex.EncodeToString(hash[:])
	}
	var excludeTrackers, addTrackers []string
	if opts != nil {
		excludeTrackers = opts.BTExcludeTracker
		addTrackers = opts.BTTracker
	}
	announceList := tracker.NormalizeAnnounceTiers(meta.Announce, meta.AnnounceList, excludeTrackers, addTrackers)
	if announceList == nil {
		announceList = [][]string{}
	}
	return btStatusMetadata{
		announceList: announceList,
		comment:      meta.Comment,
		creationDate: meta.CreationDate,
		infoHash:     infoHash,
		mode:         mode,
		name:         meta.Info.Name,
	}, true
}

func (rg *requestGroup) cacheBTStatusMetadata(meta *torrent.MetaInfo, opts *config.Options) {
	btMeta, ok := btStatusMetadataFromMeta(meta, opts)
	if !ok {
		return
	}
	rg.btStatusMeta.CompareAndSwap(nil, &btMeta)
}

func (s *btSwarm) snapshotStatusMetadata(opts *config.Options) (btStatusMetadata, bool) {
	if s == nil {
		return btStatusMetadata{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return btStatusMetadataFromMeta(s.meta, opts)
}

func (e *Engine) requestGroupBTMetadata(rg *requestGroup) (btStatusMetadata, bool) {
	if rg == nil {
		return btStatusMetadata{}, false
	}
	if meta := rg.btStatusMeta.Load(); meta != nil {
		return cloneBTStatusMetadata(*meta), true
	}
	if meta := rg.btMeta.Load(); meta != nil {
		rg.cacheBTStatusMetadata(meta, rg.opts)
		if statusMeta := rg.btStatusMeta.Load(); statusMeta != nil {
			return cloneBTStatusMetadata(*statusMeta), true
		}
	}
	if swarm := rg.btSwarm.Load(); swarm != nil {
		btMeta, ok := swarm.snapshotStatusMetadata(rg.opts)
		if ok {
			rg.btStatusMeta.CompareAndSwap(nil, &btMeta)
			if statusMeta := rg.btStatusMeta.Load(); statusMeta != nil {
				return cloneBTStatusMetadata(*statusMeta), true
			}
		}
	}
	return btStatusMetadata{}, false
}

func (e *Engine) makeStoppedStatus(rg *requestGroup, state core.Status, errCode core.ErrorCode, errMsg string) Status {
	status := cloneStatusSnapshot(*e.makeStatus(rg))
	status.Status = state
	status.ErrorCode = errCode
	status.ErrorMessage = errMsg
	status.CompletedLength = e.stoppedCompletedLength(rg, status.TotalLength, status.PieceLength, status.CompletedLength)
	status.DownloadSpeed = 0
	status.UploadSpeed = 0
	status.Connections = 0
	e.applyStoppedStatusSourceTruth(rg, &status, state, errCode)
	return status
}

func (e *Engine) stoppedCompletedLength(rg *requestGroup, totalLength, pieceLength, fallback int64) int64 {
	info, bitfield := e.statusControlSnapshot(rg)
	if info != nil {
		if info.TotalLength > 0 {
			totalLength = info.TotalLength
		}
		if info.PieceLength > 0 {
			pieceLength = info.PieceLength
		}
	}
	if completed, ok := filteredCompletedLengthFromBitfield(totalLength, pieceLength, bitfield, rg.fileEntries); ok {
		return completed
	}
	completed, ok := completedLengthFromBitfield(totalLength, pieceLength, bitfield)
	if !ok {
		return fallback
	}
	return completed
}

func completedLengthFromBitfield(totalLength, pieceLength int64, bitfield []byte) (int64, bool) {
	if totalLength <= 0 || pieceLength <= 0 || len(bitfield) == 0 {
		return 0, false
	}

	pieces := controlNumPieces(totalLength, pieceLength)
	var completed int64
	for i := 0; i < pieces; i++ {
		if controlBit(bitfield, i) {
			completed += controlPieceSize(totalLength, pieceLength, i)
		}
	}
	return completed, true
}

func filteredCompletedLengthFromBitfield(totalLength, pieceLength int64, bitfield []byte, entries []disk.FileEntry) (int64, bool) {
	if totalLength <= 0 || pieceLength <= 0 || len(bitfield) == 0 || len(entries) <= 1 {
		return 0, false
	}

	allSelected := true
	for _, entry := range entries {
		if !entry.Requested {
			allSelected = false
			break
		}
	}
	if allSelected {
		return 0, false
	}

	pieces := controlNumPieces(totalLength, pieceLength)
	selected := make([]bool, pieces)
	for _, entry := range entries {
		if !entry.Requested || entry.Length <= 0 {
			continue
		}
		start := entry.Offset
		end := entry.Offset + entry.Length
		if start < 0 {
			start = 0
		}
		if end > totalLength {
			end = totalLength
		}
		if start >= end {
			continue
		}

		firstPiece := int(start / pieceLength)
		lastPiece := int((end - 1) / pieceLength)
		for piece := firstPiece; piece <= lastPiece && piece < pieces; piece++ {
			selected[piece] = true
		}
	}

	var completed int64
	for piece := 0; piece < pieces; piece++ {
		if selected[piece] && controlBit(bitfield, piece) {
			completed += controlPieceSize(totalLength, pieceLength, piece)
		}
	}
	return completed, true
}

func (e *Engine) applyStoppedStatusSourceTruth(rg *requestGroup, status *Status, state core.Status, errCode core.ErrorCode) {
	if rg == nil || status == nil {
		return
	}

	if status.PieceLength <= 0 {
		status.PieceLength = controlPieceLength(rg.opts)
		if status.TotalLength > 0 && status.NumPieces == 0 {
			status.NumPieces = int64(controlNumPieces(status.TotalLength, status.PieceLength))
		}
	}

	if !isUserRemovedURIResult(rg, state, errCode) {
		return
	}

	if len(status.Files) == 0 {
		status.Files = []FileStatus{{
			Index:    1,
			Selected: true,
		}}
	}

	if isUserRemovedPreStartURIResult(rg, state, errCode) && rg.filePathFromURI {
		status.Files[0].Path = ""
	}
	status.Files[0].URIs = stoppedResultURIs(rg)
}

func isUserRemovedURIResult(rg *requestGroup, state core.Status, errCode core.ErrorCode) bool {
	if rg == nil {
		return false
	}
	return state == core.StatusRemoved &&
		errCode == core.ExitRemoved &&
		rg.haltReason == haltReasonUserRequest &&
		len(rg.uris) > 0 &&
		len(rg.torrent) == 0 &&
		len(rg.metalinkData) == 0
}

func isUserRemovedPreStartURIResult(rg *requestGroup, state core.Status, errCode core.ErrorCode) bool {
	if rg == nil {
		return false
	}
	return isUserRemovedURIResult(rg, state, errCode) &&
		rg.completedLength == 0 &&
		rg.totalLength == 0 &&
		len(rg.fileEntries) == 0
}

func stoppedResultURIs(rg *requestGroup) []URIStatus {
	if rg == nil || len(rg.uris) == 0 {
		return nil
	}

	usedURIs := stoppedResultUsedURIs(rg)
	uris := make([]URIStatus, 0, len(rg.uris)+len(usedURIs))
	for _, uri := range usedURIs {
		uris = append(uris, URIStatus{URI: uri, Status: "used"})
	}
	skipped := make(map[string]int, len(usedURIs))
	for _, uri := range usedURIs {
		uris = append(uris, URIStatus{URI: uri, Status: "waiting"})
		skipped[uri]++
	}
	for _, uri := range rg.uris {
		if skipped[uri] > 0 {
			skipped[uri]--
			continue
		}
		uris = append(uris, URIStatus{URI: uri, Status: "waiting"})
	}
	return uris
}

func stoppedResultUsedURIs(rg *requestGroup) []string {
	// Full aria2 fidelity requires distinct spent/request-pool/in-flight URI
	// state. Until that exists, keep only the conformance-covered current URI.
	if rg.activeURI != "" {
		return []string{rg.activeURI}
	}
	return []string{rg.uris[0]}
}

func (e *Engine) statusControlSnapshot(rg *requestGroup) (*btprogress.Info, []byte) {
	rg.controlMu.Lock()
	info := cloneControlInfo(rg.controlInfo)
	adaptor := rg.adaptor
	rg.controlMu.Unlock()

	if adaptor != nil {
		if bitfield := adaptor.Bitfield(); len(bitfield) > 0 {
			return info, bitfield
		}
	}
	if info != nil && len(info.Bitfield) > 0 {
		return info, append([]byte(nil), info.Bitfield...)
	}
	return info, nil
}

func (e *Engine) groupFileEntries(rg *requestGroup) []disk.FileEntry {
	if len(rg.fileEntries) > 0 {
		files := make([]disk.FileEntry, len(rg.fileEntries))
		copy(files, rg.fileEntries)
		return files
	}

	path := firstFilePathForRPC(rg)
	if path == "" {
		return nil
	}

	return []disk.FileEntry{{
		Name:      path,
		Length:    rg.totalLength,
		Offset:    0,
		Requested: true,
	}}
}

func firstFilePathForRPC(rg *requestGroup) string {
	if rg == nil {
		return ""
	}

	path := rg.filePath
	if path == "" && len(rg.uris) > 0 {
		path = defaultOutputPathFromURI(rg.uris[0])
	}
	if path == "" {
		return ""
	}
	if rg.inMemory {
		return "[MEMORY]" + filepath.Base(path)
	}
	if filepath.IsAbs(path) || rg.opts == nil || rg.opts.Dir == "" {
		return path
	}
	return filepath.Join(rg.opts.Dir, path)
}

func fileEntryPathForRPC(rg *requestGroup, entry disk.FileEntry) string {
	path := entry.Name
	if path == "" {
		return firstFilePathForRPC(rg)
	}
	if rg != nil && rg.inMemory {
		return "[MEMORY]" + filepath.Base(path)
	}
	if filepath.IsAbs(path) || rg == nil || rg.opts == nil || rg.opts.Dir == "" {
		return path
	}
	return filepath.Join(rg.opts.Dir, path)
}

func fileCompletedLength(entry disk.FileEntry, totalLength, pieceLength int64, bitfield []byte, fallback int64) int64 {
	if entry.Length <= 0 {
		if fallback < 0 {
			return 0
		}
		return fallback
	}
	if pieceLength <= 0 || len(bitfield) == 0 || totalLength <= 0 {
		if fallback < 0 {
			return 0
		}
		if fallback > entry.Length {
			return entry.Length
		}
		return fallback
	}

	end := entry.Offset + entry.Length
	if end > totalLength {
		end = totalLength
	}
	if entry.Offset >= end {
		return 0
	}

	var completed int64
	firstPiece := int(entry.Offset / pieceLength)
	lastPiece := int((end - 1) / pieceLength)
	for piece := firstPiece; piece <= lastPiece; piece++ {
		if !controlBit(bitfield, piece) {
			continue
		}
		pieceStart := int64(piece) * pieceLength
		pieceEnd := pieceStart + controlPieceSize(totalLength, pieceLength, piece)
		overlapStart := max64(entry.Offset, pieceStart)
		overlapEnd := min64(end, pieceEnd)
		if overlapEnd > overlapStart {
			completed += overlapEnd - overlapStart
		}
	}
	return completed
}

func (e *Engine) groupFileURIs(rg *requestGroup, fileIndex int) []URIStatus {
	if rg == nil || fileIndex != 0 || len(rg.uris) == 0 {
		return nil
	}

	usedURIs := e.statusUsedURIs(rg)
	uris := make([]URIStatus, 0, len(rg.uris))
	skipped := make(map[string]int, len(usedURIs))
	for _, uri := range usedURIs {
		uris = append(uris, URIStatus{URI: uri, Status: "used"})
		skipped[uri]++
	}
	for _, uri := range rg.uris {
		if skipped[uri] > 0 {
			skipped[uri]--
			continue
		}
		uris = append(uris, URIStatus{URI: uri, Status: "waiting"})
	}
	return uris
}

func (e *Engine) statusUsedURIs(rg *requestGroup) []string {
	if rg == nil {
		return nil
	}

	if len(rg.activeURIs) > 0 {
		return append([]string(nil), rg.activeURIs...)
	}
	if rg.activeURI != "" {
		if rg.uriUsed {
			return []string{rg.activeURI}
		}
		return nil
	}
	if len(rg.uris) == 0 {
		return nil
	}
	if rg.uriUsed || rg.state == core.StatusActive {
		return []string{rg.uris[0]}
	}
	return nil
}

func (e *Engine) buildFileStatus(rg *requestGroup) []FileStatus {
	files := e.groupFileEntries(rg)
	if len(files) == 0 {
		return nil
	}

	info, bitfield := e.statusControlSnapshot(rg)
	totalLength := rg.totalLength
	pieceLength := int64(0)
	if info != nil {
		if totalLength == 0 {
			totalLength = info.TotalLength
		}
		pieceLength = info.PieceLength
	}

	result := make([]FileStatus, 0, len(files))
	for i, entry := range files {
		requested := entry.Requested
		if len(rg.fileEntries) == 0 {
			requested = true
		}

		fallbackCompleted := int64(0)
		if len(files) == 1 {
			fallbackCompleted = rg.completedLength
		}

		result = append(result, FileStatus{
			Index:           i + 1,
			Path:            fileEntryPathForRPC(rg, entry),
			Length:          entry.Length,
			CompletedLength: fileCompletedLength(entry, totalLength, pieceLength, bitfield, fallbackCompleted),
			Selected:        requested,
			URIs:            e.groupFileURIs(rg, i),
		})
	}
	return result
}

func (e *Engine) makeStatus(rg *requestGroup) *Status {
	dir := ""
	if rg.opts != nil {
		dir = rg.opts.Dir
	}
	st := rg.state
	if rg.pauseReq && st == core.StatusWaiting {
		st = core.StatusPaused
	}

	info, bitfield := e.statusControlSnapshot(rg)
	totalLength := rg.totalLength
	uploadLength := int64(0)
	pieceLength := int64(0)
	numPieces := int64(0)
	infoHash := ""
	bitfieldHex := ""
	if info != nil {
		if totalLength == 0 {
			totalLength = info.TotalLength
		}
		uploadLength = info.UploadLength
		pieceLength = info.PieceLength
		numPieces = int64(controlNumPieces(info.TotalLength, info.PieceLength))
		if len(info.InfoHash) > 0 {
			infoHash = hex.EncodeToString(info.InfoHash)
		}
	}
	if pieceLength <= 0 {
		pieceLength = controlPieceLength(rg.opts)
		if totalLength > 0 && numPieces == 0 {
			numPieces = int64(controlNumPieces(totalLength, pieceLength))
		}
	}
	if len(bitfield) > 0 {
		bitfieldHex = hex.EncodeToString(bitfield)
	}
	bittorrent := map[string]any(nil)
	btMeta, hasBTMeta := e.requestGroupBTMetadata(rg)
	if infoHash == "" && hasBTMeta {
		infoHash = btMeta.infoHash
	}
	if hasBTMeta {
		bittorrent = make(map[string]any)
		announceList := cloneAnnounceList(btMeta.announceList)
		if announceList == nil {
			announceList = [][]string{}
		}
		bittorrent["announceList"] = announceList

		if btMeta.comment != "" {
			bittorrent["comment"] = btMeta.comment
		}
		if btMeta.creationDate != 0 {
			bittorrent["creationDate"] = btMeta.creationDate
		}
		if btMeta.mode != "" {
			bittorrent["mode"] = btMeta.mode
		}
		bittorrent["info"] = map[string]any{"name": btMeta.name}
	}

	return &Status{
		GID:             rg.gid,
		Status:          st,
		TotalLength:     totalLength,
		CompletedLength: rg.completedLength,
		UploadLength:    uploadLength,
		DownloadSpeed:   rg.downloadSpeed,
		UploadSpeed:     rg.uploadSpeed,
		InfoHash:        infoHash,
		NumSeeders:      int64(rg.numSeeders),
		Connections:     rg.numConnections,
		ErrorCode:       rg.errCode,
		ErrorMessage:    rg.errMsg,
		FollowedBy:      append([]core.GID(nil), rg.followedBy...),
		BelongsTo:       rg.belongsTo,
		Following:       rg.following,
		Dir:             dir,
		Files:           e.buildFileStatus(rg),
		Seeder:          rg.seeder,
		Bittorrent:      bittorrent,
		Bitfield:        bitfieldHex,
		PieceLength:     pieceLength,
		NumPieces:       numPieces,
	}
}
