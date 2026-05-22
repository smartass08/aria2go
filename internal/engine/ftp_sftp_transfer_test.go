package engine

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/core"
	"github.com/smartass08/aria2go/internal/netx"
	ftpproto "github.com/smartass08/aria2go/internal/protocol/ftp"
	sftpproto "github.com/smartass08/aria2go/internal/protocol/sftp"
)

type stubFTPConn struct {
	sizeErr error
	size    int64
	mdtm    time.Time
	body    []byte
	calls   []string
}

func (c *stubFTPConn) Close() error { return nil }

func (c *stubFTPConn) Size(_ context.Context, path string) (int64, error) {
	c.calls = append(c.calls, "SIZE "+path)
	return c.size, c.sizeErr
}

func (c *stubFTPConn) Mdtm(_ context.Context, path string) (time.Time, error) {
	c.calls = append(c.calls, "MDTM "+path)
	return c.mdtm, nil
}

func (c *stubFTPConn) Retrieve(_ context.Context, path string, offset int64) (io.ReadCloser, error) {
	c.calls = append(c.calls, "RETR "+path)
	return io.NopCloser(bytes.NewReader(c.body[offset:])), nil
}

type stubSFTPSession struct {
	info sftpproto.FileInfo
	body []byte
}

func (s *stubSFTPSession) Close() error { return nil }

func (s *stubSFTPSession) Stat(_ context.Context, _ string) (sftpproto.FileInfo, error) {
	return s.info, nil
}

func (s *stubSFTPSession) OpenFile(_ context.Context, _ string, offset int64) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.body[offset:])), nil
}

func TestRunFTPDownloadSizeUnsupportedContinuesAndAppliesRemoteTime(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("ftp payload without SIZE")
	remoteTime := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	conn := &stubFTPConn{
		sizeErr: ftpproto.ErrSizeUnsupported,
		mdtm:    remoteTime,
		body:    payload,
	}

	orig := ftpDial
	ftpDial = func(_ context.Context, _ *netx.Dialer, _ string, _ ftpproto.Opt) (ftpTransferConn, error) {
		return conn, nil
	}
	t.Cleanup(func() { ftpDial = orig })

	e, err := New(&config.Options{
		Dir:                    dir,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		RemoteTime:             true,
		FTPPasv:                true,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	u, err := url.Parse("ftp://example.com/file.bin")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	rg := &requestGroup{
		gid:  core.GID(1),
		opts: &config.Options{RemoteTime: true, FTPPasv: true},
	}
	outPath := filepath.Join(dir, "file.bin")

	e.runFTPDownload(context.Background(), rg, u.String(), u, outPath)

	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	if rg.totalLength != 0 {
		t.Fatalf("totalLength = %d, want 0 for unknown size", rg.totalLength)
	}
	if rg.completedLength != int64(len(payload)) {
		t.Fatalf("completedLength = %d, want %d", rg.completedLength, len(payload))
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload = %q, want %q", got, payload)
	}
	st, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if !st.ModTime().Truncate(time.Second).Equal(remoteTime) {
		t.Fatalf("mtime = %s, want %s", st.ModTime(), remoteTime)
	}
	if len(conn.calls) < 2 || conn.calls[0] != "MDTM /file.bin" || conn.calls[1] != "SIZE /file.bin" {
		t.Fatalf("call order = %v, want MDTM before SIZE", conn.calls)
	}
}

func TestRunFTPDownloadPassesFTPType(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("ascii payload")
	var gotType string

	orig := ftpDial
	ftpDial = func(_ context.Context, _ *netx.Dialer, _ string, opt ftpproto.Opt) (ftpTransferConn, error) {
		gotType = opt.Type
		return &stubFTPConn{size: int64(len(payload)), body: payload}, nil
	}
	t.Cleanup(func() { ftpDial = orig })

	e, err := New(&config.Options{
		Dir:                    dir,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		FTPPasv:                true,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	u, err := url.Parse("ftp://example.com/file.txt")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	rg := &requestGroup{
		gid:  core.GID(2),
		opts: &config.Options{FTPPasv: true, FTPType: "ascii"},
	}

	e.runFTPDownload(context.Background(), rg, u.String(), u, filepath.Join(dir, "file.txt"))

	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	if gotType != "ascii" {
		t.Fatalf("ftpDial type = %q, want ascii", gotType)
	}
}

func TestRunSFTPDownloadAppliesRemoteTime(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("sftp payload")
	remoteTime := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)

	orig := sftpOpen
	sftpOpen = func(_ context.Context, _ *netx.Dialer, _ sftpproto.Opts) (sftpTransferSession, error) {
		return &stubSFTPSession{
			info: sftpproto.FileInfo{
				Name:    "/file.bin",
				Size:    int64(len(payload)),
				ModTime: remoteTime,
			},
			body: payload,
		}, nil
	}
	t.Cleanup(func() { sftpOpen = orig })

	e, err := New(&config.Options{
		Dir:                    dir,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		RemoteTime:             true,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	u, err := url.Parse("sftp://example.com/file.bin")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	rg := &requestGroup{
		gid:  core.GID(3),
		opts: &config.Options{RemoteTime: true},
	}
	outPath := filepath.Join(dir, "file.bin")

	e.runSFTPDownload(context.Background(), rg, u.String(), u, outPath)

	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload = %q, want %q", got, payload)
	}
	st, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if !st.ModTime().Truncate(time.Second).Equal(remoteTime) {
		t.Fatalf("mtime = %s, want %s", st.ModTime(), remoteTime)
	}
}

func TestRunSFTPDownloadPassesKeyAndAgentOptions(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("sftp key auth payload")
	var gotOpts sftpproto.Opts

	t.Setenv("SSH_AUTH_SOCK", "/tmp/test-agent.sock")

	orig := sftpOpen
	sftpOpen = func(_ context.Context, _ *netx.Dialer, opt sftpproto.Opts) (sftpTransferSession, error) {
		gotOpts = opt
		return &stubSFTPSession{
			info: sftpproto.FileInfo{Name: "/file.bin", Size: int64(len(payload))},
			body: payload,
		}, nil
	}
	t.Cleanup(func() { sftpOpen = orig })

	e, err := New(&config.Options{
		Dir:                    dir,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		PrivateKey:             "/tmp/test-key",
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	u, err := url.Parse("sftp://user@example.com/file.bin")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	rg := &requestGroup{
		gid:  core.GID(4),
		opts: &config.Options{PrivateKey: "/tmp/test-key"},
	}

	e.runSFTPDownload(context.Background(), rg, u.String(), u, filepath.Join(dir, "file.bin"))

	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	if gotOpts.Auth.KeyFile != "/tmp/test-key" {
		t.Fatalf("Auth.KeyFile = %q, want /tmp/test-key", gotOpts.Auth.KeyFile)
	}
	if gotOpts.Auth.AgentSocket != "/tmp/test-agent.sock" {
		t.Fatalf("Auth.AgentSocket = %q, want /tmp/test-agent.sock", gotOpts.Auth.AgentSocket)
	}
	if gotOpts.User != "user" {
		t.Fatalf("User = %q, want user", gotOpts.User)
	}
}
