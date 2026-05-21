package conformance

import (
	"path/filepath"
	"testing"
)

func TestBitTorrent_SingleFileDownloadParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("bittorrent-single-file-download-parity", 48*1024+777)
	const (
		name        = "bt-parity.bin"
		pieceLength = 16 * 1024
	)

	refBT := startProtocolBTFixture(t, name, payload, pieceLength)
	implBT := startProtocolBTFixture(t, name, payload, pieceLength)

	refDir := t.TempDir()
	implDir := t.TempDir()
	refTorrent := refBT.writeTorrentFile(t, refDir)
	implTorrent := implBT.writeTorrentFile(t, implDir)

	ref := protocolRun(t, true, bittorrentDownloadArgs(refDir, refTorrent))
	impl := protocolRun(t, false, bittorrentDownloadArgs(implDir, implTorrent))

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref bittorrent", ref)
	protocolRequireExitZero(t, "impl bittorrent", impl)
	protocolRequireFile(t, filepath.Join(refDir, name), payload)
	protocolRequireFile(t, filepath.Join(implDir, name), payload)
}

func bittorrentDownloadArgs(dir, torrentPath string) []string {
	args := protocolBaseArgs(dir)
	args = append(args,
		"--enable-peer-exchange=false",
		"--bt-max-peers=1",
		"--seed-time=0",
		torrentPath,
	)
	return args
}
