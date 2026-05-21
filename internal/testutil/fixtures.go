package testutil

import (
	"crypto/sha1"
	"encoding/hex"
)

// MinimalTorrentBytes returns a minimal valid .torrent file as bytes.
// Single file "test.txt" of 1024 bytes, piece length 262144 (256 KiB),
// one piece with SHA-1 hash set to zeros.
func MinimalTorrentBytes() []byte {
	// This is a bencoded torrent: d{announce, info{d{files, name, piece length, pieces}}}
	// d8:announce12:http://x.com/y4:infod6:lengthi1024e4:name8:test.txt12:piece lengthi262144e6:pieces20:xxxxxxxxxxxxxxxxxxxxee
	return minimalTorrent
}

var minimalTorrent = buildMinimalTorrent()

func buildMinimalTorrent() []byte {
	announce := "http://tracker.example.com/announce"
	name := "test.txt"
	length := int64(1024)
	pieceLength := int64(262144)
	var pieceHash [20]byte

	var buf []byte
	buf = append(buf, 'd')

	// announce
	buf = append(buf, bencodeString("announce")...)
	buf = append(buf, bencodeString(announce)...)

	// info
	buf = append(buf, '4', ':', 'i', 'n', 'f', 'o', 'd')

	// file length
	buf = append(buf, bencodeString("length")...)
	buf = append(buf, bencodeInt(length)...)

	// name
	buf = append(buf, bencodeString("name")...)
	buf = append(buf, bencodeString(name)...)

	// piece length
	buf = append(buf, bencodeString("piece length")...)
	buf = append(buf, bencodeInt(pieceLength)...)

	// pieces
	buf = append(buf, bencodeString("pieces")...)
	buf = append(buf, bencodeBytes(pieceHash[:])...)

	// close info dict
	buf = append(buf, 'e')

	// close outer dict
	buf = append(buf, 'e')
	return buf
}

// MinimalMultiFileTorrentBytes returns a minimal multi-file .torrent.
// Two files: "a.txt" (512 bytes) and "b.txt" (512 bytes).
func MinimalMultiFileTorrentBytes() []byte {
	return minimalMultiTorrent
}

var minimalMultiTorrent = buildMinimalMultiTorrent()

func buildMinimalMultiTorrent() []byte {
	announce := "http://tracker.example.com/announce"
	name := "testdir"
	pieceLength := int64(262144)
	var pieceHash [20]byte

	var buf []byte
	buf = append(buf, 'd')

	buf = append(buf, bencodeString("announce")...)
	buf = append(buf, bencodeString(announce)...)

	buf = append(buf, '4', ':', 'i', 'n', 'f', 'o', 'd')

	buf = append(buf, bencodeString("name")...)
	buf = append(buf, bencodeString(name)...)
	buf = append(buf, bencodeString("piece length")...)
	buf = append(buf, bencodeInt(pieceLength)...)
	buf = append(buf, bencodeString("pieces")...)
	buf = append(buf, bencodeBytes(pieceHash[:])...)

	// files list
	buf = append(buf, bencodeString("files")...)
	buf = append(buf, 'l')

	// file 0
	buf = append(buf, 'd')
	buf = append(buf, bencodeString("length")...)
	buf = append(buf, bencodeInt(512)...)
	buf = append(buf, bencodeString("path")...)
	buf = append(buf, 'l')
	buf = append(buf, bencodeString("a.txt")...)
	buf = append(buf, 'e')
	buf = append(buf, 'e')

	// file 1
	buf = append(buf, 'd')
	buf = append(buf, bencodeString("length")...)
	buf = append(buf, bencodeInt(512)...)
	buf = append(buf, bencodeString("path")...)
	buf = append(buf, 'l')
	buf = append(buf, bencodeString("b.txt")...)
	buf = append(buf, 'e')
	buf = append(buf, 'e')

	buf = append(buf, 'e')

	buf = append(buf, 'e')
	buf = append(buf, 'e')
	return buf
}

// bencodeString encodes s as a bencoded string (length:value).
func bencodeString(s string) []byte {
	var buf []byte
	buf = append(buf, itoaBytes(len(s))...)
	buf = append(buf, ':')
	buf = append(buf, s...)
	return buf
}

// bencodeBytes encodes b as a bencoded byte string.
func bencodeBytes(b []byte) []byte {
	var buf []byte
	buf = append(buf, itoaBytes(len(b))...)
	buf = append(buf, ':')
	buf = append(buf, b...)
	return buf
}

// bencodeInt encodes i as a bencoded integer (ie).
func bencodeInt(i int64) []byte {
	var buf []byte
	buf = append(buf, 'i')
	buf = append(buf, itoaBytes(int(i))...)
	buf = append(buf, 'e')
	return buf
}

// itoaBytes converts an int to ASCII bytes (no strconv import needed).
func itoaBytes(n int) []byte {
	if n == 0 {
		return []byte{'0'}
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return buf[i:]
}

// ComputeInfoHash computes the SHA-1 info hash for a bencoded info dict.
// The input infob must be the raw bencoded info dict bytes.
func ComputeInfoHash(infob []byte) [20]byte {
	return sha1.Sum(infob)
}

// TorrentInfoHash returns the info hash for MinimalTorrentBytes.
func TorrentInfoHash() [20]byte {
	return ComputeInfoHash(extractInfoDict(minimalTorrent))
}

// TorrentInfoHashHex returns the hex string of TorrentInfoHash.
func TorrentInfoHashHex() string {
	h := TorrentInfoHash()
	return hex.EncodeToString(h[:])
}

// MultiTorrentInfoHash returns the info hash for MinimalMultiFileTorrentBytes.
func MultiTorrentInfoHash() [20]byte {
	return ComputeInfoHash(extractInfoDict(minimalMultiTorrent))
}

// MultiTorrentInfoHashHex returns the hex string of MultiTorrentInfoHash.
func MultiTorrentInfoHashHex() string {
	h := MultiTorrentInfoHash()
	return hex.EncodeToString(h[:])
}

// extractInfoDict extracts the bencoded info dict from a torrent.
// Assumes the torrent starts with 'd' and has well-formed bencoding.
func extractInfoDict(raw []byte) []byte {
	if len(raw) < 2 || raw[0] != 'd' {
		return nil
	}
	for i := 0; i < len(raw); i++ {
		// look for "4:info" key
		if i+6 <= len(raw) && string(raw[i:i+6]) == "4:info" {
			start := i + 6
			dictDepth := 0
			for j := start; j < len(raw); j++ {
				switch raw[j] {
				case 'd':
					dictDepth++
				case 'e':
					dictDepth--
					if dictDepth == 0 {
						return raw[start : j+1]
					}
				}
			}
		}
	}
	return nil
}

// MagnetURI returns a magnet URI string for MinimalTorrentBytes.
func MagnetURI() string {
	return "magnet:?xt=urn:btih:" + TorrentInfoHashHex() + "&dn=test.txt&tr=http%3A%2F%2Ftracker.example.com%2Fannounce"
}

// SessionEntryLines returns a minimal session file entry as lines.
func SessionEntryLines(gidHex string) []string {
	if gidHex == "" {
		gidHex = "0000000000000001"
	}
	return []string{
		gidHex + "\thttp://example.com/file.zip",
		"\tgid=" + gidHex,
		"\tdir=.",
		"\tmax-connection-per-server=1",
		"\tsplit=5",
	}
}

// CookieLines returns minimal Netscape-format cookie lines for testing.
func CookieLines() []string {
	return []string{
		"# Netscape HTTP Cookie File",
		".example.com\tTRUE\t/\tFALSE\t2147483647\tname\tvalue",
		"example.com\tFALSE\t/path\tTRUE\t0\tsession\tsessionval",
	}
}

// ValidPeerID returns a valid 20-byte aria2-style peer ID.
func ValidPeerID() [20]byte {
	var id [20]byte
	copy(id[:], "-AR2GO-000000000000")
	return id
}

// ValidInfoHash returns a 20-byte info hash (all zeros) for general testing.
func ValidInfoHash() [20]byte {
	return [20]byte{}
}
