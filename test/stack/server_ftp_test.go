package stack

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ftpSession holds the state for one FTP connection.
type ftpSession struct {
	conn       net.Conn
	dataLn     net.Listener
	dataPort   int
	binaryMode bool
	testData   []byte
}

func handleFTPConnection(conn net.Conn, testData []byte) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// Welcome greeting
	writer.WriteString("220 Welcome to Go Mock FTP Server\r\n")
	writer.Flush()

	session := &ftpSession{
		conn:     conn,
		testData: testData,
	}
	defer func() {
		if session.dataLn != nil {
			session.dataLn.Close()
		}
	}()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, " ", 2)
		cmd := strings.ToUpper(parts[0])
		arg := ""
		if len(parts) > 1 {
			arg = parts[1]
		}

		switch cmd {
		case "USER":
			writer.WriteString("331 User name okay, need password.\r\n")
		case "PASS":
			writer.WriteString("230 User logged in, proceed.\r\n")
		case "SYST":
			writer.WriteString("215 UNIX Type: L8\r\n")
		case "PWD":
			writer.WriteString("257 \"/\" is current directory.\r\n")
		case "TYPE":
			if arg == "I" {
				session.binaryMode = true
				writer.WriteString("200 Type set to I.\r\n")
			} else {
				writer.WriteString("200 Type set but unverified.\r\n")
			}
		case "PASV":
			// Start dynamic passive data listener on localhost
			dataLn, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				writer.WriteString("425 Can't open data connection.\r\n")
				break
			}
			if session.dataLn != nil {
				session.dataLn.Close()
			}
			session.dataLn = dataLn
			session.dataPort = dataLn.Addr().(*net.TCPAddr).Port

			p1 := session.dataPort / 256
			p2 := session.dataPort % 256
			writer.WriteString(fmt.Sprintf("227 Entering Passive Mode (127,0,0,1,%d,%d).\r\n", p1, p2))
		case "SIZE":
			writer.WriteString(fmt.Sprintf("213 %d\r\n", len(session.testData)))
		case "RETR":
			if session.dataLn == nil {
				writer.WriteString("425 Use PASV first.\r\n")
				break
			}
			writer.WriteString("150 File status okay; about to open data connection.\r\n")
			writer.Flush()

			// Accept data connection with timeout
			session.dataLn.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second))
			dataConn, err := session.dataLn.Accept()
			if err != nil {
				writer.WriteString("425 Failed to establish data connection.\r\n")
				break
			}

			// Send file data
			go func(dc net.Conn) {
				defer dc.Close()
				dc.Write(session.testData)
			}(dataConn)

			writer.WriteString("226 Closing data connection. Requested file action successful.\r\n")
			session.dataLn.Close()
			session.dataLn = nil
		case "QUIT":
			writer.WriteString("221 Goodbye.\r\n")
			writer.Flush()
			return
		default:
			writer.WriteString("202 Command not implemented.\r\n")
		}
		writer.Flush()
	}
}

func startMockFTPServer(t *testing.T, testData []byte) (int, context.CancelFunc) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ftp listen: %v", err)
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
			go handleFTPConnection(conn, testData)
		}
	}()

	return ln.Addr().(*net.TCPAddr).Port, func() {
		cancel()
		ln.Close()
	}
}

func TestFTP_Download_Stress(t *testing.T) {
	bin := findAria2goBinary(t)

	testPayload := make([]byte, 1024*1024*2) // 2 MB of random-like data
	for i := range testPayload {
		testPayload[i] = byte((i*7 + 13) % 256)
	}

	port, cancel := startMockFTPServer(t, testPayload)
	defer cancel()

	concurrencies := []int{1, 5, 10}
	for _, concat := range concurrencies {
		t.Run(fmt.Sprintf("Concurrency_%d", concat), func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "aria2go-ftp-stress-*")
			if err != nil {
				t.Fatalf("tempdir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			var wg sync.WaitGroup
			errs := make(chan error, concat)

			startTime := time.Now()
			for i := 0; i < concat; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					outFile := filepath.Join(tempDir, fmt.Sprintf("ftp_file_%d.bin", idx))
					cmd := exec.Command(bin,
						fmt.Sprintf("ftp://127.0.0.1:%d/test_file.bin", port),
						"-d", tempDir,
						"-o", fmt.Sprintf("ftp_file_%d.bin", idx),
						"--quiet=true",
						"--ftp-pasv=true",
					)
					var stderr bytes.Buffer
					cmd.Stderr = &stderr
					if err := cmd.Run(); err != nil {
						errs <- fmt.Errorf("ftp download %d failed: %v (stderr: %s)", idx, err, stderr.String())
						return
					}

					// Validate content
					downloaded, err := os.ReadFile(outFile)
					if err != nil {
						errs <- fmt.Errorf("read file %d failed: %v", idx, err)
						return
					}
					if !bytes.Equal(downloaded, testPayload) {
						errs <- fmt.Errorf("mismatch on file %d", idx)
						return
					}
				}(i)
			}
			wg.Wait()
			close(errs)

			elapsed := time.Since(startTime)
			t.Logf("FTP downloaded %d files concurrently in %v", concat, elapsed)

			for err := range errs {
				t.Errorf("Error occurred: %v", err)
			}
		})
	}
}
