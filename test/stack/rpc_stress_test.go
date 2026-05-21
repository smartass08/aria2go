package stack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"
)

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func findFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func startRPCServer(t *testing.T, bin string, port int) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "--enable-rpc", "--rpc-listen-port="+strconv.Itoa(port), "--quiet=true")
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start rpc server failed: %v", err)
	}

	// Wait until port is open
	url := fmt.Sprintf("http://127.0.0.1:%d/jsonrpc", port)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	ready := false
	for i := 0; i < 50; i++ {
		reqBytes, _ := json.Marshal(rpcRequest{
			JSONRPC: "2.0",
			ID:      "1",
			Method:  "system.listMethods",
			Params:  []any{},
		})
		resp, err := client.Post(url, "application/json", bytes.NewReader(reqBytes))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !ready {
		cancel()
		t.Fatalf("rpc server failed to bind and become ready on port %d", port)
	}

	return cmd, cancel
}

func TestRPC_Concurrent_Stress(t *testing.T) {
	bin := findAria2goBinary(t)

	port := findFreePort(t)
	cmd, cancel := startRPCServer(t, bin, port)
	defer func() {
		cancel()
		cmd.Wait()
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/jsonrpc", port)
	httpClient := &http.Client{Timeout: 5 * time.Second}

	concurrencies := []int{1, 5, 10, 20}
	requestsPerClient := 30

	for _, concat := range concurrencies {
		t.Run(fmt.Sprintf("Concurrency_%d", concat), func(t *testing.T) {
			var wg sync.WaitGroup
			errs := make(chan error, concat*requestsPerClient)

			startTime := time.Now()
			for c := 0; c < concat; c++ {
				wg.Add(1)
				go func(clientIdx int) {
					defer wg.Done()
					for reqIdx := 0; reqIdx < requestsPerClient; reqIdx++ {
						reqID := fmt.Sprintf("c%d-r%d", clientIdx, reqIdx)
						req := rpcRequest{
							JSONRPC: "2.0",
							ID:      reqID,
							Method:  "system.listMethods",
							Params:  []any{},
						}
						reqBytes, _ := json.Marshal(req)

						resp, err := httpClient.Post(url, "application/json", bytes.NewReader(reqBytes))
						if err != nil {
							errs <- fmt.Errorf("client %d req %d: post failed: %v", clientIdx, reqIdx, err)
							continue
						}

						var rr rpcResponse
						decErr := json.NewDecoder(resp.Body).Decode(&rr)
						resp.Body.Close()

						if decErr != nil {
							errs <- fmt.Errorf("client %d req %d: decode failed: %v", clientIdx, reqIdx, decErr)
							continue
						}

						if rr.Error != nil {
							errs <- fmt.Errorf("client %d req %d: RPC error response: %s", clientIdx, reqIdx, rr.Error.Message)
							continue
						}

						if rr.ID != reqID {
							errs <- fmt.Errorf("client %d req %d: ID mismatch: got %q, want %q", clientIdx, reqIdx, rr.ID, reqID)
							continue
						}
					}
				}(c)
			}
			wg.Wait()
			close(errs)

			elapsed := time.Since(startTime)
			totalRequests := concat * requestsPerClient
			t.Logf("Processed %d total RPC requests across %d concurrent clients in %v (%.2f reqs/sec)",
				totalRequests, concat, elapsed, float64(totalRequests)/elapsed.Seconds())

			for err := range errs {
				t.Errorf("Error occurred: %v", err)
			}
		})
	}
}
