// Package sftp implements the SFTP protocol (draft-ietf-secsh-filexfer-13)
// over an SSH channel. It provides file transfer, stat, and session
// management for the aria2go download engine.
package sftp

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/smartass08/aria2go/internal/netx"
	sshagent "github.com/smartass08/aria2go/internal/ssh/agent"
	"github.com/smartass08/aria2go/internal/ssh/channel"
	sshkeys "github.com/smartass08/aria2go/internal/ssh/keys"
	"github.com/smartass08/aria2go/internal/ssh/knownhosts"
	"github.com/smartass08/aria2go/internal/ssh/transport"
	"github.com/smartass08/aria2go/internal/ssh/userauth"
)

const (
	sshFxpInit     = 1
	sshFxpVersion  = 2
	sshFxpOpen     = 3
	sshFxpClose    = 4
	sshFxpRead     = 5
	sshFxpWrite    = 6
	sshFxpStat     = 17
	sshFxpFstat    = 8
	sshFxpSetstat  = 9
	sshFxpOpendir  = 11
	sshFxpReaddir  = 12
	sshFxpRemove   = 13
	sshFxpMkdir    = 14
	sshFxpRmdir    = 15
	sshFxpRealpath = 16
	sshFxpRename   = 18
	sshFxpReadlink = 19
	sshFxpSymlink  = 20
	sshFxpStatus   = 101
	sshFxpHandle   = 102
	sshFxpData     = 103
	sshFxpName     = 104
	sshFxpAttrs    = 105
)

const (
	sshFxfRead   = 0x00000001
	sshFxfWrite  = 0x00000002
	sshFxfAppend = 0x00000004
	sshFxfCreat  = 0x00000008
	sshFxfTrunc  = 0x00000010
	sshFxfExcl   = 0x00000020
)

const (
	sshFxOk               = 0
	sshFxEOF              = 1
	sshFxNoSuchFile       = 2
	sshFxPermissionDenied = 3
	sshFxFailure          = 4
	sshFxBadMessage       = 5
	sshFxNoConnection     = 6
	sshFxConnectionLost   = 7
	sshFxOpUnsupported    = 8
)

const (
	attrSize        = 0x00000001
	attrUidGid      = 0x00000002
	attrPermissions = 0x00000004
	attrAcmodTime   = 0x00000008
)

const defaultReadSize = 1 << 15

var (
	ErrSftpProtocol = errors.New("sftp: protocol error")
	ErrSftpEOF      = errors.New("sftp: end of file")
	ErrSftpStatus   = errors.New("sftp: status error")

	errHostKeyDigest = errors.New("sftp: host key digest error")
)

// IsHostKeyDigestError reports whether err came from ssh-host-key-md verification.
func IsHostKeyDigestError(err error) bool {
	return errors.Is(err, errHostKeyDigest)
}

var (
	initPacket []byte

	bodyPool = sync.Pool{
		New: func() any {
			buf := make([]byte, 0, 4096)
			return &buf
		},
	}
)

func init() {
	initPacket = buildINIT()
}

type Driver struct{}

type Opts struct {
	Host           string
	Port           int
	User           string
	Auth           AuthMethods
	HostKeyMD      string
	KnownHostsPath string
	Timeout        time.Duration
}

type AuthMethods struct {
	Password    string
	KeyFile     string
	KeyPassphrase string
	AgentSocket string
}

type Session struct {
	conn    *transport.Conn
	ch      *channel.Channel
	nextID  uint32
	version uint32
}

type FileInfo struct {
	Name    string
	Size    int64
	IsDir   bool
	ModTime time.Time
}

func Open(ctx context.Context, dialer *netx.Dialer, opts Opts) (*Session, error) {
	if opts.Port == 0 {
		opts.Port = 22
	}
	if opts.User == "" {
		opts.User = "anonymous"
	}
	if opts.Auth.Password == "" && opts.Auth.KeyFile == "" && opts.Auth.AgentSocket == "" {
		opts.Auth.Password = "ARIA2USER@"
	}

	addr := net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port))
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("sftp: dial %s: %w", addr, err)
	}

	tconn, sessionID, err := transport.ClientHandshake(conn, transport.ClientConfig{
		KEXAlgorithms:     []string{"curve25519-sha256", "diffie-hellman-group14-sha256"},
		HostKeyAlgorithms: []string{"ssh-ed25519", "ssh-rsa"},
		Ciphers:           []string{"aes128-ctr", "aes256-ctr"},
		MACs:              []string{"hmac-sha2-256"},
		Compression:       []string{"none"},
	})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("sftp: handshake: %w", err)
	}
	if err := verifyHostKey(tconn.HostKey(), conn.RemoteAddr(), opts); err != nil {
		tconn.Close()
		return nil, fmt.Errorf("sftp: host key: %w", err)
	}

	var authMethods []userauth.AuthMethod
	if opts.Auth.Password != "" {
		authMethods = append(authMethods, &userauth.PasswordAuth{
			Username: opts.User,
			Password: opts.Auth.Password,
		})
	}
	if opts.Auth.KeyFile != "" {
		keyData, err := os.ReadFile(opts.Auth.KeyFile)
		if err != nil {
			tconn.Close()
			return nil, fmt.Errorf("sftp: read key file: %w", err)
		}
		key, err := parsePrivateKeyMaybeEncrypted(keyData, opts.Auth.KeyPassphrase)
		if err != nil {
			tconn.Close()
			return nil, fmt.Errorf("sftp: parse key: %w", err)
		}
		authMethods = append(authMethods, &userauth.PublicKeyAuth{
			Username: opts.User,
			Key:      key,
		})
	}
	if opts.Auth.AgentSocket != "" {
		ag := &sshagent.Agent{}
		if err := ag.Connect(opts.Auth.AgentSocket); err == nil {
			defer ag.Close()
			ids, err := ag.List()
			if err != nil {
				if len(authMethods) == 0 {
					tconn.Close()
					return nil, fmt.Errorf("sftp: agent identities: %w", err)
				}
			} else {
				for _, id := range ids {
					authMethods = append(authMethods, &userauth.PublicKeyAuth{
						Username: opts.User,
						Key:      sshagent.NewSigner(ag, id),
					})
				}
			}
		} else if len(authMethods) == 0 {
			tconn.Close()
			return nil, fmt.Errorf("sftp: agent connect: %w", err)
		}
	}
	if len(authMethods) == 0 {
		tconn.Close()
		return nil, errors.New("sftp: no authentication method provided")
	}

	authClient := userauth.NewClient(tconn)
	if err := authClient.Authenticate(sessionID, authMethods); err != nil {
		tconn.Close()
		return nil, fmt.Errorf("sftp: auth: %w", err)
	}

	ch, err := channel.OpenSession(tconn)
	if err != nil {
		tconn.Close()
		return nil, fmt.Errorf("sftp: open session: %w", err)
	}

	if err := ch.Subsystem("sftp"); err != nil {
		ch.Close()
		tconn.Close()
		return nil, fmt.Errorf("sftp: subsystem: %w", err)
	}

	s := &Session{
		conn:    tconn,
		ch:      ch,
		nextID:  0,
		version: 0,
	}

	if err := s.init(); err != nil {
		s.Close()
		return nil, fmt.Errorf("sftp: init: %w", err)
	}

	return s, nil
}

func (s *Session) init() error {
	if _, err := s.ch.Write(initPacket); err != nil {
		return fmt.Errorf("write INIT: %w", err)
	}

	resp, err := s.readPacket(context.Background())
	if err != nil {
		return fmt.Errorf("read VERSION: %w", err)
	}

	if resp.typ != sshFxpVersion {
		return fmt.Errorf("expected VERSION, got %d: %w", resp.typ, ErrSftpProtocol)
	}

	ver, err := parseVERSION(resp.payload)
	if err != nil {
		return fmt.Errorf("parse VERSION: %w", err)
	}
	if ver < 3 {
		return fmt.Errorf("unsupported SFTP version %d", ver)
	}

	s.version = ver
	return nil
}

func (s *Session) Stat(ctx context.Context, path string) (FileInfo, error) {
	path = decodePath(path)
	id := s.nextID
	s.nextID++

	pkt := buildSTAT(id, path)
	if _, err := s.ch.Write(pkt); err != nil {
		return FileInfo{}, fmt.Errorf("sftp: stat write: %w", err)
	}

	resp, err := s.readPacket(ctx)
	if err != nil {
		return FileInfo{}, fmt.Errorf("sftp: stat response: %w", err)
	}
	if resp.id != id {
		return FileInfo{}, fmt.Errorf("sftp: stat response id mismatch: got %d, want %d: %w", resp.id, id, ErrSftpProtocol)
	}

	if resp.typ == sshFxpStatus {
		code, msg, err := parseSTATUS(resp.payload)
		if err != nil {
			return FileInfo{}, fmt.Errorf("sftp: stat status: %w", err)
		}
		if isErr(code) {
			return FileInfo{}, fmt.Errorf("sftp: stat %s: %s: %w", path, msg, ErrSftpStatus)
		}
		return FileInfo{}, fmt.Errorf("sftp: unexpected status %d: %s: %w", code, msg, ErrSftpStatus)
	}

	if resp.typ != sshFxpAttrs {
		return FileInfo{}, fmt.Errorf("sftp: expected ATTRS, got %d: %w", resp.typ, ErrSftpProtocol)
	}

	fi, err := parseATTRS(resp.payload)
	if err != nil {
		return FileInfo{}, fmt.Errorf("sftp: parse attrs: %w", err)
	}
	fi.Name = path
	return fi, nil
}

func (s *Session) OpenFile(ctx context.Context, path string, offset int64) (io.ReadCloser, error) {
	if offset < 0 {
		return nil, fmt.Errorf("sftp: negative offset %d: %w", offset, ErrSftpProtocol)
	}
	path = decodePath(path)

	id := s.nextID
	s.nextID++

	pkt := buildOPEN(id, path, sshFxfRead)
	if _, err := s.ch.Write(pkt); err != nil {
		return nil, fmt.Errorf("sftp: open write: %w", err)
	}

	resp, err := s.readPacket(ctx)
	if err != nil {
		return nil, fmt.Errorf("sftp: open response: %w", err)
	}
	if resp.id != id {
		return nil, fmt.Errorf("sftp: open response id mismatch: got %d, want %d: %w", resp.id, id, ErrSftpProtocol)
	}

	if resp.typ == sshFxpStatus {
		code, msg, err := parseSTATUS(resp.payload)
		if err != nil {
			return nil, fmt.Errorf("sftp: open status: %w", err)
		}
		return nil, fmt.Errorf("sftp: open %s: %s: %w", path, msg, statusCodeErr(code))
	}

	if resp.typ != sshFxpHandle {
		return nil, fmt.Errorf("sftp: expected HANDLE, got %d: %w", resp.typ, ErrSftpProtocol)
	}

	handle, err := parseHANDLE(resp.payload)
	if err != nil {
		return nil, fmt.Errorf("sftp: parse handle: %w", err)
	}

	return &fileReader{
		sess:      s,
		ctx:       ctx,
		handle:    handle,
		offset:    uint64(offset),
		closeSent: false,
	}, nil
}

func (s *Session) Close() error {
	var firstErr error
	if s.ch != nil {
		if err := s.ch.Close(); err != nil {
			firstErr = err
		}
	}
	if s.conn != nil {
		if err := s.conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func verifyHostKey(hostKey []byte, remote net.Addr, opts Opts) error {
	if err := verifyHostKeyDigest(hostKey, opts.HostKeyMD); err != nil {
		return err
	}
	if opts.HostKeyMD != "" {
		return nil
	}
	path := opts.KnownHostsPath
	if path == "" {
		path = defaultKnownHostsPath()
	}
	if path == "" {
		return nil
	}
	cb, err := knownhosts.New(path)
	if err != nil {
		if os.IsNotExist(err) || errors.Is(err, knownhosts.ErrNoMatch) {
			return nil
		}
		return fmt.Errorf("known_hosts: %w", err)
	}
	keyType, err := knownhosts.KeyType(hostKey)
	if err != nil {
		return nil
	}
	err = cb(knownhosts.HostPort(opts.Host, opts.Port), remote, keyType, hostKey)
	if err == nil {
		return nil
	}
	if !errors.Is(err, knownhosts.ErrNoMatch) {
		return err
	}
	err = cb(opts.Host, remote, keyType, hostKey)
	if err == nil || errors.Is(err, knownhosts.ErrNoMatch) {
		return nil
	}
	return err
}

func defaultKnownHostsPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".ssh", "known_hosts")
}

func verifyHostKeyDigest(hostKey []byte, spec string) error {
	if spec == "" {
		return nil
	}
	hashType, digestHex, ok := strings.Cut(spec, "=")
	if !ok || hashType == "" || digestHex == "" {
		return fmt.Errorf("invalid ssh-host-key-md %q: want TYPE=DIGEST: %w", spec, errHostKeyDigest)
	}
	want, err := hex.DecodeString(digestHex)
	if err != nil {
		return fmt.Errorf("invalid ssh-host-key-md digest: %w", errors.Join(errHostKeyDigest, err))
	}

	var got []byte
	switch hashType {
	case "sha-1":
		sum := sha1.Sum(hostKey)
		got = sum[:]
	case "md5":
		sum := md5.Sum(hostKey)
		got = sum[:]
	default:
		return fmt.Errorf("unsupported ssh-host-key-md type %q: %w", hashType, errHostKeyDigest)
	}
	if len(want) != len(got) || subtle.ConstantTimeCompare(want, got) != 1 {
		return fmt.Errorf("unexpected SSH host key: expected %s, actual %s: %w", strings.ToLower(digestHex), hex.EncodeToString(got), errHostKeyDigest)
	}
	return nil
}

type fPacket struct {
	typ     byte
	id      uint32
	payload []byte
}

func (s *Session) readPacket(ctx context.Context) (fPacket, error) {
	var hdr [4]byte
	if err := readFullCtx(ctx, s.ch, hdr[:]); err != nil {
		return fPacket{}, fmt.Errorf("read header: %w", err)
	}

	length := binary.BigEndian.Uint32(hdr[:])
	if length < 1 {
		return fPacket{}, fmt.Errorf("packet too short: %d: %w", length, ErrSftpProtocol)
	}

	bodyBuf, body := getBodyBuf(int(length))
	if err := readFullCtx(ctx, s.ch, body); err != nil {
		putBodyBuf(bodyBuf)
		return fPacket{}, fmt.Errorf("read body: %w", err)
	}

	typ, id, err := parseFPacket(body)
	if err != nil {
		putBodyBuf(bodyBuf)
		return fPacket{}, fmt.Errorf("parse packet: %w", err)
	}

	var payload []byte
	if typ == sshFxpInit || typ == sshFxpVersion {
		payload = append([]byte(nil), body[1:]...)
	} else {
		if len(body) < 5 {
			putBodyBuf(bodyBuf)
			return fPacket{}, fmt.Errorf("packet body too short for request id: %d: %w", len(body), ErrSftpProtocol)
		}
		payload = append([]byte(nil), body[5:]...)
	}
	putBodyBuf(bodyBuf)

	return fPacket{typ: typ, id: id, payload: payload}, nil
}

func getBodyBuf(n int) (buf *[]byte, out []byte) {
	bp := bodyPool.Get().(*[]byte)
	b := *bp
	if cap(b) < n {
		b = make([]byte, n)
	} else {
		b = b[:n]
	}
	*bp = b
	return bp, b
}

func putBodyBuf(bp *[]byte) {
	bodyPool.Put(bp)
}

func parseFPacket(body []byte) (typ byte, id uint32, err error) {
	typ = body[0]
	if typ == sshFxpInit || typ == sshFxpVersion {
		return typ, 0, nil
	}
	if len(body) < 5 {
		return 0, 0, fmt.Errorf("body too short for id: %d", len(body))
	}
	id = binary.BigEndian.Uint32(body[1:5])
	return typ, id, nil
}

func readFullCtx(ctx context.Context, r io.Reader, buf []byte) error {
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(r, buf)
		done <- err
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func encodeFPacket(typ byte, id uint32, payload []byte, hasID bool) []byte {
	var bodyLen int
	if hasID {
		bodyLen = 1 + 4 + len(payload)
	} else {
		bodyLen = 1 + len(payload)
	}

	buf := make([]byte, 4+bodyLen)
	binary.BigEndian.PutUint32(buf[:4], uint32(bodyLen))
	buf[4] = typ
	if hasID {
		binary.BigEndian.PutUint32(buf[5:9], id)
		copy(buf[9:], payload)
	} else {
		copy(buf[5:], payload)
	}
	return buf
}

func decodeFPacket(data []byte, hasID bool) (typ byte, id uint32, payload []byte, err error) {
	length := binary.BigEndian.Uint32(data[:4])
	if hasID {
		minLen := 9
		if len(data) < minLen {
			return 0, 0, nil, fmt.Errorf("packet too short: %d", len(data))
		}
		if int(length)+4 != len(data) {
			return 0, 0, nil, fmt.Errorf("length mismatch: %d+4 != %d", length, len(data))
		}
		typ = data[4]
		id = binary.BigEndian.Uint32(data[5:9])
		payload = data[9:]
	} else {
		minLen := 5
		if len(data) < minLen {
			return 0, 0, nil, fmt.Errorf("packet too short: %d", len(data))
		}
		if int(length)+4 != len(data) {
			return 0, 0, nil, fmt.Errorf("length mismatch: %d+4 != %d", length, len(data))
		}
		typ = data[4]
		id = 0
		payload = data[5:]
	}
	return typ, id, payload, nil
}

func buildINIT() []byte {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, 3)
	return encodeFPacket(sshFxpInit, 0, payload, false)
}

func buildOPEN(id uint32, path string, pflags uint32) []byte {
	payload := make([]byte, 4+len(path)+4+4)
	binary.BigEndian.PutUint32(payload[:4], uint32(len(path)))
	copy(payload[4:], path)
	off := 4 + len(path)
	binary.BigEndian.PutUint32(payload[off:off+4], pflags)
	return encodeFPacket(sshFxpOpen, id, payload, true)
}

func buildREAD(id uint32, handle []byte, offset uint64, length uint32) []byte {
	payload := make([]byte, 4+len(handle)+8+4)
	binary.BigEndian.PutUint32(payload[:4], uint32(len(handle)))
	copy(payload[4:], handle)
	off := 4 + len(handle)
	binary.BigEndian.PutUint64(payload[off:off+8], offset)
	off += 8
	binary.BigEndian.PutUint32(payload[off:off+4], length)
	return encodeFPacket(sshFxpRead, id, payload, true)
}

func buildCLOSE(id uint32, handle []byte) []byte {
	payload := make([]byte, 4+len(handle))
	binary.BigEndian.PutUint32(payload[:4], uint32(len(handle)))
	copy(payload[4:], handle)
	return encodeFPacket(sshFxpClose, id, payload, true)
}

func buildSTAT(id uint32, path string) []byte {
	payload := make([]byte, 4+len(path))
	binary.BigEndian.PutUint32(payload[:4], uint32(len(path)))
	copy(payload[4:], path)
	return encodeFPacket(sshFxpStat, id, payload, true)
}

func decodePath(path string) string {
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return path
	}
	return decoded
}

func parseVERSION(payload []byte) (uint32, error) {
	if len(payload) < 4 {
		return 0, fmt.Errorf("VERSION payload too short: %d", len(payload))
	}
	return binary.BigEndian.Uint32(payload[:4]), nil
}

func parseHANDLE(payload []byte) ([]byte, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("HANDLE payload too short: %d", len(payload))
	}
	hlen := binary.BigEndian.Uint32(payload[:4])
	if len(payload) < 4+int(hlen) {
		return nil, fmt.Errorf("HANDLE payload truncated: %d < %d", len(payload), 4+hlen)
	}
	h := make([]byte, hlen)
	copy(h, payload[4:4+hlen])
	return h, nil
}

func parseDATA(payload []byte) ([]byte, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("DATA payload too short: %d", len(payload))
	}
	dlen := binary.BigEndian.Uint32(payload[:4])
	if len(payload) < 4+int(dlen) {
		return nil, fmt.Errorf("DATA payload truncated: %d < %d", len(payload), 4+dlen)
	}
	d := make([]byte, dlen)
	copy(d, payload[4:4+dlen])
	return d, nil
}

func parseSTATUS(payload []byte) (code uint32, msg string, err error) {
	if len(payload) < 4 {
		return 0, "", fmt.Errorf("STATUS payload too short: %d", len(payload))
	}
	code = binary.BigEndian.Uint32(payload[:4])
	pos := 4

	if len(payload) < pos+4 {
		return code, "", nil
	}
	mlen := binary.BigEndian.Uint32(payload[pos:])
	pos += 4
	if len(payload) < pos+int(mlen) {
		return code, "", fmt.Errorf("STATUS message truncated: %d < %d", len(payload)-pos, mlen)
	}
	msg = string(payload[pos : pos+int(mlen)])
	pos += int(mlen)
	if len(payload) == pos {
		return code, msg, nil
	}
	if len(payload) < pos+4 {
		return code, msg, fmt.Errorf("STATUS language length truncated")
	}
	llen := binary.BigEndian.Uint32(payload[pos:])
	pos += 4
	if len(payload) < pos+int(llen) {
		return code, msg, fmt.Errorf("STATUS language truncated: %d < %d", len(payload)-pos, llen)
	}
	return code, msg, nil
}

func parseATTRS(payload []byte) (FileInfo, error) {
	if len(payload) < 4 {
		return FileInfo{}, fmt.Errorf("ATTRS payload too short: %d", len(payload))
	}
	flags := binary.BigEndian.Uint32(payload[:4])
	pos := 4

	fi := FileInfo{}
	fields := []struct {
		flag   uint32
		offset int
		size   int
	}{
		{attrSize, 0, 8},
		{attrUidGid, 0, 8},
		{attrPermissions, 0, 4},
		{attrAcmodTime, 0, 8},
	}

	for _, f := range fields {
		if flags&f.flag != 0 {
			if f.flag == attrSize {
				if len(payload) < pos+8 {
					return FileInfo{}, fmt.Errorf("ATTRS truncated at size")
				}
				fi.Size = int64(binary.BigEndian.Uint64(payload[pos:]))
				pos += 8
			} else if f.flag == attrUidGid {
				if len(payload) < pos+8 {
					return FileInfo{}, fmt.Errorf("ATTRS truncated at uidgid")
				}
				pos += 8
			} else if f.flag == attrPermissions {
				if len(payload) < pos+4 {
					return FileInfo{}, fmt.Errorf("ATTRS truncated at permissions")
				}
				perm := binary.BigEndian.Uint32(payload[pos:])
				fi.IsDir = (perm & 0170000) == 0040000
				pos += 4
			} else if f.flag == attrAcmodTime {
				if len(payload) < pos+8 {
					return FileInfo{}, fmt.Errorf("ATTRS truncated at acmodtime")
				}
				mtime := binary.BigEndian.Uint32(payload[pos+4:])
				fi.ModTime = time.Unix(int64(mtime), 0).UTC()
				pos += 8
			}
		}
	}

	return fi, nil
}

func isEOF(code uint32) bool {
	return code == sshFxEOF
}

func isErr(code uint32) bool {
	return code != sshFxOk && code != sshFxEOF
}

func statusCodeErr(code uint32) error {
	switch code {
	case sshFxEOF:
		return ErrSftpEOF
	case sshFxNoSuchFile:
		return fmt.Errorf("no such file: %w", ErrSftpStatus)
	case sshFxPermissionDenied:
		return fmt.Errorf("permission denied: %w", ErrSftpStatus)
	case sshFxFailure:
		return fmt.Errorf("failure: %w", ErrSftpStatus)
	case sshFxBadMessage:
		return fmt.Errorf("bad message: %w", ErrSftpStatus)
	case sshFxNoConnection:
		return fmt.Errorf("no connection: %w", ErrSftpStatus)
	case sshFxConnectionLost:
		return fmt.Errorf("connection lost: %w", ErrSftpStatus)
	case sshFxOpUnsupported:
		return fmt.Errorf("operation unsupported: %w", ErrSftpStatus)
	default:
		return fmt.Errorf("status %d: %w", code, ErrSftpStatus)
	}
}

type fileReader struct {
	sess      *Session
	ctx       context.Context
	handle    []byte
	offset    uint64
	closeSent bool
}

func (r *fileReader) Read(p []byte) (int, error) {
	if r.closeSent {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}

	id := r.sess.nextID
	r.sess.nextID++

	reqLen := uint32(len(p))
	if reqLen > defaultReadSize {
		reqLen = defaultReadSize
	}

	pkt := buildREAD(id, r.handle, r.offset, reqLen)
	if _, err := r.sess.ch.Write(pkt); err != nil {
		return 0, fmt.Errorf("sftp: read request: %w", err)
	}

	resp, err := r.sess.readPacket(r.ctx)
	if err != nil {
		return 0, fmt.Errorf("sftp: read response: %w", err)
	}
	if resp.id != id {
		return 0, fmt.Errorf("sftp: read response id mismatch: got %d, want %d: %w", resp.id, id, ErrSftpProtocol)
	}

	if resp.typ == sshFxpStatus {
		code, msg, err := parseSTATUS(resp.payload)
		if err != nil {
			return 0, fmt.Errorf("sftp: read status parse: %w", err)
		}
		if code == sshFxEOF {
			return 0, io.EOF
		}
		return 0, fmt.Errorf("sftp: read status %d: %s: %w", code, msg, statusCodeErr(code))
	}

	if resp.typ != sshFxpData {
		return 0, fmt.Errorf("sftp: expected DATA, got %d: %w", resp.typ, ErrSftpProtocol)
	}

	data, err := parseDATA(resp.payload)
	if err != nil {
		return 0, fmt.Errorf("sftp: parse data: %w", err)
	}

	n := copy(p, data)
	r.offset += uint64(n)
	return n, nil
}

func (r *fileReader) Close() error {
	if r.closeSent {
		return nil
	}
	r.closeSent = true

	id := r.sess.nextID
	r.sess.nextID++

	pkt := buildCLOSE(id, r.handle)
	if _, err := r.sess.ch.Write(pkt); err != nil {
		return fmt.Errorf("sftp: close handle: %w", err)
	}

	resp, err := r.sess.readPacket(r.ctx)
	if err != nil {
		return fmt.Errorf("sftp: close response: %w", err)
	}
	if resp.id != id {
		return fmt.Errorf("sftp: close response id mismatch: got %d, want %d: %w", resp.id, id, ErrSftpProtocol)
	}

	if resp.typ == sshFxpStatus {
		code, _, err := parseSTATUS(resp.payload)
		if err != nil {
			return fmt.Errorf("sftp: close status parse: %w", err)
		}
		if code != sshFxOk {
			return fmt.Errorf("sftp: close status %d: %w", code, statusCodeErr(code))
		}
		return nil
	}

	return fmt.Errorf("sftp: expected STATUS, got %d: %w", resp.typ, ErrSftpProtocol)
}

func parsePrivateKey(data []byte) (any, error) {
	key, err := sshkeys.ParsePrivateKey(data)
	if err != nil {
		return nil, err
	}
	return key, nil
}

// parsePrivateKeyMaybeEncrypted parses a PEM-encoded private key, using
// passphrase to decrypt it when the passphrase is non-empty.  When passphrase
// is empty the key is assumed to be unencrypted.
func parsePrivateKeyMaybeEncrypted(data []byte, passphrase string) (any, error) {
	if passphrase == "" {
		return parsePrivateKey(data)
	}
	key, err := sshkeys.ParsePrivateKeyWithPassphrase(data, []byte(passphrase))
	if err != nil {
		return nil, err
	}
	return key, nil
}
