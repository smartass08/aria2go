// Package ftp implements the FTP control connection with login, command
// dispatch, passive/active data connections, and file transfers (RETR, SIZE).
package ftp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/smartass08/aria2go/internal/core"
	"github.com/smartass08/aria2go/internal/netx"
)

// Opt configures FTP connection options.
type Opt struct {
	User      string
	Pass      string
	TLSMode   int // 0=none, 1=explicit (AUTH TLS), 2=implicit (connect TLS)
	TLSConfig *tls.Config
	Passive   bool
}

// maxRecvBuffer limits total FTP response bytes to prevent memory exhaustion.
const maxRecvBuffer = 64 << 10

var errMaxRecvBuffer = errors.New("ftp: max FTP recv buffer reached")

// recvLimited wraps a net.Conn with a read limit.
type recvLimited struct {
	net.Conn
	n int64
}

func (r *recvLimited) Read(p []byte) (int, error) {
	if r.n >= maxRecvBuffer {
		return 0, fmt.Errorf("%w (length=%d)", errMaxRecvBuffer, r.n+int64(len(p)))
	}
	if int64(len(p)) > maxRecvBuffer-r.n {
		p = p[:maxRecvBuffer-r.n]
	}
	nn, err := r.Conn.Read(p)
	r.n += int64(nn)
	return nn, err
}

// Conn is an FTP control connection.
type Conn struct {
	conn           net.Conn
	text           *textproto.Conn
	baseWorkingDir string
	passive        bool
}

// Pre-built FTP command byte slices for no-argument commands.
var (
	cmdTYPEI = []byte("TYPE I\r\n")
	cmdPWD   = []byte("PWD\r\n")
	cmdEPSV  = []byte("EPSV\r\n")
	cmdPASV  = []byte("PASV\r\n")
	cmdAUTH  = []byte("AUTH TLS\r\n")
)

// cmdBufPool provides reusable byte buffers for building FTP command lines.
var cmdBufPool = sync.Pool{
	New: func() any { buf := make([]byte, 0, 128); return &buf },
}

// pasvBufPool provides reusable slices for PASV parsing (6 fields, 6 ints).
var pasvBufPool = sync.Pool{
	New: func() any { return make([]int, 6) },
}

// dataDialer is a reusable dialer for data connections with socket options pre-configured.
var dataDialer = &net.Dialer{
	Control: func(network, addr string, c syscall.RawConn) error {
		var setErr error
		ctrlErr := c.Control(func(fd uintptr) {
			if err := setSockOptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
				setErr = err
				return
			}
			if network == "tcp" || network == "tcp4" || network == "tcp6" {
				setErr = setSockOptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)
			}
		})
		if ctrlErr != nil {
			return ctrlErr
		}
		return setErr
	},
}

// getCmdBuf returns a pooled byte buffer, cleared for use.
func getCmdBuf() *[]byte {
	buf := cmdBufPool.Get().(*[]byte)
	*buf = (*buf)[:0]
	return buf
}

// putCmdBuf returns a byte buffer to the pool.
func putCmdBuf(buf *[]byte) {
	cmdBufPool.Put(buf)
}

// Dial connects to an FTP server, reads the banner, and performs login.
func Dial(ctx context.Context, dialer *netx.Dialer, addr string, opt Opt) (*Conn, error) {
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, ftpError("dial", err)
	}

	if opt.TLSMode == 2 {
		if opt.TLSConfig == nil {
			conn.Close()
			return nil, errors.New("ftp: implicit TLS requires TLSConfig")
		}
		tlsConn := tls.Client(conn, cloneTLSConfig(opt.TLSConfig, addr))
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, ftpError("TLS handshake", err)
		}
		conn = tlsConn
	}

	rc := &recvLimited{Conn: conn}
	c := &Conn{
		conn:    conn,
		text:    textproto.NewConn(rc),
		passive: opt.Passive,
	}

	code, _, err := c.text.ReadResponse(220)
	if err != nil {
		c.conn.Close()
		return nil, ftpError("read banner", err)
	}
	_ = code

	if opt.TLSMode == 1 {
		if err := c.authTLS(ctx, opt.TLSConfig, addr); err != nil {
			c.conn.Close()
			return nil, err
		}
	}

	if err := c.login(ctx, opt.User, opt.Pass); err != nil {
		c.conn.Close()
		return nil, err
	}

	if _, _, err := c.cmdBytes(200, cmdTYPEI); err != nil {
		c.conn.Close()
		return nil, err
	}

	pwd, err := c.Pwd(ctx)
	if err != nil {
		c.conn.Close()
		return nil, err
	}
	c.baseWorkingDir = pwd

	return c, nil
}

func (c *Conn) authTLS(ctx context.Context, cfg *tls.Config, addr string) error {
	if cfg == nil {
		return errors.New("ftp: explicit TLS requires TLSConfig")
	}

	if _, err := c.conn.Write(cmdAUTH); err != nil {
		return ftpError("AUTH TLS write", err)
	}

	code, _, err := c.text.ReadResponse(234)
	if err != nil {
		return err
	}
	if code != 234 {
		return ftpError("AUTH TLS", fmt.Errorf("unexpected code %d", code))
	}

	tlsConn := tls.Client(c.conn, cloneTLSConfig(cfg, addr))
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return ftpError("TLS handshake", err)
	}
	c.conn = tlsConn
	rc := &recvLimited{Conn: tlsConn}
	c.text = textproto.NewConn(rc)
	return nil
}

func (c *Conn) login(ctx context.Context, user, pass string) error {
	if user == "" {
		user = "anonymous"
	}
	if pass == "" {
		pass = "ARIA2USER@"
	}
	code, _, err := c.cmdBytesString(0, "USER ", user)
	if err != nil {
		return err
	}
	_ = ctx
	if code == 230 {
		return nil
	}
	if code != 331 {
		return ftpError("USER", fmt.Errorf("unexpected code %d", code))
	}
	code, _, err = c.cmdBytesString(230, "PASS ", pass)
	if err != nil {
		return err
	}
	if code != 230 {
		return ftpError("PASS", fmt.Errorf("unexpected code %d", code))
	}
	return nil
}

// cmdBytesInt builds and sends a command with an integer argument.
func (c *Conn) cmdBytesInt(expected int, prefix string, n int64) (int, string, error) {
	buf := getCmdBuf()
	*buf = append(*buf, prefix...)
	*buf = strconv.AppendInt(*buf, n, 10)
	*buf = append(*buf, '\r', '\n')
	code, msg, err := c.sendAndRead(expected, *buf)
	putCmdBuf(buf)
	return code, msg, err
}

// cmdBytesString builds and sends a command with a string argument.
func (c *Conn) cmdBytesString(expected int, prefix, arg string) (int, string, error) {
	buf := getCmdBuf()
	*buf = append(*buf, prefix...)
	*buf = append(*buf, arg...)
	*buf = append(*buf, '\r', '\n')
	code, msg, err := c.sendAndRead(expected, *buf)
	putCmdBuf(buf)
	return code, msg, err
}

// cmdBytes writes a pre-built command and reads the response.
func (c *Conn) cmdBytes(expected int, cmd []byte) (int, string, error) {
	return c.sendAndRead(expected, cmd)
}

// sendAndRead writes a command to the control connection and reads the response.
func (c *Conn) sendAndRead(expected int, cmd []byte) (int, string, error) {
	if _, err := c.text.W.Write(cmd); err != nil {
		return 0, "", ftpError("write", err)
	}
	if err := c.text.W.Flush(); err != nil {
		return 0, "", ftpError("flush", err)
	}
	code, msg, err := c.text.ReadResponse(expected)
	if err != nil {
		return code, msg, ftpError("ftp", err)
	}
	return code, msg, nil
}

// Cmd sends a formatted FTP command and reads the response.
// For performance, prefer cmdBytes, cmdBytesString, or cmdBytesInt methods
// when the command format is known at build time.
func (c *Conn) Cmd(expected int, format string, args ...interface{}) (int, string, error) {
	buf := getCmdBuf()
	*buf = fmt.Appendf(*buf, format, args...)
	*buf = append(*buf, '\r', '\n')
	code, msg, err := c.sendAndRead(expected, *buf)
	putCmdBuf(buf)
	if err != nil {
		return code, msg, ftpError(format, err)
	}
	return code, msg, nil
}

// Size returns the size of a file on the FTP server using the SIZE command.
func (c *Conn) Size(ctx context.Context, path string) (int64, error) {
	_, msg, err := c.cmdBytesString(213, "SIZE ", path)
	if err != nil {
		return 0, err
	}
	_ = ctx
	size, err := strconv.ParseInt(strings.TrimSpace(msg), 10, 64)
	if err != nil {
		return 0, ftpError("SIZE parse", err)
	}
	if size < 0 {
		return 0, ftpError("SIZE", errors.New("negative size"))
	}
	return size, nil
}

// Pwd sends PWD and parses the current working directory from the 257 response.
func (c *Conn) Pwd(ctx context.Context) (string, error) {
	_, msg, err := c.cmdBytes(257, cmdPWD)
	if err != nil {
		return "", err
	}
	_ = ctx
	start := strings.Index(msg, "\"")
	if start < 0 {
		return "", ftpError("PWD parse", errors.New("no opening quote"))
	}
	end := strings.Index(msg[start+1:], "\"")
	if end < 0 {
		return "", ftpError("PWD parse", errors.New("no closing quote"))
	}
	return msg[start+1 : start+1+end], nil
}

// BaseWorkingDir returns the base working directory obtained from PWD during login.
func (c *Conn) BaseWorkingDir() string {
	return c.baseWorkingDir
}

// Mdtm sends MDTM and parses the file modification time.
func (c *Conn) Mdtm(ctx context.Context, path string) (time.Time, error) {
	_, msg, err := c.cmdBytesString(213, "MDTM ", path)
	if err != nil {
		return time.Time{}, err
	}
	_ = ctx
	msg = strings.TrimSpace(msg)
	if len(msg) < 14 {
		return time.Time{}, ftpError("MDTM parse", errors.New("response too short for YYYYMMDDhhmmss"))
	}
	t, err := time.ParseInLocation("20060102150405", msg[:14], time.UTC)
	if err != nil {
		return time.Time{}, ftpError("MDTM parse", err)
	}
	return t.In(time.UTC), nil
}

// Cwd sends CWD to change the remote working directory.
func (c *Conn) Cwd(ctx context.Context, dir string) error {
	_, _, err := c.cmdBytesString(250, "CWD ", dir)
	_ = ctx
	return err
}

// Retrieve initiates a RETR download and returns the data connection.
func (c *Conn) Retrieve(ctx context.Context, path string, offset int64) (io.ReadCloser, error) {
	var dataConn net.Conn
	var err error

	if c.optPassive() {
		dataConn, err = c.dialPassive(ctx)
	} else {
		return nil, errors.New("ftp: active mode not implemented")
	}
	if err != nil {
		return nil, err
	}

	code, _, err := c.cmdBytesInt(0, "REST ", offset)
	if err != nil {
		dataConn.Close()
		return nil, err
	}
	if code != 350 {
		if offset != 0 {
			dataConn.Close()
			return nil, ftpError("REST", fmt.Errorf("server rejected REST: code %d", code))
		}
	}

	code, _, err = c.cmdBytesString(0, "RETR ", path)
	if err != nil {
		dataConn.Close()
		return nil, err
	}
	if code != 150 && code != 125 {
		dataConn.Close()
		return nil, ftpError("RETR", fmt.Errorf("unexpected code %d", code))
	}

	return &dataReader{Conn: dataConn, ctrl: c}, nil
}

func (c *Conn) optPassive() bool {
	return c.passive
}

func (c *Conn) dialPassive(ctx context.Context) (net.Conn, error) {
	_, msg, err := c.cmdBytes(229, cmdEPSV)
	if err != nil {
		msg = ""
		_, msg, err = c.cmdBytes(227, cmdPASV)
		if err != nil {
			return nil, err
		}
		_, port, perr := parsePASV(msg)
		if perr != nil {
			return nil, perr
		}
		return c.dialData(ctx, c.remoteHost(), port)
	}
	port, perr := parseEPSV(msg)
	if perr != nil {
		return nil, perr
	}
	return c.dialData(ctx, c.remoteHost(), port)
}

func (c *Conn) dialData(ctx context.Context, host string, port int) (net.Conn, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := dataDialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, ftpError("data dial", err)
	}
	return conn, nil
}

func (c *Conn) remoteHost() string {
	host, _, _ := net.SplitHostPort(c.conn.RemoteAddr().String())
	return host
}

// Close closes the FTP control connection.
func (c *Conn) Close() error {
	return c.conn.Close()
}

type dataReader struct {
	net.Conn
	ctrl *Conn
}

func (r *dataReader) Close() error {
	dataErr := r.Conn.Close()
	code, _, ctrlErr := r.ctrl.text.ReadResponse(226)
	if ctrlErr != nil {
		if dataErr != nil {
			return errors.Join(dataErr, ftpError("transfer complete", ctrlErr))
		}
		return ftpError("transfer complete", ctrlErr)
	}
	if code != 226 {
		err := ftpError("transfer complete", fmt.Errorf("unexpected code %d", code))
		if dataErr != nil {
			return errors.Join(dataErr, err)
		}
		return err
	}
	return dataErr
}

func parsePASV(msg string) (string, int, error) {
	start := strings.Index(msg, "(")
	end := strings.LastIndex(msg, ")")
	if start < 0 || end < 0 || end <= start {
		return "", 0, ftpError("PASV parse", errors.New("no parentheses in PASV response"))
	}
	inner := msg[start+1 : end]
	st := 0
	nums := pasvBufPool.Get().([]int)
	nums = nums[:0]
	for i := 0; i <= len(inner); i++ {
		if i == len(inner) || inner[i] == ',' {
			if i > st {
				n, err := strconv.Atoi(strings.TrimSpace(inner[st:i]))
				if err != nil {
					pasvBufPool.Put(nums)
					return "", 0, ftpError("PASV parse", err)
				}
				if len(nums) < 4 && (n < 0 || n > 255) {
					pasvBufPool.Put(nums)
					return "", 0, ftpError("PASV parse", fmt.Errorf("octet %d out of range: %d", len(nums)+1, n))
				}
				nums = append(nums, n)
			}
			st = i + 1
		}
	}
	if len(nums) != 6 {
		pasvBufPool.Put(nums)
		return "", 0, ftpError("PASV parse", errors.New("expected 6 comma-separated fields"))
	}
	host := buildHost(nums[:4])
	port := nums[4]*256 + nums[5]
	if port < 1 || port > 65535 {
		pasvBufPool.Put(nums)
		return "", 0, ftpError("PASV parse", fmt.Errorf("port out of range: %d", port))
	}
	pasvBufPool.Put(nums)
	return host, port, nil
}

func buildHost(octets []int) string {
	buf := getCmdBuf()
	*buf = strconv.AppendInt(*buf, int64(octets[0]), 10)
	for i := 1; i < 4; i++ {
		*buf = append(*buf, '.')
		*buf = strconv.AppendInt(*buf, int64(octets[i]), 10)
	}
	s := string(*buf)
	putCmdBuf(buf)
	return s
}

func parseEPSV(msg string) (int, error) {
	start := strings.Index(msg, "(")
	if start < 0 {
		return 0, ftpError("EPSV parse", errors.New("no parentheses in EPSV response"))
	}
	end := strings.Index(msg, ")")
	if end < 0 {
		return 0, ftpError("EPSV parse", errors.New("missing closing parenthesis"))
	}
	if start > end {
		return 0, ftpError("EPSV parse", errors.New("closing parenthesis before opening parenthesis"))
	}
	inner := msg[start+1 : end]
	parts := strings.Split(inner, "|")
	if len(parts) != 5 {
		return 0, ftpError("EPSV parse", fmt.Errorf("expected 5 pipe-separated fields, got %d", len(parts)))
	}
	port, err := strconv.Atoi(strings.TrimSpace(parts[3]))
	if err != nil {
		return 0, ftpError("EPSV parse", err)
	}
	if port < 1 || port > 65535 {
		return 0, ftpError("EPSV parse", fmt.Errorf("port out of range: %d", port))
	}
	return port, nil
}

func cloneTLSConfig(cfg *tls.Config, addr string) *tls.Config {
	if cfg == nil {
		return nil
	}
	c := cfg.Clone()
	if c.ServerName == "" {
		host, _, err := net.SplitHostPort(addr)
		if err == nil {
			c.ServerName = host
		}
	}
	return c
}

func ftpError(op string, err error) error {
	return core.WrapError(core.ExitFTPProtocolError, op, err)
}
