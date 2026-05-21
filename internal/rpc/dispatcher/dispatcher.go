package dispatcher

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/subtle"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/core"
	ariabase64 "github.com/smartass08/aria2go/internal/encoding/base64"
	"github.com/smartass08/aria2go/internal/engine"
	"github.com/smartass08/aria2go/internal/protocol/metalink"
	"github.com/smartass08/aria2go/internal/torrent"
)

// MethodErr is the error type returned by RPC method handlers.
// It maps to the aria2 RPC error format (code 1 with message).
type MethodErr struct {
	Message string
}

// Error returns the error string.
func (e *MethodErr) Error() string { return e.Message }

// newMethodErr creates a MethodErr with a formatted message.
func newMethodErr(format string, args ...interface{}) error {
	return &MethodErr{Message: fmt.Sprintf(format, args...)}
}

// NotifySink receives notification events from the dispatcher.
// name is the notification name (e.g. "aria2.onDownloadStart").
// params is the notification payload (e.g. {"gid": "..."}).
type NotifySink func(name string, params map[string]interface{})

// notification names list, matching aria2 1.37.0.
var notificationNames = []string{
	"aria2.onDownloadStart",
	"aria2.onDownloadPause",
	"aria2.onDownloadStop",
	"aria2.onDownloadComplete",
	"aria2.onDownloadError",
	"aria2.onBtDownloadComplete",
}

// eventToNotification maps engine event kinds to notification names.
var eventToNotification = map[core.EventKind]string{
	core.EvStart:      "aria2.onDownloadStart",
	core.EvPause:      "aria2.onDownloadPause",
	core.EvStop:       "aria2.onDownloadStop",
	core.EvComplete:   "aria2.onDownloadComplete",
	core.EvError:      "aria2.onDownloadError",
	core.EvBTComplete: "aria2.onBtDownloadComplete",
}

// dispatchFunc is the signature for RPC method handlers.
type dispatchFunc func(d *Dispatcher, ctx context.Context, params []interface{}) (interface{}, error)

// methodNames list, matching aria2 1.37.0 (alphabetical).
var methodNames = []string{
	"aria2.addMetalink",
	"aria2.addTorrent",
	"aria2.addUri",
	"aria2.changeGlobalOption",
	"aria2.changeOption",
	"aria2.changePosition",
	"aria2.changeUri",
	"aria2.forcePause",
	"aria2.forcePauseAll",
	"aria2.forceRemove",
	"aria2.forceShutdown",
	"aria2.getFiles",
	"aria2.getGlobalOption",
	"aria2.getGlobalStat",
	"aria2.getOption",
	"aria2.getPeers",
	"aria2.getServers",
	"aria2.getSessionInfo",
	"aria2.getUris",
	"aria2.getVersion",
	"aria2.pause",
	"aria2.pauseAll",
	"aria2.purgeDownloadResult",
	"aria2.remove",
	"aria2.removeDownloadResult",
	"aria2.saveSession",
	"aria2.shutdown",
	"aria2.tellActive",
	"aria2.tellStopped",
	"aria2.tellStatus",
	"aria2.tellWaiting",
	"aria2.unpause",
	"aria2.unpauseAll",
	"system.listMethods",
	"system.listNotifications",
	"system.multicall",
}

// authlessMethods are methods that do not require a top-level RPC secret token.
var authlessMethods = map[string]bool{
	"system.listMethods":       true,
	"system.listNotifications": true,
	"system.multicall":         true,
}

// Config configures the RPC dispatcher.
type Config struct {
	// Secret is the --rpc-secret token. If empty, auth is skipped.
	Secret string
	// ReadOnly prevents mutating methods (addUri, remove, etc.).
	ReadOnly bool
}

// mutatingMethods are methods that modify state and are blocked in read-only mode.
var mutatingMethods = map[string]bool{
	"aria2.addMetalink":          true,
	"aria2.addTorrent":           true,
	"aria2.addUri":               true,
	"aria2.changeGlobalOption":   true,
	"aria2.changeOption":         true,
	"aria2.changePosition":       true,
	"aria2.changeUri":            true,
	"aria2.forcePause":           true,
	"aria2.forcePauseAll":        true,
	"aria2.forceRemove":          true,
	"aria2.forceShutdown":        true,
	"aria2.pause":                true,
	"aria2.pauseAll":             true,
	"aria2.purgeDownloadResult":  true,
	"aria2.remove":               true,
	"aria2.removeDownloadResult": true,
	"aria2.saveSession":          true,
	"aria2.shutdown":             true,
	"aria2.unpause":              true,
	"aria2.unpauseAll":           true,
}

// Dispatcher dispatches RPC method calls to the download engine.
type Dispatcher struct {
	engine *engine.Engine
	cfg    Config

	mu    sync.Mutex
	sinks []NotifySink

	engineSubOnceMu  sync.Mutex
	engineSubStarted bool
}

// New creates a new Dispatcher.
func New(e *engine.Engine, cfg Config) *Dispatcher {
	return &Dispatcher{
		engine: e,
		cfg:    cfg,
		sinks:  make([]NotifySink, 0),
	}
}

// ListMethods returns all available method names, sorted alphabetically.
func (d *Dispatcher) ListMethods() []string {
	result := make([]string, len(methodNames))
	copy(result, methodNames)
	return result
}

// ListNotifications returns all available notification names.
func (d *Dispatcher) ListNotifications() []string {
	result := make([]string, len(notificationNames))
	copy(result, notificationNames)
	return result
}

// authCheck validates the token against the configured secret using
// constant-time comparison. Returns nil if auth passes, error otherwise.
// If no secret is configured or the method is authless, auth passes.
func (d *Dispatcher) authCheck(method, token string) error {
	if authlessMethods[method] {
		return nil
	}
	if d.cfg.Secret == "" {
		return nil
	}
	if subtle.ConstantTimeCompare([]byte(d.cfg.Secret), []byte(token)) != 1 {
		return newMethodErr("Unauthorized")
	}
	return nil
}

// Call executes an RPC method. token is the raw secret extracted from the
// first positional parameter (already stripped of the "token:" prefix).
// params are the remaining positional arguments.
func (d *Dispatcher) Call(ctx context.Context, token, method string, params []interface{}) (interface{}, error) {
	if err := d.authCheck(method, token); err != nil {
		return nil, err
	}
	if d.cfg.ReadOnly && mutatingMethods[method] {
		return nil, newMethodErr("RPC server is in read-only mode")
	}
	return d.dispatch(ctx, method, params)
}

// dispatchTable maps method names to handler functions, built at init time.
var dispatchTable map[string]dispatchFunc

// dispatch routes a method call by name.
func (d *Dispatcher) dispatch(ctx context.Context, method string, params []interface{}) (interface{}, error) {
	fn, ok := dispatchTable[method]
	if !ok {
		return nil, newMethodErr("No such method: %s", method)
	}
	return fn(d, ctx, params)
}

// ---------------------------------------------------------------------------
// Parameter helpers
// ---------------------------------------------------------------------------

func paramString(params []interface{}, index int) (string, error) {
	if index >= len(params) {
		return "", newMethodErr("parameter at %d is required but missing", index)
	}
	s, ok := params[index].(string)
	if !ok {
		return "", newMethodErr("the parameter at %d has wrong type", index)
	}
	return s, nil
}

func optString(params []interface{}, index int) (string, bool) {
	if index >= len(params) {
		return "", false
	}
	s, ok := params[index].(string)
	return s, ok
}

func paramInt(params []interface{}, index int) (int64, error) {
	if index >= len(params) {
		return 0, newMethodErr("parameter at %d is required but missing", index)
	}
	v, ok := toInt64(params[index])
	if !ok {
		return 0, newMethodErr("the parameter at %d has wrong type", index)
	}
	return v, nil
}

func optInt(params []interface{}, index int) (int64, bool) {
	if index >= len(params) {
		return 0, false
	}
	v, ok := toInt64(params[index])
	return v, ok
}

func toInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case float64:
		return int64(n), true
	}
	return 0, false
}

func paramStringArray(params []interface{}, index int) ([]string, error) {
	if index >= len(params) {
		return nil, newMethodErr("parameter at %d is required but missing", index)
	}
	arr, ok := params[index].([]interface{})
	if !ok {
		return nil, newMethodErr("the parameter at %d has wrong type", index)
	}
	result := make([]string, 0, len(arr))
	for i, elem := range arr {
		s, ok := elem.(string)
		if !ok {
			return nil, newMethodErr("the parameter at %d element at %d has wrong type", index, i)
		}
		result = append(result, s)
	}
	return result, nil
}

func optStringArray(params []interface{}, index int) []string {
	if index >= len(params) {
		return nil
	}
	arr, ok := params[index].([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, elem := range arr {
		s, ok := elem.(string)
		if !ok {
			continue
		}
		result = append(result, s)
	}
	return result
}

func paramMap(params []interface{}, index int) (map[string]interface{}, error) {
	if index >= len(params) {
		return nil, newMethodErr("parameter at %d is required but missing", index)
	}
	m, ok := params[index].(map[string]interface{})
	if !ok {
		return nil, newMethodErr("the parameter at %d has wrong type", index)
	}
	return m, nil
}

func optMap(params []interface{}, index int) map[string]interface{} {
	if index >= len(params) {
		return nil
	}
	m, _ := params[index].(map[string]interface{})
	return m
}

// parseGID parses a GID from a string parameter (hex or decimal).
func parseGID(s string) (core.GID, error) {
	return core.ParseGID(s)
}

// gidString formats a GID as a 16-character hex string.
func gidString(g core.GID) string {
	return g.Hex()
}

// ---------------------------------------------------------------------------
// Method implementations
// ---------------------------------------------------------------------------

func (d *Dispatcher) addUri(ctx context.Context, params []interface{}) (interface{}, error) {
	uris, err := paramStringArray(params, 0)
	if err != nil {
		return nil, err
	}
	if len(uris) == 0 {
		return nil, newMethodErr("uris must not be empty")
	}
	optsMap := optMap(params, 1)
	position, posGiven := optInt(params, 2)
	if posGiven && position < 0 {
		return nil, newMethodErr("Position must be greater than or equal to 0.")
	}
	pos := int(position)

	opts := mapToOptions(optsMap)

	spec := engine.AddSpec{
		URIs:        uris,
		Options:     opts,
		Position:    pos,
		PositionSet: posGiven,
	}
	gid, err := d.engine.Add(spec)
	if err != nil {
		return nil, newMethodErr("%v", err)
	}
	return gidString(gid), nil
}

func (d *Dispatcher) addTorrent(ctx context.Context, params []interface{}) (interface{}, error) {
	torrentData, err := paramString(params, 0)
	if err != nil {
		return nil, err
	}
	torrentBytes, err := decodeBase64Param("torrent", torrentData)
	if err != nil {
		return nil, err
	}
	if _, err := torrent.Load(torrentBytes); err != nil {
		return nil, newMethodErr("Bencode decoding failed")
	}
	uris := optStringArray(params, 1)
	optsMap := optMap(params, 2)
	position, posGiven := optInt(params, 3)
	if posGiven && position < 0 {
		return nil, newMethodErr("Position must be greater than or equal to 0.")
	}
	pos := int(position)

	opts := mapToOptions(optsMap)
	if opts == nil {
		opts = &config.Options{}
	}
	globalOpts := d.engine.GetGlobalOption()

	// rpc-save-upload-metadata: save uploaded .torrent file.
	torrentFile := ""
	if globalOpts.RPCSaveUploadMetadata {
		hash := sha1.Sum(torrentBytes)
		torrentFile = filepath.Join(optsValue(opts, globalOpts, "dir"),
			fmt.Sprintf("%x.torrent", hash))
		if err := os.WriteFile(torrentFile, torrentBytes, 0644); err == nil {
			if opts.TorrentFile == "" {
				opts.TorrentFile = torrentFile
			}
		}
	}

	spec := engine.AddSpec{
		URIs:        uris,
		Options:     opts,
		Torrent:     torrentBytes,
		Position:    pos,
		PositionSet: posGiven,
	}
	gid, err := d.engine.Add(spec)
	if err != nil {
		return nil, newMethodErr("%v", err)
	}
	return gidString(gid), nil
}

func (d *Dispatcher) addMetalink(ctx context.Context, params []interface{}) (interface{}, error) {
	metalinkData, err := paramString(params, 0)
	if err != nil {
		return nil, err
	}
	metalinkBytes, err := decodeBase64Param("metalink", metalinkData)
	if err != nil {
		return nil, err
	}
	metalinkDoc, err := metalink.Parse(bytes.NewReader(metalinkBytes))
	if err != nil {
		return nil, newMethodErr("Could not parse Metalink XML document.")
	}
	if len(metalinkDoc.Files) == 0 {
		return []string{}, nil
	}
	optsMap := optMap(params, 1)
	position, posGiven := optInt(params, 2)
	if posGiven && position < 0 {
		return nil, newMethodErr("Position must be greater than or equal to 0.")
	}
	pos := int(position)

	opts := mapToOptions(optsMap)
	if opts == nil {
		opts = &config.Options{}
	}
	globalOpts := d.engine.GetGlobalOption()

	// rpc-save-upload-metadata: save uploaded .meta4 file.
	if globalOpts.RPCSaveUploadMetadata {
		hash := sha1.Sum(metalinkBytes)
		filename := filepath.Join(optsValue(opts, globalOpts, "dir"),
			fmt.Sprintf("%x.meta4", hash))
		if err := os.WriteFile(filename, metalinkBytes, 0644); err == nil {
			if opts.MetalinkFile == "" {
				opts.MetalinkFile = filename
			}
		}
	}

	spec := engine.AddSpec{
		Options:     opts,
		Metalink:    metalinkBytes,
		Position:    pos,
		PositionSet: posGiven,
	}
	gid, err := d.engine.Add(spec)
	if err != nil {
		return nil, newMethodErr("%v", err)
	}
	return []string{gidString(gid)}, nil
}

func decodeBase64Param(name, value string) ([]byte, error) {
	decoded, err := ariabase64.Decode(value)
	if err != nil {
		return nil, newMethodErr("%s data must be base64-encoded", name)
	}
	return decoded, nil
}

// optsValue returns the value for a named option from reqOpts, falling back
// to globalOpts. Used when constructing file paths for rpc-save-upload-metadata.
func optsValue(reqOpts, globalOpts *config.Options, name string) string {
	switch name {
	case "dir":
		if reqOpts != nil && reqOpts.Dir != "" {
			return reqOpts.Dir
		}
		if globalOpts != nil {
			return globalOpts.Dir
		}
	}
	return ""
}

func (d *Dispatcher) remove(ctx context.Context, params []interface{}) (interface{}, error) {
	gidStr, err := paramString(params, 0)
	if err != nil {
		return nil, err
	}
	gid, err := parseGID(gidStr)
	if err != nil {
		return nil, newMethodErr("invalid GID: %v", err)
	}
	if err := d.engine.Remove(gid, false); err != nil {
		return nil, newMethodErr("%v", err)
	}
	return gidString(gid), nil
}

func (d *Dispatcher) forceRemove(ctx context.Context, params []interface{}) (interface{}, error) {
	gidStr, err := paramString(params, 0)
	if err != nil {
		return nil, err
	}
	gid, err := parseGID(gidStr)
	if err != nil {
		return nil, newMethodErr("invalid GID: %v", err)
	}
	if err := d.engine.Remove(gid, true); err != nil {
		return nil, newMethodErr("%v", err)
	}
	return gidString(gid), nil
}

func (d *Dispatcher) pause(ctx context.Context, params []interface{}) (interface{}, error) {
	gidStr, err := paramString(params, 0)
	if err != nil {
		return nil, err
	}
	gid, err := parseGID(gidStr)
	if err != nil {
		return nil, newMethodErr("invalid GID: %v", err)
	}
	if err := d.engine.Pause(gid, false); err != nil {
		return nil, newMethodErr("%v", err)
	}
	return gidString(gid), nil
}

func (d *Dispatcher) forcePause(ctx context.Context, params []interface{}) (interface{}, error) {
	gidStr, err := paramString(params, 0)
	if err != nil {
		return nil, err
	}
	gid, err := parseGID(gidStr)
	if err != nil {
		return nil, newMethodErr("invalid GID: %v", err)
	}
	if err := d.engine.Pause(gid, true); err != nil {
		return nil, newMethodErr("%v", err)
	}
	return gidString(gid), nil
}

func (d *Dispatcher) pauseAll(ctx context.Context, params []interface{}) (interface{}, error) {
	// Iterate all active + waiting and pause each.
	active := d.engine.TellActive()
	waiting := d.engine.TellWaiting(0, 10000)
	for _, s := range active {
		_ = d.engine.Pause(s.GID, false)
	}
	for _, s := range waiting {
		if s.Status == core.StatusWaiting {
			_ = d.engine.Pause(s.GID, false)
		}
	}
	return "OK", nil
}

func (d *Dispatcher) forcePauseAll(ctx context.Context, params []interface{}) (interface{}, error) {
	active := d.engine.TellActive()
	waiting := d.engine.TellWaiting(0, 10000)
	for _, s := range active {
		_ = d.engine.Pause(s.GID, true)
	}
	for _, s := range waiting {
		if s.Status == core.StatusWaiting {
			_ = d.engine.Pause(s.GID, true)
		}
	}
	return "OK", nil
}

func (d *Dispatcher) unpause(ctx context.Context, params []interface{}) (interface{}, error) {
	gidStr, err := paramString(params, 0)
	if err != nil {
		return nil, err
	}
	gid, err := parseGID(gidStr)
	if err != nil {
		return nil, newMethodErr("invalid GID: %v", err)
	}
	if err := d.engine.Resume(gid); err != nil {
		return nil, newMethodErr("%v", err)
	}
	return gidString(gid), nil
}

func (d *Dispatcher) unpauseAll(ctx context.Context, params []interface{}) (interface{}, error) {
	// TellWaiting includes paused downloads.
	waiting := d.engine.TellWaiting(0, 10000)
	for _, s := range waiting {
		if s.Status == core.StatusPaused {
			_ = d.engine.Resume(s.GID)
		}
	}
	return "OK", nil
}

func (d *Dispatcher) tellStatus(ctx context.Context, params []interface{}) (interface{}, error) {
	gidStr, err := paramString(params, 0)
	if err != nil {
		return nil, err
	}
	gid, err := parseGID(gidStr)
	if err != nil {
		return nil, newMethodErr("invalid GID: %v", err)
	}
	keys := optStringArray(params, 1)

	return d.engine.TellStatusKeys(gid, keys)
}

func (d *Dispatcher) tellActive(ctx context.Context, params []interface{}) (interface{}, error) {
	keys := optStringArray(params, 0)
	return d.engine.TellActiveKeys(keys), nil
}

func (d *Dispatcher) tellWaiting(ctx context.Context, params []interface{}) (interface{}, error) {
	offset, err := paramInt(params, 0)
	if err != nil {
		return nil, err
	}
	num, err := paramInt(params, 1)
	if err != nil {
		return nil, err
	}
	keys := optStringArray(params, 2)

	total := d.engine.TellWaitingKeys(0, 100000, keys)
	if offset < 0 {
		return tellPaginatedReverse(total, int(offset), int(num)), nil
	}
	return tellPaginatedForward(total, int(offset), int(num)), nil
}

func (d *Dispatcher) tellStopped(ctx context.Context, params []interface{}) (interface{}, error) {
	offset, err := paramInt(params, 0)
	if err != nil {
		return nil, err
	}
	num, err := paramInt(params, 1)
	if err != nil {
		return nil, err
	}
	keys := optStringArray(params, 2)

	total := d.engine.TellStoppedKeys(0, 100000, keys)
	if offset < 0 {
		return tellPaginatedReverse(total, int(offset), int(num)), nil
	}
	return tellPaginatedForward(total, int(offset), int(num)), nil
}

func tellPaginatedForward(maps []map[string]interface{}, offset, num int) []map[string]interface{} {
	if offset >= len(maps) || num <= 0 {
		return []map[string]interface{}{}
	}
	end := offset + num
	if end > len(maps) {
		end = len(maps)
	}
	return maps[offset:end]
}

func tellPaginatedReverse(maps []map[string]interface{}, offset, num int) []map[string]interface{} {
	n := len(maps)
	if n == 0 || num <= 0 {
		return []map[string]interface{}{}
	}
	// negative offset: -1 is last element
	idx := n + offset // offset is negative
	if idx < 0 {
		return []map[string]interface{}{}
	}
	start := idx - (num - 1)
	if start < 0 {
		start = 0
	}
	result := make([]map[string]interface{}, 0, idx-start+1)
	for i := idx; i >= start; i-- {
		result = append(result, maps[i])
	}
	return result
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func (d *Dispatcher) getUris(ctx context.Context, params []interface{}) (interface{}, error) {
	gidStr, err := paramString(params, 0)
	if err != nil {
		return nil, err
	}
	gid, err := parseGID(gidStr)
	if err != nil {
		return nil, newMethodErr("invalid GID: %v", err)
	}
	status, err := d.engine.TellStatus(gid)
	if err != nil {
		return nil, newMethodErr("%v", err)
	}

	var uris []map[string]interface{}
	for _, f := range status.Files {
		for _, u := range f.URIs {
			uris = append(uris, map[string]interface{}{
				"uri":    u.URI,
				"status": u.Status,
			})
		}
	}
	return uris, nil
}

func (d *Dispatcher) getFiles(ctx context.Context, params []interface{}) (interface{}, error) {
	gidStr, err := paramString(params, 0)
	if err != nil {
		return nil, err
	}
	gid, err := parseGID(gidStr)
	if err != nil {
		return nil, newMethodErr("invalid GID: %v", err)
	}
	status, err := d.engine.TellStatus(gid)
	if err != nil {
		return nil, newMethodErr("%v", err)
	}

	var files []map[string]interface{}
	for _, f := range status.Files {
		fm := map[string]interface{}{
			"index":           fmt.Sprintf("%d", f.Index),
			"path":            f.Path,
			"length":          fmt.Sprintf("%d", f.Length),
			"completedLength": fmt.Sprintf("%d", f.CompletedLength),
			"selected":        boolStr(f.Selected),
		}
		uris := make([]map[string]interface{}, len(f.URIs))
		for i, u := range f.URIs {
			uris[i] = map[string]interface{}{
				"uri":    u.URI,
				"status": u.Status,
			}
		}
		fm["uris"] = uris
		files = append(files, fm)
	}
	return files, nil
}

func (d *Dispatcher) getPeers(ctx context.Context, params []interface{}) (interface{}, error) {
	gidStr, err := paramString(params, 0)
	if err != nil {
		return nil, err
	}
	gid, err := parseGID(gidStr)
	if err != nil {
		return nil, newMethodErr("invalid GID: %v", err)
	}
	status, err := d.engine.TellStatus(gid)
	if err != nil || isTerminalStatus(status.Status) {
		return nil, newMethodErr("No peer data is available for GID#%s", gidString(gid))
	}
	// Non-BitTorrent downloads have no peer storage in aria2 and return [].
	return []interface{}{}, nil
}

func (d *Dispatcher) getServers(ctx context.Context, params []interface{}) (interface{}, error) {
	gidStr, err := paramString(params, 0)
	if err != nil {
		return nil, err
	}
	gid, err := parseGID(gidStr)
	if err != nil {
		return nil, newMethodErr("invalid GID: %v", err)
	}
	status, err := d.engine.TellStatus(gid)
	if err != nil {
		return nil, newMethodErr("%v", err)
	}
	if status.Status != core.StatusActive {
		return nil, newMethodErr("No active download for GID#%s", gidString(gid))
	}

	result := make([]interface{}, 0, len(status.Files))
	for _, f := range status.Files {
		servers := make([]interface{}, 0, len(f.URIs))
		for _, u := range f.URIs {
			currentURI := ""
			if u.Status == "used" {
				currentURI = u.URI
			}
			servers = append(servers, map[string]interface{}{
				"uri":           u.URI,
				"currentUri":    currentURI,
				"downloadSpeed": fmt.Sprintf("%d", status.DownloadSpeed),
			})
		}
		result = append(result, map[string]interface{}{
			"index":   fmt.Sprintf("%d", f.Index),
			"servers": servers,
		})
	}
	return result, nil
}

func isTerminalStatus(status core.Status) bool {
	switch status {
	case core.StatusComplete, core.StatusError, core.StatusRemoved:
		return true
	default:
		return false
	}
}

func (d *Dispatcher) getOption(ctx context.Context, params []interface{}) (interface{}, error) {
	gidStr, err := paramString(params, 0)
	if err != nil {
		return nil, err
	}
	gid, err := parseGID(gidStr)
	if err != nil {
		return nil, newMethodErr("invalid GID: %v", err)
	}
	opts, err := d.engine.GetOption(gid)
	if err != nil {
		return nil, newMethodErr("%v", err)
	}
	return downloadOptionsToMap(opts), nil
}

func (d *Dispatcher) changeOption(ctx context.Context, params []interface{}) (interface{}, error) {
	gidStr, err := paramString(params, 0)
	if err != nil {
		return nil, err
	}
	gid, err := parseGID(gidStr)
	if err != nil {
		return nil, newMethodErr("invalid GID: %v", err)
	}
	optsMap, err := paramMap(params, 1)
	if err != nil {
		return nil, err
	}
	opts := mapToOptions(optsMap)
	if err := d.engine.ChangeOption(gid, opts); err != nil {
		return nil, newMethodErr("%v", err)
	}
	return "OK", nil
}

func (d *Dispatcher) getGlobalOption(ctx context.Context, params []interface{}) (interface{}, error) {
	opts := d.engine.GetGlobalOption()
	if opts == nil {
		return map[string]interface{}{}, nil
	}
	return optionsToMap(opts), nil
}

func (d *Dispatcher) changeGlobalOption(ctx context.Context, params []interface{}) (interface{}, error) {
	optsMap, err := paramMap(params, 0)
	if err != nil {
		return nil, err
	}
	opts := mapToOptions(optsMap)
	if err := d.engine.ChangeGlobalOption(opts); err != nil {
		return nil, newMethodErr("%v", err)
	}
	return "OK", nil
}

func (d *Dispatcher) changePosition(ctx context.Context, params []interface{}) (interface{}, error) {
	gidStr, err := paramString(params, 0)
	if err != nil {
		return nil, err
	}
	gid, err := parseGID(gidStr)
	if err != nil {
		return nil, newMethodErr("invalid GID: %v", err)
	}
	pos, err := paramInt(params, 1)
	if err != nil {
		return nil, err
	}
	how, err := paramString(params, 2)
	if err != nil {
		return nil, err
	}
	switch how {
	case "POS_SET", "POS_CUR", "POS_END":
		// valid
	default:
		return nil, newMethodErr("Illegal argument.")
	}
	destPos, err := d.engine.ChangePosition(gid, int(pos), how)
	if err != nil {
		return nil, newMethodErr("%v", err)
	}
	return destPos, nil
}

func (d *Dispatcher) changeUri(ctx context.Context, params []interface{}) (interface{}, error) {
	gidStr, err := paramString(params, 0)
	if err != nil {
		return nil, err
	}
	gid, err := parseGID(gidStr)
	if err != nil {
		return nil, newMethodErr("invalid GID: %v", err)
	}
	fileIndex, err := paramInt(params, 1)
	if err != nil {
		return nil, err
	}
	if fileIndex < 1 {
		return nil, newMethodErr("The integer parameter at 1 has invalid value: the value must be greater than or equal to 1.")
	}
	delURIs, err := paramStringArray(params, 2)
	if err != nil {
		return nil, err
	}
	addURIs, err := paramStringArray(params, 3)
	if err != nil {
		return nil, err
	}
	pos := -1
	posGiven := false
	if p, ok := optInt(params, 4); ok {
		if p < 0 {
			return nil, newMethodErr("Position must be greater than or equal to 0.")
		}
		pos = int(p)
		posGiven = true
	}

	removed, added, err := d.engine.ChangeURIWithPosition(gid, int(fileIndex), delURIs, addURIs, pos, posGiven)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, newMethodErr("Cannot remove URIs from GID#%s", gidString(gid))
		}
		if strings.Contains(err.Error(), "fileIndex") {
			return nil, newMethodErr("fileIndex is out of range")
		}
		return nil, newMethodErr("%v", err)
	}
	return []interface{}{removed, added}, nil
}

func (d *Dispatcher) purgeDownloadResult(ctx context.Context, params []interface{}) (interface{}, error) {
	d.engine.PurgeDownloadResult()
	return "OK", nil
}

func (d *Dispatcher) removeDownloadResult(ctx context.Context, params []interface{}) (interface{}, error) {
	gidStr, err := paramString(params, 0)
	if err != nil {
		return nil, err
	}
	gid, err := parseGID(gidStr)
	if err != nil {
		return nil, newMethodErr("invalid GID: %v", err)
	}
	if err := d.engine.RemoveDownloadResult(gid); err != nil {
		return nil, newMethodErr("Could not remove download result of GID#%s", gidString(gid))
	}
	return "OK", nil
}

func (d *Dispatcher) getGlobalStat(ctx context.Context, params []interface{}) (interface{}, error) {
	gs := d.engine.GetGlobalStat()
	return map[string]interface{}{
		"downloadSpeed":   fmt.Sprintf("%d", gs.DownloadSpeed),
		"uploadSpeed":     fmt.Sprintf("%d", gs.UploadSpeed),
		"numActive":       fmt.Sprintf("%d", gs.NumActive),
		"numWaiting":      fmt.Sprintf("%d", gs.NumWaiting),
		"numStopped":      fmt.Sprintf("%d", gs.NumStopped),
		"numStoppedTotal": fmt.Sprintf("%d", gs.NumStoppedTotal),
	}, nil
}

func (d *Dispatcher) getVersion(ctx context.Context, params []interface{}) (interface{}, error) {
	return map[string]interface{}{
		"version": "1.37.0",
		"enabledFeatures": []string{
			"Async DNS",
			"BitTorrent",
			"HTTPS",
			"Metalink",
			"XML-RPC",
			"GZip",
			"Message Digest",
			"Firefox3 Cookie",
			"WebSocket",
			"SFTP",
		},
	}, nil
}

func (d *Dispatcher) getSessionInfo(ctx context.Context, params []interface{}) (interface{}, error) {
	return map[string]interface{}{
		"sessionId": d.engine.SessionID(),
	}, nil
}

func (d *Dispatcher) shutdown(ctx context.Context, params []interface{}) (interface{}, error) {
	d.engine.ShutdownDelayed(false)
	return "OK", nil
}

func (d *Dispatcher) forceShutdown(ctx context.Context, params []interface{}) (interface{}, error) {
	d.engine.ShutdownDelayed(true)
	return "OK", nil
}

func (d *Dispatcher) saveSession(ctx context.Context, params []interface{}) (interface{}, error) {
	if err := d.engine.SaveSession(); err != nil {
		return nil, newMethodErr("%v", err)
	}
	return "OK", nil
}

func (d *Dispatcher) systemMulticall(ctx context.Context, params []interface{}) (interface{}, error) {
	methodSpecs, err := paramGenericArray(params, 0)
	if err != nil {
		return nil, err
	}

	results := make([]interface{}, 0, len(methodSpecs))
	for _, spec := range methodSpecs {
		specMap, ok := spec.(map[string]interface{})
		if !ok {
			results = append(results, multicallError("system.multicall expected struct."))
			continue
		}

		methodName, ok := specMap["methodName"].(string)
		if !ok {
			results = append(results, multicallError("Missing methodName."))
			continue
		}

		if methodName == "system.multicall" {
			results = append(results, multicallError("Recursive system.multicall forbidden."))
			continue
		}

		subParams := toStringArray(specMap["params"])
		// Each sub-call independently provides the token.
		token, subParams := extractToken(subParams)

		// Check auth for this sub-call independently.
		if authErr := d.authCheck(methodName, token); authErr != nil {
			results = append(results, multicallError(authErr.Error()))
			continue
		}

		result, err := d.dispatch(ctx, methodName, subParams)
		if err != nil {
			results = append(results, multicallError(err.Error()))
		} else {
			// Success: wrap in single-element array (multicall convention).
			results = append(results, []interface{}{result})
		}
	}

	return results, nil
}

func multicallError(message string) map[string]interface{} {
	return map[string]interface{}{
		"code":    int64(1),
		"message": message,
	}
}

func (d *Dispatcher) listMethods() []string {
	return d.ListMethods()
}

func (d *Dispatcher) listNotifications() []string {
	return d.ListNotifications()
}

// paramGenericArray extracts a []interface{} at param index.
func paramGenericArray(params []interface{}, index int) ([]interface{}, error) {
	if index >= len(params) {
		return nil, newMethodErr("parameter at %d is required but missing", index)
	}
	arr, ok := params[index].([]interface{})
	if !ok {
		return nil, newMethodErr("the parameter at %d has wrong type", index)
	}
	return arr, nil
}

// toStringArray converts an interface{} that may be []interface{} of strings
// to a []interface{} suitable for RPC params.
func toStringArray(v interface{}) []interface{} {
	if v == nil {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	return arr
}

// extractToken strips the "token:" prefix from the first element if present.
func extractToken(params []interface{}) (string, []interface{}) {
	if len(params) == 0 {
		return "", params
	}
	s, ok := params[0].(string)
	if !ok {
		return "", params
	}
	const prefix = "token:"
	if len(s) > len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], params[1:]
	}
	return "", params
}

// ---------------------------------------------------------------------------
// Options conversion helpers
// ---------------------------------------------------------------------------

// optFieldInfo pre-computes reflection info for config.Options fields.
type optFieldInfo struct {
	index   int
	jsonTag string
	kind    reflect.Kind
}

var optFields []optFieldInfo

func init() {
	t := reflect.TypeOf(config.Options{})
	optFieldByName = make(map[string]optFieldInfo, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		info := optFieldInfo{
			index:   i,
			jsonTag: tag,
			kind:    f.Type.Kind(),
		}
		optFields = append(optFields, info)
		optFieldByName[tag] = info
	}
}

// mapToOptions converts a map[string]interface{} from RPC params to config.Options.
func mapToOptions(m map[string]interface{}) *config.Options {
	if m == nil {
		return nil
	}
	opts := &config.Options{}
	v := reflect.ValueOf(opts).Elem()
	for _, fi := range optFields {
		raw, ok := m[fi.jsonTag]
		if !ok {
			continue
		}
		s, ok := raw.(string)
		if !ok {
			continue
		}
		f := v.Field(fi.index)
		setFieldByKind(f, fi.kind, s)
		opts.MarkExplicit(fi.jsonTag)
	}
	return opts
}

func setFieldByKind(f reflect.Value, kind reflect.Kind, val string) {
	switch kind {
	case reflect.String:
		f.SetString(val)
	case reflect.Int:
		n, err := strconv.Atoi(val)
		if err == nil {
			f.SetInt(int64(n))
		}
	case reflect.Bool:
		f.SetBool(val == "true" || val == "1")
	case reflect.Slice:
		// Only []string slices are supported for now.
		if f.Type().Elem().Kind() == reflect.String && val != "" {
			f.Set(reflect.Append(f, reflect.ValueOf(val)))
		}
	}
}

// hiddenOption is the set of aria2 options that exist internally
// but are not exposed via getGlobalOption. Matching aria2's behavior
// where these options have non-zero defaults but are hidden from RPC.
var hiddenOption = map[string]bool{
	"max-http-pipelining":     true,
	"dns-timeout":             true,
	"bt-timeout":              true,
	"bt-request-timeout":      true,
	"bt-keep-alive-interval":  true,
	"peer-connection-timeout": true,
	"select-least-used-host":  true,
	"startup-idle-time":       true,
	"dht-listen-addr":         true,
}

// optionsToMap converts config.Options to a map[string]interface{} with
// defined option values, matching aria2's getGlobalOption behavior. Excludes
// rpc-secret for security and options aria2 does not expose globally.
func optionsToMap(o *config.Options) map[string]interface{} {
	if o == nil {
		return map[string]interface{}{}
	}
	v := reflect.ValueOf(o).Elem()
	m := map[string]interface{}{}
	explicit := explicitNameSet(o)
	for _, fi := range optFields {
		field := v.Field(fi.index)
		if !isGlobalOptionDefined(fi.jsonTag, explicit) && !isGlobalOptionDefinedByValue(fi.jsonTag, field, fi.kind) {
			continue
		}
		m[fi.jsonTag] = formatOptionField(fi.jsonTag, field, fi.kind)
	}
	if globalDefaultOption["help"] {
		m["help"] = "#basic"
	}
	return m
}

func isGlobalOptionDefined(name string, explicit map[string]bool) bool {
	if globalExcludedOption[name] || hiddenOption[name] {
		return false
	}
	return globalDefaultOption[name] || (explicit[name] && !requestOnlyOption[name])
}

func isGlobalOptionDefinedByValue(name string, field reflect.Value, kind reflect.Kind) bool {
	if globalExcludedOption[name] || hiddenOption[name] || requestOnlyOption[name] {
		return false
	}
	switch name {
	case "header":
		return kind == reflect.Slice && field.Len() > 0
	case "ca-certificate":
		return kind == reflect.String && field.String() != ""
	default:
		return false
	}
}

func explicitNameSet(o *config.Options) map[string]bool {
	names := o.ExplicitNames()
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

func explicitOptionsToMap(o *config.Options) map[string]interface{} {
	if o == nil {
		return map[string]interface{}{}
	}
	v := reflect.ValueOf(o).Elem()
	m := map[string]interface{}{}
	for _, name := range o.ExplicitNames() {
		field, ok := optFieldByName[name]
		if !ok || !isGlobalOptionDefined(field.jsonTag, map[string]bool{name: true}) {
			continue
		}
		m[field.jsonTag] = formatOptionField(field.jsonTag, v.Field(field.index), field.kind)
	}
	return m
}

func downloadOptionsToMap(o *config.Options) map[string]interface{} {
	if o == nil {
		return map[string]interface{}{}
	}
	v := reflect.ValueOf(o).Elem()
	m := map[string]interface{}{}
	for _, fi := range optFields {
		if !downloadOption[fi.jsonTag] {
			continue
		}
		field := v.Field(fi.index)
		if fi.jsonTag == "out" && isZeroField(field, fi.kind) {
			continue
		}
		m[fi.jsonTag] = formatOptionField(fi.jsonTag, field, fi.kind)
	}
	return m
}

func isZeroField(f reflect.Value, kind reflect.Kind) bool {
	switch kind {
	case reflect.String:
		return f.String() == ""
	case reflect.Int:
		return f.Int() == 0
	case reflect.Bool:
		return !f.Bool()
	case reflect.Slice:
		return f.Len() == 0
	}
	return true
}

var optFieldByName map[string]optFieldInfo

var globalExcludedOption = map[string]bool{
	"rpc-passwd": true,
	"rpc-secret": true,
	"rpc-user":   true,
}

var requestOnlyOption = map[string]bool{
	"checksum":      true,
	"gid":           true,
	"index-out":     true,
	"metalink-file": true,
	"out":           true,
	"select-file":   true,
	"torrent-file":  true,
	"uri":           true,
	"version":       true,
}

var globalDefaultOption = map[string]bool{
	"allow-overwrite":                  true,
	"allow-piece-length-change":        true,
	"always-resume":                    true,
	"async-dns":                        true,
	"auto-file-renaming":               true,
	"auto-save-interval":               true,
	"bt-detach-seed-only":              true,
	"bt-enable-hook-after-hash-check":  true,
	"bt-enable-lpd":                    true,
	"bt-force-encryption":              true,
	"bt-hash-check-seed":               true,
	"bt-load-saved-metadata":           true,
	"bt-max-open-files":                true,
	"bt-max-peers":                     true,
	"bt-metadata-only":                 true,
	"bt-min-crypto-level":              true,
	"bt-remove-unselected-file":        true,
	"bt-request-peer-speed-limit":      true,
	"bt-require-crypto":                true,
	"bt-save-metadata":                 true,
	"bt-seed-unverified":               true,
	"bt-stop-timeout":                  true,
	"bt-tracker-connect-timeout":       true,
	"bt-tracker-interval":              true,
	"bt-tracker-timeout":               true,
	"check-certificate":                true,
	"check-integrity":                  true,
	"conditional-get":                  true,
	"conf-path":                        true,
	"connect-timeout":                  true,
	"console-log-level":                true,
	"content-disposition-default-utf8": true,
	"continue":                         true,
	"daemon":                           true,
	"deferred-input":                   true,
	"dht-file-path":                    true,
	"dht-file-path6":                   true,
	"dht-listen-port":                  true,
	"dht-message-timeout":              true,
	"dir":                              true,
	"disable-ipv6":                     true,
	"disk-cache":                       true,
	"download-result":                  true,
	"dry-run":                          true,
	"dscp":                             true,
	"enable-color":                     true,
	"enable-dht":                       true,
	"enable-dht6":                      true,
	"enable-http-keep-alive":           true,
	"enable-http-pipelining":           true,
	"enable-mmap":                      true,
	"enable-peer-exchange":             true,
	"enable-rpc":                       true,
	"event-poll":                       true,
	"file-allocation":                  true,
	"follow-metalink":                  true,
	"follow-torrent":                   true,
	"force-save":                       true,
	"ftp-pasv":                         true,
	"ftp-reuse-connection":             true,
	"ftp-type":                         true,
	"hash-check-only":                  true,
	"help":                             true,
	"http-accept-gzip":                 true,
	"http-auth-challenge":              true,
	"http-no-cache":                    true,
	"human-readable":                   true,
	"keep-unfinished-download-result":  true,
	"listen-port":                      true,
	"log-level":                        true,
	"lowest-speed-limit":               true,
	"max-concurrent-downloads":         true,
	"max-connection-per-server":        true,
	"max-download-limit":               true,
	"max-download-result":              true,
	"max-file-not-found":               true,
	"max-mmap-limit":                   true,
	"max-overall-download-limit":       true,
	"max-overall-upload-limit":         true,
	"max-resume-failure-tries":         true,
	"max-tries":                        true,
	"max-upload-limit":                 true,
	"metalink-enable-unique-protocol":  true,
	"metalink-preferred-protocol":      true,
	"min-split-size":                   true,
	"min-tls-version":                  true,
	"netrc-path":                       true,
	"no-conf":                          true,
	"no-file-allocation-limit":         true,
	"no-netrc":                         true,
	"no-want-digest-header":            true,
	"optimize-concurrent-downloads":    true,
	"parameterized-uri":                true,
	"pause-metadata":                   true,
	"peer-agent":                       true,
	"peer-id-prefix":                   true,
	"piece-length":                     true,
	"proxy-method":                     true,
	"quiet":                            true,
	"realtime-chunk-checksum":          true,
	"remote-time":                      true,
	"remove-control-file":              true,
	"retry-wait":                       true,
	"reuse-uri":                        true,
	"rlimit-nofile":                    true,
	"rpc-allow-origin-all":             true,
	"rpc-listen-all":                   true,
	"rpc-listen-port":                  true,
	"rpc-max-request-size":             true,
	"rpc-save-upload-metadata":         true,
	"rpc-secure":                       true,
	"save-not-found":                   true,
	"save-session-interval":            true,
	"seed-ratio":                       true,
	"server-stat-timeout":              true,
	"show-console-readout":             true,
	"show-files":                       true,
	"socket-recv-buffer-size":          true,
	"split":                            true,
	"stderr":                           true,
	"stop":                             true,
	"stream-piece-selector":            true,
	"summary-interval":                 true,
	"timeout":                          true,
	"truncate-console-readout":         true,
	"uri-selector":                     true,
	"use-head":                         true,
	"user-agent":                       true,
}

func formatOptionField(name string, f reflect.Value, kind reflect.Kind) string {
	switch kind {
	case reflect.String:
		if normalized, ok := normalizeUnitOption(name, f.String()); ok {
			return normalized
		}
		if normalized, ok := normalizeProxyOption(name, f.String()); ok {
			return normalized
		}
		return f.String()
	case reflect.Int:
		return strconv.FormatInt(f.Int(), 10)
	case reflect.Bool:
		if f.Bool() {
			return "true"
		}
		return "false"
	case reflect.Slice:
		n := f.Len()
		if name == "header" {
			var sb strings.Builder
			for i := 0; i < n; i++ {
				sb.WriteString(f.Index(i).String())
				sb.WriteByte('\n')
			}
			return sb.String()
		}
		var sb strings.Builder
		sb.WriteByte('[')
		for i := 0; i < n; i++ {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(f.Index(i).String())
		}
		sb.WriteByte(']')
		return sb.String()
	}
	return ""
}

var unitOption = map[string]bool{
	"min-split-size":              true,
	"max-overall-download-limit":  true,
	"max-download-limit":          true,
	"max-overall-upload-limit":    true,
	"max-upload-limit":            true,
	"lowest-speed-limit":          true,
	"bt-request-peer-speed-limit": true,
	"rpc-max-request-size":        true,
	"disk-cache":                  true,
	"no-file-allocation-limit":    true,
	"max-mmap-limit":              true,
	"piece-length":                true,
	"socket-recv-buffer-size":     true,
}

var proxyOption = map[string]bool{
	"all-proxy":   true,
	"http-proxy":  true,
	"https-proxy": true,
	"ftp-proxy":   true,
}

var downloadOption = map[string]bool{
	"allow-overwrite":                  true,
	"allow-piece-length-change":        true,
	"always-resume":                    true,
	"async-dns":                        true,
	"auto-file-renaming":               true,
	"bt-enable-hook-after-hash-check":  true,
	"bt-enable-lpd":                    true,
	"bt-force-encryption":              true,
	"bt-hash-check-seed":               true,
	"bt-load-saved-metadata":           true,
	"bt-max-peers":                     true,
	"bt-metadata-only":                 true,
	"bt-min-crypto-level":              true,
	"bt-remove-unselected-file":        true,
	"bt-request-peer-speed-limit":      true,
	"bt-require-crypto":                true,
	"bt-save-metadata":                 true,
	"bt-seed-unverified":               true,
	"bt-stop-timeout":                  true,
	"bt-tracker-connect-timeout":       true,
	"bt-tracker-interval":              true,
	"bt-tracker-timeout":               true,
	"check-integrity":                  true,
	"conditional-get":                  true,
	"connect-timeout":                  true,
	"content-disposition-default-utf8": true,
	"continue":                         true,
	"dir":                              true,
	"dry-run":                          true,
	"enable-http-keep-alive":           true,
	"enable-http-pipelining":           true,
	"enable-mmap":                      true,
	"enable-peer-exchange":             true,
	"file-allocation":                  true,
	"follow-metalink":                  true,
	"follow-torrent":                   true,
	"force-save":                       true,
	"ftp-pasv":                         true,
	"ftp-reuse-connection":             true,
	"ftp-type":                         true,
	"hash-check-only":                  true,
	"http-accept-gzip":                 true,
	"http-auth-challenge":              true,
	"http-no-cache":                    true,
	"lowest-speed-limit":               true,
	"max-connection-per-server":        true,
	"max-download-limit":               true,
	"max-file-not-found":               true,
	"max-mmap-limit":                   true,
	"max-resume-failure-tries":         true,
	"max-tries":                        true,
	"max-upload-limit":                 true,
	"metalink-enable-unique-protocol":  true,
	"metalink-preferred-protocol":      true,
	"min-split-size":                   true,
	"no-file-allocation-limit":         true,
	"no-netrc":                         true,
	"no-want-digest-header":            true,
	"out":                              true,
	"parameterized-uri":                true,
	"pause-metadata":                   true,
	"piece-length":                     true,
	"proxy-method":                     true,
	"realtime-chunk-checksum":          true,
	"remote-time":                      true,
	"remove-control-file":              true,
	"retry-wait":                       true,
	"reuse-uri":                        true,
	"rpc-save-upload-metadata":         true,
	"save-not-found":                   true,
	"seed-ratio":                       true,
	"split":                            true,
	"stream-piece-selector":            true,
	"timeout":                          true,
	"uri-selector":                     true,
	"use-head":                         true,
	"user-agent":                       true,
}

func normalizeUnitOption(name, value string) (string, bool) {
	if !unitOption[name] || value == "" {
		return "", false
	}
	n, err := parseRPCUnit(value)
	if err != nil {
		return value, true
	}
	return strconv.FormatInt(n, 10), true
}

func normalizeProxyOption(name, value string) (string, bool) {
	if !proxyOption[name] || value == "" {
		return "", false
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	if !strings.HasSuffix(value, "/") {
		value += "/"
	}
	return value, true
}

func parseRPCUnit(value string) (int64, error) {
	mult := int64(1)
	switch last := value[len(value)-1]; last {
	case 'K', 'k':
		mult = 1024
		value = value[:len(value)-1]
	case 'M', 'm':
		mult = 1024 * 1024
		value = value[:len(value)-1]
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("bad unit value")
	}
	if n > (1<<63-1)/mult {
		return 0, fmt.Errorf("unit value overflows")
	}
	return n * mult, nil
}

// ---------------------------------------------------------------------------
// Notifications
// ---------------------------------------------------------------------------

// SubscribeNotifications registers a sink that receives engine events
// translated to aria2 notifications. Returns a cancel function that
// unsubscribes the sink.
func (d *Dispatcher) SubscribeNotifications(sink NotifySink) (cancel func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sinks = append(d.sinks, sink)
	idx := len(d.sinks) - 1

	d.ensureBridge()

	return func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.sinks[idx] = nil
	}
}

// ensureBridge starts a goroutine that subscribes to engine events and
// forwards them to registered sinks. Idempotent; safe to call multiple times.
func (d *Dispatcher) ensureBridge() {
	d.engineSubOnceMu.Lock()
	if d.engineSubStarted {
		d.engineSubOnceMu.Unlock()
		return
	}
	d.engineSubStarted = true
	d.engineSubOnceMu.Unlock()

	ch := make(chan core.Event, 64)
	result := d.engine.SubscribeChannel(ch)
	go d.bridgeLoop(ch, result.Unsubscribe)
}

// bridgeLoop drains engine events from ch and forwards them to registered
// sinks via OnEvent. Runs until ch is closed (unsubscribed).
func (d *Dispatcher) bridgeLoop(ch <-chan core.Event, unsub func()) {
	defer unsub()
	for ev := range ch {
		d.OnEvent(ev)
	}
}

// OnEvent implements engine.Subscriber. It translates engine events to
// aria2 notifications and forwards them to all registered sinks.
func (d *Dispatcher) OnEvent(ev core.Event) {
	d.mu.Lock()
	sinks := make([]NotifySink, 0, len(d.sinks))
	for _, s := range d.sinks {
		if s != nil {
			sinks = append(sinks, s)
		}
	}
	d.mu.Unlock()

	name, ok := eventToNotification[ev.Kind]
	if !ok {
		return
	}

	params := map[string]interface{}{
		"gid": gidString(ev.GID),
	}

	for _, sink := range sinks {
		sink(name, params)
	}
}

// Ensure Dispatcher implements engine.Subscriber.
var _ engine.Subscriber = (*Dispatcher)(nil)

// Ensure method names are sorted for stable listMethods output.
// Also pre-build the dispatch table for O(1) method lookups.
func init() {
	sort.Strings(methodNames)

	dispatchTable = map[string]dispatchFunc{
		"aria2.addUri":               (*Dispatcher).addUri,
		"aria2.addTorrent":           (*Dispatcher).addTorrent,
		"aria2.addMetalink":          (*Dispatcher).addMetalink,
		"aria2.remove":               (*Dispatcher).remove,
		"aria2.forceRemove":          (*Dispatcher).forceRemove,
		"aria2.pause":                (*Dispatcher).pause,
		"aria2.forcePause":           (*Dispatcher).forcePause,
		"aria2.pauseAll":             (*Dispatcher).pauseAll,
		"aria2.forcePauseAll":        (*Dispatcher).forcePauseAll,
		"aria2.unpause":              (*Dispatcher).unpause,
		"aria2.unpauseAll":           (*Dispatcher).unpauseAll,
		"aria2.tellStatus":           (*Dispatcher).tellStatus,
		"aria2.tellActive":           (*Dispatcher).tellActive,
		"aria2.tellWaiting":          (*Dispatcher).tellWaiting,
		"aria2.tellStopped":          (*Dispatcher).tellStopped,
		"aria2.getUris":              (*Dispatcher).getUris,
		"aria2.getFiles":             (*Dispatcher).getFiles,
		"aria2.getPeers":             (*Dispatcher).getPeers,
		"aria2.getServers":           (*Dispatcher).getServers,
		"aria2.getOption":            (*Dispatcher).getOption,
		"aria2.changeOption":         (*Dispatcher).changeOption,
		"aria2.getGlobalOption":      (*Dispatcher).getGlobalOption,
		"aria2.changeGlobalOption":   (*Dispatcher).changeGlobalOption,
		"aria2.changePosition":       (*Dispatcher).changePosition,
		"aria2.changeUri":            (*Dispatcher).changeUri,
		"aria2.purgeDownloadResult":  (*Dispatcher).purgeDownloadResult,
		"aria2.removeDownloadResult": (*Dispatcher).removeDownloadResult,
		"aria2.getGlobalStat":        (*Dispatcher).getGlobalStat,
		"aria2.getVersion":           (*Dispatcher).getVersion,
		"aria2.getSessionInfo":       (*Dispatcher).getSessionInfo,
		"aria2.shutdown":             (*Dispatcher).shutdown,
		"aria2.forceShutdown":        (*Dispatcher).forceShutdown,
		"aria2.saveSession":          (*Dispatcher).saveSession,
		"system.multicall":           (*Dispatcher).systemMulticall,
		"system.listMethods":         dispatcherListMethods,
		"system.listNotifications":   dispatcherListNotifications,
	}
}

func dispatcherListMethods(d *Dispatcher, _ context.Context, _ []interface{}) (interface{}, error) {
	return d.listMethods(), nil
}

func dispatcherListNotifications(d *Dispatcher, _ context.Context, _ []interface{}) (interface{}, error) {
	return d.listNotifications(), nil
}
