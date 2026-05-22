package transport

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	ariabase64 "github.com/smartass08/aria2go/internal/encoding/base64"
	"github.com/smartass08/aria2go/internal/rpc/jsonrpc"
	"github.com/smartass08/aria2go/internal/rpc/xmlrpc"
)

var (
	basicAuthHMACKey   []byte
	basicAuthHMACKeyMu sync.Mutex

	xmlBufPool = sync.Pool{
		New: func() any { return new(bytes.Buffer) },
	}
)

func ensureHMACKey() []byte {
	basicAuthHMACKeyMu.Lock()
	defer basicAuthHMACKeyMu.Unlock()
	if basicAuthHMACKey == nil {
		key := make([]byte, sha1.BlockSize) // 64 bytes (same as SHA-1 block size)
		if _, err := rand.Read(key); err != nil {
			panic("transport: failed to generate HMAC key: " + err.Error())
		}
		basicAuthHMACKey = key
	}
	return basicAuthHMACKey
}

func hmacResult(data string) []byte {
	key := ensureHMACKey()
	mac := hmac.New(sha1.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

// Config holds the configuration for an RPC transport server.
type Config struct {
	Listen         string      // e.g. ":6800"
	TLS            *tls.Config // nil = HTTP only
	AllowedOrigins []string    // CORS allowed origins; "*" allows all
	Dispatcher     Dispatcher
	Secret         string // --rpc-secret value
	RPCUser        string // --rpc-user value for HTTP Basic auth
	RPCPasswd      string // --rpc-passwd value for HTTP Basic auth
	ListenAll      bool   // listen on all interfaces
	MaxRequestSize int64  // --rpc-max-request-size (bytes); 0 = default
}

// Server is an HTTP server that exposes aria2-compatible JSON-RPC 2.0,
// XML-RPC, and WebSocket endpoints.
type Server struct {
	cfg    Config
	logger *slog.Logger
	http   *http.Server

	wsMan        *websocketSessionMan
	hmacUsername []byte // HMAC of rpc-user (nil = no auth)
	hmacPassword []byte // HMAC of rpc-passwd (nil = no password check)
}

// New creates a new Server with the given configuration.
func New(cfg Config) (*Server, error) {
	if cfg.Dispatcher == nil {
		return nil, fmt.Errorf("transport: Dispatcher is required")
	}
	wsMan := newWebsocketSessionMan()
	s := &Server{
		cfg:    cfg,
		logger: slog.Default().With("component", "rpc-transport"),
		wsMan:  wsMan,
	}
	if cfg.RPCUser != "" {
		s.hmacUsername = hmacResult(cfg.RPCUser)
	}
	if cfg.RPCPasswd != "" {
		s.hmacPassword = hmacResult(cfg.RPCPasswd)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/jsonrpc", s.handleJSONRPC)
	mux.HandleFunc("/rpc", s.handleXMLRPC)
	mux.HandleFunc("/", s.handleRoot)
	s.http = &http.Server{
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return s, nil
}

// Run starts the RPC transport server and blocks until ctx is cancelled
// or the server encounters a fatal error.
func (s *Server) Run(ctx context.Context) error {
	addr := s.cfg.Listen
	if s.cfg.ListenAll {
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("transport: invalid listen address %q: %w", addr, err)
		}
		addr = net.JoinHostPort("0.0.0.0", port)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("transport: listen on %q: %w", addr, err)
	}

	if s.cfg.TLS != nil {
		ln = tls.NewListener(ln, s.cfg.TLS)
	}

	s.logger.Info("RPC transport listening", "addr", addr, "tls", s.cfg.TLS != nil)

	// Start notification broadcast goroutine.
	notifCtx, cancelNotif := context.WithCancel(ctx)
	defer cancelNotif()
	if s.cfg.Dispatcher != nil {
		go s.broadcastNotifications(notifCtx)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.http.Serve(ln)
	}()

	select {
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return fmt.Errorf("transport: server error: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.http.Shutdown(shutdownCtx)
	}
}

func (s *Server) broadcastNotifications(ctx context.Context) {
	ch, err := s.cfg.Dispatcher.SubscribeNotifications(ctx)
	if err != nil {
		s.logger.Error("failed to subscribe to notifications", "error", err)
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case notif, ok := <-ch:
			if !ok {
				return
			}
			s.wsMan.broadcast(notif)
		}
	}
}

// corsOrigin returns the CORS header value for the given request origin.
func (s *Server) corsOrigin(reqOrigin string) string {
	allowed := s.cfg.AllowedOrigins
	if len(allowed) == 0 {
		return ""
	}
	for _, o := range allowed {
		if o == "*" {
			return "*"
		}
		if o == reqOrigin {
			return reqOrigin
		}
	}
	return ""
}

// corsHandler handles CORS preflight requests.
func (s *Server) corsHandler(w http.ResponseWriter, r *http.Request, origin string) (handled bool) {
	if r.Method != http.MethodOptions {
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	if origin != "*" {
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Max-Age", "1728000")
	reqHeaders := r.Header.Get("Access-Control-Request-Headers")
	if reqHeaders != "" {
		w.Header().Set("Access-Control-Allow-Headers", reqHeaders)
	}
	w.WriteHeader(http.StatusOK)
	return true
}

// setCORSHeaders adds CORS headers to a response.
func (s *Server) setCORSHeaders(w http.ResponseWriter, origin string) {
	w.Header().Set("Access-Control-Allow-Origin", origin)
	if origin != "*" {
		w.Header().Set("Vary", "Origin")
	}
}

// authenticate checks the RPC secret token against the request.
// It extracts the token from the first positional parameter using HMAC-based
// validation (matching aria2's validateToken).
// Returns true if authentication passes.
func (s *Server) authenticate(params []any) bool {
	if s.cfg.Secret == "" {
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
	return jsonrpc.ValidateToken(s.cfg.Secret, token)
}

func requiresSecretToken(method string) bool {
	switch method {
	case "system.multicall", "system.listMethods", "system.listNotifications":
		return false
	default:
		return true
	}
}

// authenticateHTTP checks HTTP Basic authentication via the Authorization
// header against configured --rpc-user and --rpc-passwd.
// Returns true if authentication passes. Matches aria2's HMAC-based comparison.
func (s *Server) authenticateHTTP(r *http.Request) bool {
	if s.hmacUsername == nil {
		return true
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}

	scheme, rest, found := strings.Cut(authHeader, " ")
	if !found || !strings.EqualFold(scheme, "Basic") {
		return false
	}

	decoded, err := base64.StdEncoding.DecodeString(rest)
	if err != nil {
		return false
	}

	user, pass, hasColon := strings.Cut(string(decoded), ":")
	if !hasColon {
		return false
	}

	expectedUser := hmacResult(user)
	if subtle.ConstantTimeCompare(s.hmacUsername, expectedUser) != 1 {
		return false
	}

	if s.hmacPassword != nil {
		expectedPass := hmacResult(pass)
		return subtle.ConstantTimeCompare(s.hmacPassword, expectedPass) == 1
	}

	return true
}

// decodeJSONPQuery parses GET query parameters into a JSON-RPC request body,
// matching aria2's decodeGetParams behavior.
func decodeJSONPQuery(query string) (body []byte, callback string) {
	if query == "" {
		return nil, ""
	}

	query = strings.TrimPrefix(query, "?")
	var method, id, paramsStr string
	var hasMethod, hasID, hasParams bool
	for len(query) > 0 {
		part := query
		if amp := strings.IndexByte(query, '&'); amp >= 0 {
			part = query[:amp]
			query = query[amp+1:]
		} else {
			query = ""
		}
		switch {
		case strings.HasPrefix(part, "method="):
			hasMethod = true
			method = part[len("method="):]
		case strings.HasPrefix(part, "id="):
			hasID = true
			id = part[len("id="):]
		case strings.HasPrefix(part, "params="):
			hasParams = true
			paramsStr = part[len("params="):]
		case strings.HasPrefix(part, "jsoncallback="):
			callback = part[len("jsoncallback="):]
		}
	}

	jsonParam := decodeJSONPParam(paramsStr)
	if !hasMethod && !hasID {
		// Batch call: params is the raw JSON.
		return []byte(jsonParam), callback
	}

	// aria2's GET decoder preserves the raw query substrings for method and id.
	// It also rejects method-less single requests and broken base64 params as
	// top-level invalid requests rather than echoing the provided id.
	if !hasMethod || (hasParams && jsonParam == "") {
		return []byte(`{}`), callback
	}

	var buf bytes.Buffer
	buf.WriteString("{")
	if hasMethod {
		buf.WriteString(`"method":"`)
		buf.WriteString(method)
		buf.WriteString(`"`)
	}
	if hasID {
		if hasMethod {
			buf.WriteString(",")
		}
		buf.WriteString(`"id":"`)
		buf.WriteString(id)
		buf.WriteString(`"`)
	}
	if hasParams {
		if hasMethod || hasID {
			buf.WriteString(",")
		}
		buf.WriteString(`"params":`)
		buf.WriteString(jsonParam)
	}
	buf.WriteString("}")
	return buf.Bytes(), callback
}

func decodeJSONPParam(raw string) string {
	if raw == "" {
		return ""
	}
	decoded, err := ariabase64.Decode(percentDecode(raw))
	if err != nil {
		return ""
	}
	return string(decoded)
}

func percentDecode(s string) string {
	if s == "" {
		return ""
	}
	var buf strings.Builder
	buf.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '%' || i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
			buf.WriteByte(s[i])
			continue
		}
		buf.WriteByte(fromHex(s[i+1])<<4 | fromHex(s[i+2]))
		i += 2
	}
	return buf.String()
}

func isHex(b byte) bool {
	return ('0' <= b && b <= '9') || ('a' <= b && b <= 'f') || ('A' <= b && b <= 'F')
}

func fromHex(b byte) byte {
	switch {
	case '0' <= b && b <= '9':
		return b - '0'
	case 'a' <= b && b <= 'f':
		return b - 'a' + 10
	default:
		return b - 'A' + 10
	}
}

// wrapJSONP wraps a JSON body with a JSONP callback: callback(...).
func wrapJSONP(body []byte, callback string) []byte {
	var buf bytes.Buffer
	buf.WriteString(callback)
	buf.WriteString("(")
	buf.Write(body)
	buf.WriteString(")")
	return buf.Bytes()
}

// httpStatusCode maps an RPC error code to an HTTP status code,
// matching aria2's behavior.
func httpStatusCode(rpcCode int) int {
	switch rpcCode {
	case 1:
		return http.StatusBadRequest
	case jsonrpc.ErrCodeInvalidRequest:
		return http.StatusBadRequest
	case jsonrpc.ErrCodeMethodNotFound:
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func (s *Server) abortOversizedRequest(r *http.Request) {
	s.logger.Info("request too long, closing without response",
		"contentLength", r.ContentLength,
		"maxRequestSize", s.cfg.MaxRequestSize)
	if r.Body != nil {
		_ = r.Body.Close()
	}
	panic(http.ErrAbortHandler)
}

func (s *Server) requestTooLarge(r *http.Request) bool {
	return s.cfg.MaxRequestSize > 0 && r.ContentLength > s.cfg.MaxRequestSize
}

// handleJSONRPC handles requests to /jsonrpc.
// Supports: POST (standard JSON-RPC), GET (JSONP), OPTIONS (CORS preflight),
// and WebSocket upgrade.
func (s *Server) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	origin := s.corsOrigin(r.Header.Get("Origin"))
	if origin != "" {
		s.setCORSHeaders(w, origin)
		if s.corsHandler(w, r, origin) {
			return
		}
	}

	// OPTIONS is not restricted by authentication (CORS preflight).
	if r.Method == http.MethodOptions {
		// Already handled by corsHandler above.
		return
	}

	// Check HTTP Basic auth (rpc-user/rpc-passwd) before anything else,
	// except OPTIONS which is already handled above.
	if !s.authenticateHTTP(r) {
		s.disableKeepAlive()
		w.Header().Set("WWW-Authenticate", "Basic realm=\"aria2\"")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Check for WebSocket upgrade.
	if r.Method == http.MethodGet && headerHasToken(r.Header, "Connection", "upgrade") &&
		strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		s.handleWebSocketUpgrade(w, r, origin)
		return
	}

	// JSONP: GET /jsonrpc with query parameters.
	if r.Method == http.MethodGet {
		s.handleJSONP(w, r, origin)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Enforce max request size for POST.
	if s.requestTooLarge(r) {
		s.abortOversizedRequest(r)
		return
	}

	body, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		s.writeJSONError(w, jsonrpc.NewParseError(), http.StatusBadRequest, origin)
		return
	}

	single, batch, err := jsonrpc.Decode(body)
	if err != nil {
		code := jsonrpc.ErrCodeParse
		msg := "Parse error."
		if jerr, ok := err.(*jsonrpc.Error); ok {
			code = jerr.Code
			msg = jerr.Message
		}
		resp := jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      nil,
			Error:   &jsonrpc.Error{Code: code, Message: msg},
		}
		s.writeJSONError(w, resp, http.StatusBadRequest, origin)
		return
	}

	if single != nil {
		s.handleSingleJSONRPC(w, r, single, origin)
		return
	}
	s.handleBatchJSONRPC(w, r, batch, origin)
}

// handleJSONP handles GET /jsonrpc JSON-P callbacks.
func (s *Server) handleJSONP(w http.ResponseWriter, r *http.Request, origin string) {
	body, callback := decodeJSONPQuery(r.URL.RawQuery)

	single, batch, err := jsonrpc.Decode(body)
	if err != nil {
		code := jsonrpc.ErrCodeParse
		msg := "Parse error."
		if jerr, ok := err.(*jsonrpc.Error); ok {
			code = jerr.Code
			msg = jerr.Message
		}
		resp := jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      nil,
			Error:   &jsonrpc.Error{Code: code, Message: msg},
		}
		encoded, _ := jsonrpc.Encode(resp)
		responseBody := encoded
		contentType := "application/json-rpc"
		if callback != "" {
			responseBody = wrapJSONP(encoded, callback)
			contentType = "text/javascript"
		}
		s.writeJSONBytes(w, responseBody, contentType, http.StatusBadRequest, origin, false)
		return
	}

	if single != nil {
		if single.IsNotification() {
			resp := jsonrpc.NewInvalidRequestError(nil)
			s.sendJSONPResponse(w, resp, http.StatusBadRequest, origin, callback)
			return
		}
		resp := s.processSingleJSONRPC(single)
		status := http.StatusOK
		if resp.Error != nil {
			status = httpStatusCode(resp.Error.Code)
		}
		s.sendJSONPResponse(w, resp, status, origin, callback)
		return
	}

	s.handleJSONPBatch(w, batch, origin, callback)
}

func (s *Server) sendJSONPResponse(w http.ResponseWriter, resp jsonrpc.Response, status int, origin string, callback string) {
	encoded, _ := jsonrpc.Encode(resp)
	responseBody := encoded
	contentType := "application/json-rpc"
	if callback != "" {
		responseBody = wrapJSONP(encoded, callback)
		contentType = "text/javascript"
	}
	// Error responses always disable keep-alive, matching aria2.
	keepAlive := !isErrorResponse(resp)
	s.writeJSONBytes(w, responseBody, contentType, status, origin, keepAlive)
}

// isErrorResponse checks if a JSON-RPC response is an error.
func isErrorResponse(resp jsonrpc.Response) bool {
	return resp.Error != nil
}

// disableKeepAlive adds Connection: close to the response (callable from
// outside writeJSONBytes when we're using http.Error directly).
func (s *Server) disableKeepAlive() {}

func (s *Server) handleJSONPBatch(w http.ResponseWriter, batch []jsonrpc.Request, origin string, callback string) {
	responses := make([]jsonrpc.Response, 0, len(batch))
	unauthorized := false
	for i := range batch {
		req := &batch[i]
		if req.ValidationError != nil {
			responses = append(responses, jsonrpc.NewErrorResponse(req, req.ValidationError.Code, req.ValidationError.Message))
			continue
		}
		if req.IsNotification() {
			resp := jsonrpc.NewInvalidRequestError(nil)
			responses = append(responses, resp)
			continue
		}
		params, err := s.extractJSONParams(req.Params)
		if err != nil {
			resp := jsonrpc.NewErrorResponse(req, jsonrpc.ErrCodeParse, "Parse error.")
			responses = append(responses, resp)
			continue
		}
		if requiresSecretToken(req.Method) && !s.authenticate(params) {
			unauthorized = true
			resp := jsonrpc.NewErrorResponse(req, 1, "Unauthorized")
			responses = append(responses, resp)
			continue
		}
		result, callErr := s.cfg.Dispatcher.Call(req.Method, params)
		if callErr != nil {
			code, msg := s.rpcErrorToJSON(callErr)
			resp := jsonrpc.NewErrorResponse(req, code, msg)
			responses = append(responses, resp)
			continue
		}
		responses = append(responses, jsonrpc.NewResponse(req, result))
	}

	if unauthorized {
		time.Sleep(1 * time.Second)
		w.Header().Set("WWW-Authenticate", "Basic realm=\"aria2\"")
	}

	encoded, err := jsonrpc.EncodeBatch(responses)
	if err != nil {
		s.writeJSONError(w, jsonrpc.NewParseError(), http.StatusInternalServerError, origin)
		return
	}
	responseBody := encoded
	contentType := "application/json-rpc"
	if callback != "" {
		responseBody = wrapJSONP(encoded, callback)
		contentType = "text/javascript"
	}
	keepAlive := !unauthorized
	s.writeJSONBytes(w, responseBody, contentType, http.StatusOK, origin, keepAlive)
}

// processSingleJSONRPC processes a single JSON-RPC request and returns a
// response. This is shared between POST and GET/JSONP paths.
// Callers must handle notifications before calling this method.
func (s *Server) processSingleJSONRPC(req *jsonrpc.Request) jsonrpc.Response {
	if req.ValidationError != nil {
		return jsonrpc.NewErrorResponse(req, req.ValidationError.Code, req.ValidationError.Message)
	}
	params, err := s.extractJSONParams(req.Params)
	if err != nil {
		return jsonrpc.NewErrorResponse(req, jsonrpc.ErrCodeParse, "Parse error.")
	}

	if requiresSecretToken(req.Method) && !s.authenticate(params) {
		return jsonrpc.NewErrorResponse(req, 1, "Unauthorized")
	}

	result, callErr := s.cfg.Dispatcher.Call(req.Method, params)
	if callErr != nil {
		code, msg := s.rpcErrorToJSON(callErr)
		return jsonrpc.NewErrorResponse(req, code, msg)
	}

	return jsonrpc.NewResponse(req, result)
}

func (s *Server) handleSingleJSONRPC(w http.ResponseWriter, r *http.Request, req *jsonrpc.Request, origin string) {
	if req.ValidationError != nil {
		resp := jsonrpc.NewErrorResponse(req, req.ValidationError.Code, req.ValidationError.Message)
		s.writeJSONResponse(w, resp, httpStatusCode(resp.Error.Code), origin, false)
		return
	}
	if req.IsNotification() {
		resp := jsonrpc.NewInvalidRequestError(nil)
		s.writeJSONResponse(w, resp, http.StatusBadRequest, origin, false)
		return
	}

	params, err := s.extractJSONParams(req.Params)
	if err != nil {
		resp := jsonrpc.NewErrorResponse(req, jsonrpc.ErrCodeParse, "Parse error.")
		s.writeJSONResponse(w, resp, http.StatusBadRequest, origin, false)
		return
	}

	if requiresSecretToken(req.Method) && !s.authenticate(params) {
		time.Sleep(1 * time.Second)
		resp := jsonrpc.NewErrorResponse(req, 1, "Unauthorized")
		s.writeJSONResponse(w, resp, http.StatusBadRequest, origin, false)
		return
	}

	result, callErr := s.cfg.Dispatcher.Call(req.Method, params)
	if callErr != nil {
		code, msg := s.rpcErrorToJSON(callErr)
		resp := jsonrpc.NewErrorResponse(req, code, msg)
		s.writeJSONResponse(w, resp, httpStatusCode(code), origin, false)
		return
	}

	resp := jsonrpc.NewResponse(req, result)
	s.writeJSONResponse(w, resp, http.StatusOK, origin, true)
}

func (s *Server) handleBatchJSONRPC(w http.ResponseWriter, r *http.Request, batch []jsonrpc.Request, origin string) {
	responses := make([]jsonrpc.Response, 0, len(batch))
	unauthorized := false
	for i := range batch {
		req := &batch[i]
		if req.ValidationError != nil {
			responses = append(responses, jsonrpc.NewErrorResponse(req, req.ValidationError.Code, req.ValidationError.Message))
			continue
		}
		if req.IsNotification() {
			resp := jsonrpc.NewInvalidRequestError(nil)
			responses = append(responses, resp)
			continue
		}
		params, err := s.extractJSONParams(req.Params)
		if err != nil {
			resp := jsonrpc.NewErrorResponse(req, jsonrpc.ErrCodeParse, "Parse error.")
			responses = append(responses, resp)
			continue
		}

		if requiresSecretToken(req.Method) && !s.authenticate(params) {
			unauthorized = true
			resp := jsonrpc.NewErrorResponse(req, 1, "Unauthorized")
			responses = append(responses, resp)
			continue
		}

		result, callErr := s.cfg.Dispatcher.Call(req.Method, params)
		if callErr != nil {
			code, msg := s.rpcErrorToJSON(callErr)
			resp := jsonrpc.NewErrorResponse(req, code, msg)
			responses = append(responses, resp)
			continue
		}
		responses = append(responses, jsonrpc.NewResponse(req, result))
	}

	if unauthorized {
		time.Sleep(1 * time.Second)
		w.Header().Set("WWW-Authenticate", "Basic realm=\"aria2\"")
	}

	encoded, err := jsonrpc.EncodeBatch(responses)
	if err != nil {
		s.writeJSONError(w, jsonrpc.NewParseError(), http.StatusInternalServerError, origin)
		return
	}
	s.writeJSONBytes(w, encoded, "application/json-rpc", http.StatusOK, origin, !unauthorized)
}

func (s *Server) extractJSONParams(raw json.RawMessage) ([]any, error) {
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

// handleXMLRPC handles POST requests to /rpc.
func (s *Server) handleXMLRPC(w http.ResponseWriter, r *http.Request) {
	origin := s.corsOrigin(r.Header.Get("Origin"))
	if origin != "" {
		s.setCORSHeaders(w, origin)
		if s.corsHandler(w, r, origin) {
			return
		}
	}

	if r.Method == http.MethodOptions {
		return
	}

	// HTTP Basic auth check.
	if !s.authenticateHTTP(r) {
		s.disableKeepAlive()
		w.Header().Set("WWW-Authenticate", "Basic realm=\"aria2\"")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.requestTooLarge(r) {
		s.abortOversizedRequest(r)
		return
	}

	body, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		s.writeXMLError(w, 1, "Failed to read request body", origin)
		return
	}

	call, err := xmlrpc.DecodeCall(bytes.NewReader(body))
	if err != nil {
		s.writePlainError(w, "Bad Request", http.StatusBadRequest, origin)
		return
	}
	call.Params = normalizeXMLRPCUploadParams(call.MethodName, call.Params)

	if requiresSecretToken(call.MethodName) && !s.authenticate(call.Params) {
		time.Sleep(1 * time.Second)
		w.Header().Set("WWW-Authenticate", "Basic realm=\"aria2\"")
		s.writeXMLError(w, 1, "Unauthorized", origin)
		return
	}

	result, callErr := s.cfg.Dispatcher.Call(call.MethodName, call.Params)
	if callErr != nil {
		code, msg := s.rpcErrorToXML(callErr)
		s.writeXMLError(w, code, msg, origin)
		return
	}

	buf := xmlBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if err := xmlrpc.EncodeReply(buf, xmlrpc.Reply{Result: result}); err != nil {
		xmlBufPool.Put(buf)
		s.writeXMLError(w, 1, "Failed to encode response", origin)
		return
	}
	s.writeXMLBytes(w, buf.Bytes(), origin)
	xmlBufPool.Put(buf)
}

func normalizeXMLRPCUploadParams(method string, params []any) []any {
	if method != "aria2.addTorrent" && method != "aria2.addMetalink" {
		return params
	}
	payloadIndex := 0
	if len(params) > 0 {
		if token, ok := params[0].(string); ok && strings.HasPrefix(token, jsonrpc.TokenPrefix) {
			payloadIndex = 1
		}
	}
	if payloadIndex >= len(params) {
		return params
	}
	payload, ok := params[payloadIndex].(string)
	if !ok {
		return params
	}
	normalized := append([]any(nil), params...)
	normalized[payloadIndex] = ariabase64.Encode([]byte(payload))
	return normalized
}

// handleRoot handles requests to /. Only returns 404 (matches aria2 behavior).
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	origin := s.corsOrigin(r.Header.Get("Origin"))
	if origin != "" {
		s.setCORSHeaders(w, origin)
		if s.corsHandler(w, r, origin) {
			return
		}
	}

	// OPTIONS not subject to auth.
	if r.Method == http.MethodOptions {
		return
	}

	if !s.authenticateHTTP(r) {
		w.Header().Set("WWW-Authenticate", "Basic realm=\"aria2\"")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	http.Error(w, "Not Found", http.StatusNotFound)
}

func (s *Server) rpcErrorToJSON(err error) (code int, message string) {
	type coder interface {
		Code() int
	}
	if cd, ok := err.(coder); ok {
		return cd.Code(), err.Error()
	}
	if isMethodNotFound(err) {
		return jsonrpc.ErrCodeMethodNotFound, jsonrpc.MsgMethodNotFound
	}
	return 1, err.Error()
}

func (s *Server) rpcErrorToXML(err error) (code int, message string) {
	type coder interface {
		Code() int
	}
	if cd, ok := err.(coder); ok {
		return cd.Code(), err.Error()
	}
	return 1, err.Error()
}

func isMethodNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.EqualFold(msg, "method not found") ||
		strings.HasPrefix(strings.ToLower(msg), "no such method:")
}

func headerHasToken(header http.Header, key, token string) bool {
	for _, value := range header.Values(key) {
		for part := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func (s *Server) writeJSONResponse(w http.ResponseWriter, resp jsonrpc.Response, status int, origin string, keepAlive bool) {
	encoded, err := jsonrpc.Encode(resp)
	if err != nil {
		s.writeJSONError(w, jsonrpc.NewParseError(), http.StatusInternalServerError, origin)
		return
	}
	s.writeJSONBytes(w, encoded, "application/json-rpc", status, origin, keepAlive)
}

func (s *Server) writeJSONError(w http.ResponseWriter, resp jsonrpc.Response, status int, origin string) {
	encoded, err := jsonrpc.Encode(resp)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	s.writeJSONBytes(w, encoded, "application/json-rpc", status, origin, false)
}

func (s *Server) writeJSONBytes(w http.ResponseWriter, body []byte, contentType string, status int, origin string, keepAlive bool) {
	now := time.Now().UTC().Format(time.RFC1123)
	w.Header().Set("Date", now)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Expires", now)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	if !keepAlive {
		w.Header().Set("Connection", "close")
	}
	w.WriteHeader(status)
	w.Write(body)
}

func (s *Server) writePlainError(w http.ResponseWriter, message string, status int, origin string) {
	body := []byte(message + "\n")
	now := time.Now().UTC().Format(time.RFC1123)
	w.Header().Set("Date", now)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Expires", now)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	w.Header().Set("Connection", "close")
	w.WriteHeader(status)
	w.Write(body)
}

func (s *Server) writeXMLError(w http.ResponseWriter, code int, message string, origin string) {
	buf := xmlBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if err := xmlrpc.EncodeReply(buf, xmlrpc.Reply{
		Fault: &xmlrpc.Fault{Code: code, String: message},
	}); err != nil {
		xmlBufPool.Put(buf)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	s.writeXMLBytes(w, buf.Bytes(), origin)
	xmlBufPool.Put(buf)
}

func (s *Server) writeXMLBytes(w http.ResponseWriter, body []byte, origin string) {
	now := time.Now().UTC().Format(time.RFC1123)
	w.Header().Set("Date", now)
	w.Header().Set("Content-Type", "text/xml")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Expires", now)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	w.Header().Set("Connection", "close")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}
