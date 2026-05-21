package stack

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/smartass08/aria2go/internal/bencode"
)

// bencodeMarshal is a local wrapper to marshal a dictionary to bytes.
func buildDynamicTorrent(announce string, filename string, fileData []byte, pieceLen int) ([]byte, []byte) {
	// Calculate piece hashes
	var pieces []byte
	for i := 0; i < len(fileData); i += pieceLen {
		end := i + pieceLen
		if end > len(fileData) {
			end = len(fileData)
		}
		h := sha1.Sum(fileData[i:end])
		pieces = append(pieces, h[:]...)
	}

	info := bencode.NewDict()
	info.Set("name", bencode.NewString(filename))
	info.Set("piece length", bencode.IntVal{I: int64(pieceLen)})
	info.Set("pieces", bencode.NewString(string(pieces)))
	info.Set("length", bencode.IntVal{I: int64(len(fileData))})

	d := bencode.NewDict()
	d.Set("announce", bencode.NewString(announce))
	d.Set("info", info)

	data, err := bencode.Marshal(d)
	if err != nil {
		panic(err)
	}

	// Calculate info-hash
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		panic(err)
	}
	infoHash := sha1.Sum(infoBytes)

	return data, infoHash[:]
}

func handlePeerConnection(conn net.Conn, infoHash []byte, fileData []byte, pieceLen int) {
	defer conn.Close()

	// 1. Read handshake
	var handshake [68]byte
	if _, err := io.ReadFull(conn, handshake[:]); err != nil {
		return
	}

	// Verify info_hash
	if !bytes.Equal(handshake[28:48], infoHash) {
		return
	}

	// 2. Send handshake response
	var response [68]byte
	response[0] = 19
	copy(response[1:20], "BitTorrent protocol")
	copy(response[28:48], infoHash)
	copy(response[48:68], "-GO0001-mockpeerstub")
	conn.Write(response[:])

	// 3. Send bitfield showing we have all pieces
	numPieces := (len(fileData) + pieceLen - 1) / pieceLen
	bitfieldLen := (numPieces + 7) / 8
	bitfield := make([]byte, bitfieldLen)
	for i := 0; i < numPieces; i++ {
		bitfield[i/8] |= 1 << (7 - (i % 8))
	}

	// Message: len(4 bytes) + id(1 byte) + payload
	msg := make([]byte, 4+1+bitfieldLen)
	binaryPutUint32(msg[:4], uint32(1+bitfieldLen))
	msg[4] = 5 // bitfield ID
	copy(msg[5:], bitfield)
	conn.Write(msg)

	// 4. Send unchoke
	unchoke := []byte{0, 0, 0, 1, 1}
	conn.Write(unchoke)

	// 5. Handle incoming requests
	var lenBuf [4]byte
	for {
		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			return
		}
		msgLen := binaryGetUint32(lenBuf[:])
		if msgLen == 0 {
			continue // keep-alive
		}

		payload := make([]byte, msgLen)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return
		}

		id := payload[0]
		switch id {
		case 2: // interested
			// Send unchoke just in case
			conn.Write(unchoke)
		case 6: // request
			if len(payload) < 13 {
				return
			}
			index := binaryGetUint32(payload[1:5])
			begin := binaryGetUint32(payload[5:9])
			length := binaryGetUint32(payload[9:13])

			offset := int64(index)*int64(pieceLen) + int64(begin)
			if offset+int64(length) <= int64(len(fileData)) {
				// Send piece
				pieceMsg := make([]byte, 4+1+8+length)
				binaryPutUint32(pieceMsg[:4], uint32(9+length))
				pieceMsg[4] = 7 // piece ID
				binaryPutUint32(pieceMsg[5:9], index)
				binaryPutUint32(pieceMsg[9:13], begin)
				copy(pieceMsg[13:], fileData[offset:offset+int64(length)])
				conn.Write(pieceMsg)
			}
		}
	}
}

func binaryPutUint32(buf []byte, val uint32) {
	buf[0] = byte(val >> 24)
	buf[1] = byte(val >> 16)
	buf[2] = byte(val >> 8)
	buf[3] = byte(val)
}

func binaryGetUint32(buf []byte) uint32 {
	return uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])
}

func startMockPeer(t *testing.T, infoHash []byte, fileData []byte, pieceLen int) (int, context.CancelFunc) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("peer listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}
			go handlePeerConnection(conn, infoHash, fileData, pieceLen)
		}
	}()

	return ln.Addr().(*net.TCPAddr).Port, func() {
		cancel()
		ln.Close()
	}
}

func startMockTracker(t *testing.T, peerPort *int) (int, context.CancelFunc) {
	mux := http.NewServeMux()
	mux.HandleFunc("/announce", func(w http.ResponseWriter, r *http.Request) {
		// Respond with compact peer format (6 bytes: 4 bytes IP, 2 bytes Port)
		peerIP := net.ParseIP("127.0.0.1").To4()
		var compactPeer [6]byte
		copy(compactPeer[0:4], peerIP)
		compactPeer[4] = byte(*peerPort >> 8)
		compactPeer[5] = byte(*peerPort)

		resp := bencode.NewDict()
		resp.Set("interval", bencode.IntVal{I: 1800})
		resp.Set("peers", bencode.NewString(string(compactPeer[:])))

		data, err := bencode.Marshal(resp)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		w.Write(data)
	})

	srv := &http.Server{
		Handler: mux,
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("tracker listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	go srv.Serve(ln)

	return port, func() {
		srv.Shutdown(context.Background())
	}
}

func TestBitTorrent_Download_Stress(t *testing.T) {
	bin := findAria2goBinary(t)

	// Payload data (128 KB)
	testPayload := make([]byte, 1024*128)
	for i := range testPayload {
		testPayload[i] = byte((i*59 + 17) % 256)
	}
	pieceLen := 16384 // 16 KB pieces

	// Start Mock Peer
	dummyHash := make([]byte, 20)
	peerPort, cancelPeer := startMockPeer(t, dummyHash, testPayload, pieceLen)
	defer cancelPeer()

	// Start Mock Tracker
	trackerPort, cancelTracker := startMockTracker(t, &peerPort)
	defer cancelTracker()

	// Generate real Torrent File targeting our local Tracker
	announceURL := fmt.Sprintf("http://127.0.0.1:%d/announce", trackerPort)
	torrentData, infoHash := buildDynamicTorrent(announceURL, "btfile.bin", testPayload, pieceLen)

	// Restart peer with actual info-hash
	cancelPeer()
	peerPort, cancelPeer = startMockPeer(t, infoHash, testPayload, pieceLen)

	concurrencies := []int{1, 5, 10}
	for _, concat := range concurrencies {
		t.Run(fmt.Sprintf("Concurrency_%d", concat), func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "aria2go-bt-stress-*")
			if err != nil {
				t.Fatalf("tempdir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			// Write torrent file
			torrentPath := filepath.Join(tempDir, "test.torrent")
			if err := os.WriteFile(torrentPath, torrentData, 0644); err != nil {
				t.Fatalf("write torrent: %v", err)
			}

			var wg sync.WaitGroup
			errs := make(chan error, concat)

			startTime := time.Now()
			for i := 0; i < concat; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					outFile := filepath.Join(tempDir, fmt.Sprintf("bt_%d", idx), "btfile.bin")
					cmd := exec.Command(bin,
						torrentPath,
						"-d", filepath.Join(tempDir, fmt.Sprintf("bt_%d", idx)),
						"--seed-time=0", // Stop seeding immediately
					)
					var stdout, stderr bytes.Buffer
					cmd.Stdout = &stdout
					cmd.Stderr = &stderr
					if err := cmd.Run(); err != nil {
						errs <- fmt.Errorf("torrent download %d failed: %v (stdout: %s, stderr: %s)", idx, err, stdout.String(), stderr.String())
						return
					}

					// Let's log the output of aria2go for diagnostics
					_ = stdout // diagnostic log placeholder

					// Validate content
					downloaded, err := os.ReadFile(outFile)
					if err != nil {
						errs <- fmt.Errorf("read file %d failed: %v (expected path: %s)", idx, err, outFile)
						return
					}
					if !bytes.Equal(downloaded, testPayload) {
						errs <- fmt.Errorf("mismatch on file %d: downloaded size = %d, expected size = %d. stdout: %s. stderr: %s", idx, len(downloaded), len(testPayload), stdout.String(), stderr.String())
						return
					}
				}(i)
			}
			wg.Wait()
			close(errs)

			elapsed := time.Since(startTime)
			t.Logf("BitTorrent downloaded %d files concurrently in %v", concat, elapsed)

			for err := range errs {
				t.Errorf("Error occurred: %v", err)
			}
		})
	}
}
