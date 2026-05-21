package transport

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/smartass08/aria2go/internal/ioutilx"
	"github.com/smartass08/aria2go/internal/rpc/jsonrpc"
)

const (
	websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	// WebSocket opcodes (RFC 6455 §5.2).
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA

	// WebSocket close status codes (RFC 6455 §7.4).
	closeNormal      = 1000
	closeProtocolErr = 1002
	closePolicy      = 1008
	closeMessageSize = 1009

	// Max frame payload size for a single frame (control frames: 125 bytes).
	maxControlPayload = 125

	// outboxBufferSize is the size of the per-session outbox channel.
	outboxBufferSize = 256

	// writeTimeout is the time allowed for writing a frame to the connection.
	writeTimeout = 10 * time.Second
)

var (
	errWSProtocolError   = errors.New("websocket: protocol error")
	errWSMessageTooBig   = errors.New("websocket: message too big")
	errWSPolicyViolation = errors.New("websocket: policy violation")

	acceptKeyCache sync.Map
)

// websocketConn wraps a net.Conn with buffered read/write for WebSocket.
type websocketConn struct {
	conn   net.Conn
	reader *bufio.Reader
	mu     sync.Mutex // protects writes
}

func newWebsocketConn(conn net.Conn) *websocketConn {
	return &websocketConn{
		conn:   conn,
		reader: bufio.NewReader(conn),
	}
}

func (wc *websocketConn) readFrame() (wsFrame, error) {
	return readFramex(wc.reader)
}

func (wc *websocketConn) writeFrame(f wsFrame) error {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	return writeFramex(wc.conn, f)
}

func (wc *websocketConn) close() error {
	return wc.conn.Close()
}

// wsFrame represents a parsed WebSocket frame.
type wsFrame struct {
	fin     bool
	opcode  byte
	masked  bool
	payload []byte
	buf     *ioutilx.Buf // non-nil if payload is from a pool; must be freed.
}

func (f *wsFrame) free() {
	if f.buf != nil {
		f.buf.Free()
		f.buf = nil
		f.payload = nil
	}
}

// readFramex reads a single WebSocket frame from the reader.
func readFramex(r *bufio.Reader) (wsFrame, error) {
	var frame wsFrame

	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return frame, fmt.Errorf("websocket: read header: %w", err)
	}

	frame.fin = header[0]&0x80 != 0
	frame.opcode = header[0] & 0x0F
	frame.masked = header[1]&0x80 != 0

	payloadLen := uint64(header[1] & 0x7F)

	switch {
	case payloadLen == 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return frame, fmt.Errorf("websocket: read extended length: %w", err)
		}
		payloadLen = uint64(binary.BigEndian.Uint16(ext[:]))
	case payloadLen == 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return frame, fmt.Errorf("websocket: read extended length: %w", err)
		}
		payloadLen = binary.BigEndian.Uint64(ext[:])
		if payloadLen&(1<<63) != 0 {
			return frame, fmt.Errorf("websocket: invalid extended length, MSB set")
		}
	}

	// Control frames must have payload length ≤ 125.
	if isControlFrame(frame.opcode) && payloadLen > maxControlPayload {
		return frame, fmt.Errorf("websocket: control frame payload too large: %d", payloadLen)
	}

	var maskKey [4]byte
	if frame.masked {
		if _, err := io.ReadFull(r, maskKey[:]); err != nil {
			return frame, fmt.Errorf("websocket: read mask key: %w", err)
		}
	}

	if payloadLen > 0 {
		bu := poolGet(int(payloadLen))
		frame.payload = bu.B[:payloadLen]
		frame.buf = bu
		if _, err := io.ReadFull(r, frame.payload); err != nil {
			frame.free()
			return frame, fmt.Errorf("websocket: read payload: %w", err)
		}
		if frame.masked {
			applyMask(frame.payload, maskKey)
		}
	}

	return frame, nil
}

func poolGet(size int) *ioutilx.Buf {
	switch {
	case size <= 4<<10:
		bu := ioutilx.Pool4K.Get()
		bu.B = bu.B[:size]
		return bu
	case size <= 16<<10:
		bu := ioutilx.Pool16K.Get()
		bu.B = bu.B[:size]
		return bu
	case size <= 64<<10:
		bu := ioutilx.Pool64K.Get()
		bu.B = bu.B[:size]
		return bu
	default:
		return &ioutilx.Buf{B: make([]byte, size)}
	}
}

// writeFramex writes a single WebSocket frame to the writer.
func writeFramex(w io.Writer, f wsFrame) error {
	header := make([]byte, 2, 10)
	if f.fin {
		header[0] = 0x80
	}
	header[0] |= f.opcode & 0x0F

	length := len(f.payload)
	switch {
	case length <= 125:
		header[1] = byte(length)
	case length <= 65535:
		header[1] = 126
		header = binary.BigEndian.AppendUint16(header, uint16(length))
	default:
		header[1] = 127
		header = binary.BigEndian.AppendUint64(header, uint64(length))
	}

	// Server frames are never masked.
	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("websocket: write header: %w", err)
	}
	if len(f.payload) > 0 {
		if _, err := w.Write(f.payload); err != nil {
			return fmt.Errorf("websocket: write payload: %w", err)
		}
	}
	return nil
}

func applyMask(data []byte, key [4]byte) {
	for i := range data {
		data[i] ^= key[i%4]
	}
}

func isControlFrame(opcode byte) bool {
	return opcode&0x08 != 0
}

// websocketSession represents a single WebSocket connection.
type websocketSession struct {
	conn       *websocketConn
	outbox     chan []byte
	done       chan struct{}
	logger     *slog.Logger
	closeMu    sync.Mutex
	closed     bool
	secret     string
	maxReqSize int64
}

// newWebsocketSession creates a new session wrapping the given connection.
func newWebsocketSession(conn net.Conn, secret string, maxReqSize int64) *websocketSession {
	return &websocketSession{
		conn:       newWebsocketConn(conn),
		outbox:     make(chan []byte, outboxBufferSize),
		done:       make(chan struct{}),
		logger:     slog.Default().With("component", "ws-session"),
		secret:     secret,
		maxReqSize: maxReqSize,
	}
}

// run starts the reader and writer goroutines. It blocks until the session ends.
func (s *websocketSession) run(ctx context.Context, dispatcher Dispatcher) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		s.readLoop(ctx, dispatcher)
	}()
	go func() {
		defer wg.Done()
		s.writeLoop(ctx)
	}()

	wg.Wait()
	s.conn.close()
	close(s.done)
}

func (s *websocketSession) readLoop(ctx context.Context, dispatcher Dispatcher) {
	defer func() {
		// Signal writer to stop by closing outbox.
		close(s.outbox)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		s.conn.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		frame, err := s.conn.readFrame()
		if err != nil {
			if !isNetClosed(err) {
				s.logger.Debug("websocket read error", "error", err)
			}
			return
		}
		if !frame.masked {
			frame.free()
			s.sendClose(closeProtocolErr, "unmasked client frame")
			return
		}

		switch frame.opcode {
		case opText, opBinary:
			if !frame.fin {
				frame.free()
				s.sendClose(closePolicy, "fragmented messages not supported")
				return
			}
			if frame.opcode == opText {
				s.handleTextMessage(frame.payload, dispatcher)
			}
			frame.free()

		case opClose:
			s.handleCloseFrame(frame)
			frame.free()
			return

		case opPing:
			s.handlePing(frame)
			frame.free()

		case opPong:
			frame.free()

		default:
			frame.free()
			s.sendClose(closeProtocolErr, "unknown opcode")
			return
		}
	}
}

func (s *websocketSession) handleTextMessage(payload []byte, dispatcher Dispatcher) {
	// Enforce max request size at the parser level, matching aria2 behavior.
	// If payload exceeds maxReqSize, silently truncate (feed no data to parser).
	if s.maxReqSize > 0 && int64(len(payload)) > s.maxReqSize {
		// Matching aria2: silently ignore oversized messages.
		// The parser never completes, so no error response is sent.
		return
	}

	single, batch, err := jsonrpc.Decode(payload)
	if err != nil {
		s.sendError(-32700, "Parse error.")
		return
	}

	if single != nil {
		if single.IsNotification() {
			s.sendResponse(jsonrpc.NewErrorResponse(single, -32600, "Invalid Request."))
			return
		}
		s.processSingle(single, dispatcher)
		return
	}
	s.processBatch(batch, dispatcher)
}

func (s *websocketSession) processSingle(req *jsonrpc.Request, dispatcher Dispatcher) {
	params, err := extractJSONParamsWS(req.Params)
	if err != nil {
		s.sendResponse(jsonrpc.NewErrorResponse(req, -32700, "Parse error."))
		return
	}

	if requiresSecretToken(req.Method) && !s.authenticateFromParams(params) {
		s.sendResponseDelayed(jsonrpc.NewErrorResponse(req, 1, "Unauthorized"))
		return
	}

	result, callErr := dispatcher.Call(req.Method, params)
	if callErr != nil {
		code := 1
		type coder interface {
			Code() int
		}
		if cd, ok := callErr.(coder); ok {
			code = cd.Code()
		}
		s.sendResponse(jsonrpc.NewErrorResponse(req, code, callErr.Error()))
		return
	}

	s.sendResponse(jsonrpc.NewResponse(req, result))
}

func (s *websocketSession) processBatch(batch []jsonrpc.Request, dispatcher Dispatcher) {
	responses := make([]jsonrpc.Response, 0, len(batch))
	unauthorized := false
	for i := range batch {
		req := &batch[i]
		if req.IsNotification() {
			continue
		}
		params, err := extractJSONParamsWS(req.Params)
		if err != nil {
			resp := jsonrpc.NewErrorResponse(req, -32700, "Parse error.")
			responses = append(responses, resp)
			continue
		}
		if requiresSecretToken(req.Method) && !s.authenticateFromParams(params) {
			unauthorized = true
			resp := jsonrpc.NewErrorResponse(req, 1, "Unauthorized")
			responses = append(responses, resp)
			continue
		}
		result, callErr := dispatcher.Call(req.Method, params)
		if callErr != nil {
			code := 1
			type coder interface {
				Code() int
			}
			if cd, ok := callErr.(coder); ok {
				code = cd.Code()
			}
			resp := jsonrpc.NewErrorResponse(req, code, callErr.Error())
			responses = append(responses, resp)
			continue
		}
		responses = append(responses, jsonrpc.NewResponse(req, result))
	}
	encoded, err := jsonrpc.EncodeBatch(responses)
	if err != nil {
		s.sendError(-32700, "Parse error.")
		return
	}
	if unauthorized {
		s.sendTextDelayed(encoded)
	} else {
		s.sendText(encoded)
	}
}

func (s *websocketSession) handleCloseFrame(frame wsFrame) {
	respFrame := wsFrame{
		fin:    true,
		opcode: opClose,
	}
	if len(frame.payload) >= 2 {
		respFrame.payload = make([]byte, 2)
		copy(respFrame.payload, frame.payload[:2])
	}
	s.conn.writeFrame(respFrame)
}

func (s *websocketSession) handlePing(frame wsFrame) {
	pongFrame := wsFrame{
		fin:     true,
		opcode:  opPong,
		payload: frame.payload,
	}
	s.conn.writeFrame(pongFrame)
}

func (s *websocketSession) sendClose(code int, reason string) {
	payload := make([]byte, 2, 2+len(reason))
	binary.BigEndian.PutUint16(payload, uint16(code))
	payload = append(payload, []byte(reason)...)
	frame := wsFrame{
		fin:     true,
		opcode:  opClose,
		payload: payload,
	}
	s.conn.writeFrame(frame)
}

func (s *websocketSession) sendError(code int, message string) {
	resp := jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      nil,
		Error:   &jsonrpc.Error{Code: code, Message: message},
	}
	encoded, _ := jsonrpc.Encode(resp)
	s.sendText(encoded)
}

func (s *websocketSession) sendResponse(resp jsonrpc.Response) {
	encoded, _ := jsonrpc.Encode(resp)
	s.sendText(encoded)
}

func (s *websocketSession) sendResponseDelayed(resp jsonrpc.Response) {
	encoded, _ := jsonrpc.Encode(resp)
	s.sendTextDelayed(encoded)
}

func (s *websocketSession) sendText(data []byte) {
	select {
	case s.outbox <- data:
	default:
		s.logger.Warn("websocket outbox full, dropping message")
	}
}

func (s *websocketSession) sendTextDelayed(data []byte) {
	go func() {
		time.Sleep(1 * time.Second)
		select {
		case s.outbox <- data:
		default:
			s.logger.Warn("websocket outbox full, dropping message")
		}
	}()
}

func (s *websocketSession) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-s.outbox:
			if !ok {
				return
			}
			frame := wsFrame{
				fin:     true,
				opcode:  opText,
				payload: msg,
			}
			s.conn.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := s.conn.writeFrame(frame); err != nil {
				if !isNetClosed(err) {
					s.logger.Debug("websocket write error", "error", err)
				}
				return
			}
		}
	}
}

// sendNotification sends a notification to the WebSocket session.
func (s *websocketSession) sendNotification(notif Notification) {
	msg, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  notif.Method,
		"params":  notif.Params,
	})
	if err != nil {
		s.logger.Error("failed to marshal notification", "error", err)
		return
	}
	select {
	case s.outbox <- msg:
	default:
		s.logger.Warn("websocket outbox full, dropping notification")
	}
}

func (s *websocketSession) authenticateFromParams(params []any) bool {
	if s.secret == "" {
		return true
	}
	if len(params) == 0 {
		return false
	}
	token, ok := params[0].(string)
	if !ok {
		return false
	}
	if strings.HasPrefix(token, jsonrpc.TokenPrefix) {
		token = token[len(jsonrpc.TokenPrefix):]
	}
	return jsonrpc.ValidateToken(s.secret, token)
}

func isNetClosed(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	return strings.Contains(err.Error(), "use of closed network connection")
}

func extractJSONParamsWS(raw json.RawMessage) ([]any, error) {
	if len(raw) == 0 || string(raw) == "[]" {
		return nil, nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, err
	}
	out := make([]any, len(arr))
	for i, r := range arr {
		var v any
		if err := json.Unmarshal(r, &v); err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// websocketSessionMan manages all active WebSocket sessions.
type websocketSessionMan struct {
	mu       sync.RWMutex
	sessions map[*websocketSession]struct{}
}

func newWebsocketSessionMan() *websocketSessionMan {
	return &websocketSessionMan{
		sessions: make(map[*websocketSession]struct{}),
	}
}

func (m *websocketSessionMan) add(s *websocketSession) {
	m.mu.Lock()
	m.sessions[s] = struct{}{}
	m.mu.Unlock()
}

func (m *websocketSessionMan) remove(s *websocketSession) {
	m.mu.Lock()
	delete(m.sessions, s)
	m.mu.Unlock()
}

func (m *websocketSessionMan) broadcast(notif Notification) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for sess := range m.sessions {
		sess.sendNotification(notif)
	}
}

func (m *websocketSessionMan) count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// handleWebSocketUpgrade performs the WebSocket upgrade handshake.
// Note: aria2 does not restrict WebSocket connections by Origin header,
// only CORS headers use --rpc-allow-origin-all.
func (s *Server) handleWebSocketUpgrade(w http.ResponseWriter, r *http.Request, origin string) {
	clientKey := r.Header.Get("Sec-WebSocket-Key")
	if clientKey == "" {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	wsVersion := r.Header.Get("Sec-WebSocket-Version")
	if wsVersion != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		http.Error(w, "Upgrade Required", http.StatusUpgradeRequired)
		return
	}

	// Compute accept key: base64(sha1(clientKey + GUID))
	serverKey := computeAcceptKey(clientKey)

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		s.logger.Error("websocket: server does not support hijacking")
		return
	}

	conn, bufrw, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		s.logger.Error("websocket: hijack failed", "error", err)
		return
	}

	// Write the upgrade response.
	resp := fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Accept: %s\r\n"+
		"\r\n", serverKey)
	if _, err := bufrw.WriteString(resp); err != nil {
		s.logger.Error("websocket: write upgrade response failed", "error", err)
		conn.Close()
		return
	}
	if err := bufrw.Flush(); err != nil {
		s.logger.Error("websocket: flush upgrade response failed", "error", err)
		conn.Close()
		return
	}

	session := newWebsocketSession(conn, s.cfg.Secret, s.cfg.MaxRequestSize)
	s.wsMan.add(session)

	// Use a background context for the session — the HTTP request
	// context is cancelled when handleWebSocketUpgrade returns.
	go func() {
		defer s.wsMan.remove(session)
		session.run(context.Background(), s.cfg.Dispatcher)
	}()
}

func (s *Server) isOriginAllowed(origin string) bool {
	if len(s.cfg.AllowedOrigins) == 0 {
		return true
	}
	for _, o := range s.cfg.AllowedOrigins {
		if o == "*" || o == origin {
			return true
		}
	}
	return false
}

func computeAcceptKey(clientKey string) string {
	if cached, ok := acceptKeyCache.Load(clientKey); ok {
		return cached.(string)
	}
	h := sha1.New()
	h.Write([]byte(clientKey))
	h.Write([]byte(websocketGUID))
	accept := base64.StdEncoding.EncodeToString(h.Sum(nil))
	acceptKeyCache.Store(clientKey, accept)
	return accept
}
