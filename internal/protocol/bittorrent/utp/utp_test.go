package utp

import (
	"bytes"
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

func TestPacketHeaderEncodeDecode(t *testing.T) {
	pkt := packet{
		typ:          stData,
		ver:          1,
		connID:       0x1234,
		timestamp:    0x56789ABC,
		tsDiff:       0xDEF01234,
		wndSize:      0x10000,
		seqNr:        42,
		ackNr:        10,
		ext:          0,
		payload:      []byte("hello"),
		selectiveAck: nil,
	}

	buf := make([]byte, headerSize+len(pkt.payload))
	pkt.encode(buf)

	if len(buf) != headerSize+len(pkt.payload) {
		t.Fatalf("encoded length %d, want %d", len(buf), headerSize+len(pkt.payload))
	}

	decoded, _, err := decodePacket(buf)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if decoded.typ != stData {
		t.Errorf("type = %d, want %d", decoded.typ, stData)
	}
	if decoded.ver != 1 {
		t.Errorf("version = %d, want 1", decoded.ver)
	}
	if decoded.connID != 0x1234 {
		t.Errorf("connID = %x, want 1234", decoded.connID)
	}
	if decoded.timestamp != 0x56789ABC {
		t.Errorf("timestamp = %x, want 56789ABC", decoded.timestamp)
	}
	if decoded.tsDiff != 0xDEF01234 {
		t.Errorf("tsDiff = %x, want DEF01234", decoded.tsDiff)
	}
	if decoded.wndSize != 0x10000 {
		t.Errorf("wndSize = %x, want 10000", decoded.wndSize)
	}
	if decoded.seqNr != 42 {
		t.Errorf("seqNr = %d, want 42", decoded.seqNr)
	}
	if decoded.ackNr != 10 {
		t.Errorf("ackNr = %d, want 10", decoded.ackNr)
	}
	if !bytes.Equal(decoded.payload, []byte("hello")) {
		t.Errorf("payload = %q, want %q", decoded.payload, "hello")
	}
}

func TestPacketTypeConstants(t *testing.T) {
	if stData != 0 {
		t.Errorf("stData = %d, want 0", stData)
	}
	if stFin != 1 {
		t.Errorf("stFin = %d, want 1", stFin)
	}
	if stState != 2 {
		t.Errorf("stState = %d, want 2", stState)
	}
	if stReset != 3 {
		t.Errorf("stReset = %d, want 3", stReset)
	}
	if stSyn != 4 {
		t.Errorf("stSyn = %d, want 4", stSyn)
	}
}

func TestSynPacketConnectionID(t *testing.T) {
	pkt := packet{
		typ:    stSyn,
		ver:    1,
		connID: 0xABCD,
		seqNr:  1,
	}

	buf := make([]byte, headerSize)
	pkt.encode(buf)

	decoded, _, err := decodePacket(buf)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if decoded.typ != stSyn {
		t.Errorf("type = %d, want %d", decoded.typ, stSyn)
	}
	if decoded.connID != 0xABCD {
		t.Errorf("connID = %x, want ABCD", decoded.connID)
	}
	if decoded.seqNr != 1 {
		t.Errorf("seqNr = %d, want 1", decoded.seqNr)
	}
}

func TestFinPacket(t *testing.T) {
	pkt := packet{
		typ:    stFin,
		ver:    1,
		connID: 0x2000,
		seqNr:  5,
		ackNr:  3,
	}

	buf := make([]byte, headerSize)
	pkt.encode(buf)

	decoded, _, err := decodePacket(buf)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if decoded.typ != stFin {
		t.Errorf("type = %d, want %d", decoded.typ, stFin)
	}
}

func TestResetPacket(t *testing.T) {
	pkt := packet{
		typ:    stReset,
		ver:    1,
		connID: 0x3000,
		seqNr:  99,
		ackNr:  98,
	}

	buf := make([]byte, headerSize)
	pkt.encode(buf)

	decoded, _, err := decodePacket(buf)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if decoded.typ != stReset {
		t.Errorf("type = %d, want %d", decoded.typ, stReset)
	}
}

func TestSocketDialAccept(t *testing.T) {
	addr := "127.0.0.1:0"
	sock, err := NewSocket(addr)
	if err != nil {
		t.Fatalf("NewSocket: %v", err)
	}
	defer sock.Close()

	sockAddr := sock.LocalAddr().(*net.UDPAddr)

	var serverConn *Conn
	var serverErr error
	var serverWg sync.WaitGroup

	serverWg.Add(1)
	go func() {
		defer serverWg.Done()
		ctx := context.Background()
		serverConn, serverErr = sock.Accept(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	ctx := context.Background()
	clientConn, err := sock.Dial(ctx, sockAddr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer clientConn.Close()

	serverWg.Wait()
	if serverErr != nil {
		t.Fatalf("Accept: %v", serverErr)
	}
	defer serverConn.Close()

	if clientConn.LocalAddr() == nil {
		t.Error("client LocalAddr is nil")
	}
	if clientConn.RemoteAddr() == nil {
		t.Error("client RemoteAddr is nil")
	}
	if serverConn.LocalAddr() == nil {
		t.Error("server LocalAddr is nil")
	}
	if serverConn.RemoteAddr() == nil {
		t.Error("server RemoteAddr is nil")
	}
}

func TestDataExchange(t *testing.T) {
	sock, err := NewSocket("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSocket: %v", err)
	}
	defer sock.Close()

	sockAddr := sock.LocalAddr().(*net.UDPAddr)

	var serverConn *Conn
	var serverErr error
	var serverWg sync.WaitGroup

	serverWg.Add(1)
	go func() {
		defer serverWg.Done()
		ctx := context.Background()
		serverConn, serverErr = sock.Accept(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	ctx := context.Background()
	clientConn, err := sock.Dial(ctx, sockAddr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer clientConn.Close()

	serverWg.Wait()
	if serverErr != nil {
		t.Fatalf("Accept: %v", serverErr)
	}
	defer serverConn.Close()

	message := []byte("hello uTP!")
	n, err := clientConn.Write(message)
	if err != nil {
		t.Fatalf("client Write: %v", err)
	}
	if n != len(message) {
		t.Errorf("client wrote %d bytes, want %d", n, len(message))
	}

	buf := make([]byte, 1024)
	n, err = serverConn.Read(buf)
	if err != nil {
		t.Fatalf("server Read: %v", err)
	}
	if n != len(message) {
		t.Errorf("server read %d bytes, want %d", n, len(message))
	}
	if string(buf[:n]) != string(message) {
		t.Errorf("server read %q, want %q", buf[:n], message)
	}

	response := []byte("roger that!")
	n, err = serverConn.Write(response)
	if err != nil {
		t.Fatalf("server Write: %v", err)
	}

	n, err = clientConn.Read(buf)
	if err != nil {
		t.Fatalf("client Read: %v", err)
	}
	if string(buf[:n]) != string(response) {
		t.Errorf("client read %q, want %q", buf[:n], response)
	}
}

func TestConnClose(t *testing.T) {
	sock, err := NewSocket("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSocket: %v", err)
	}
	defer sock.Close()

	sockAddr := sock.LocalAddr().(*net.UDPAddr)

	var serverConn *Conn
	var serverErr error
	var serverWg sync.WaitGroup

	serverWg.Add(1)
	go func() {
		defer serverWg.Done()
		ctx := context.Background()
		serverConn, serverErr = sock.Accept(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	ctx := context.Background()
	clientConn, err := sock.Dial(ctx, sockAddr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	serverWg.Wait()
	if serverErr != nil {
		t.Fatalf("Accept: %v", serverErr)
	}

	if err := clientConn.Close(); err != nil {
		t.Errorf("client Close: %v", err)
	}
	if err := serverConn.Close(); err != nil {
		t.Errorf("server Close: %v", err)
	}

	_, err = clientConn.Read(make([]byte, 1))
	if err == nil {
		t.Error("expected error reading from closed conn")
	}

	_, err = serverConn.Write([]byte("data"))
	if err == nil {
		t.Error("expected error writing to closed conn")
	}
}

func TestContextCancellation(t *testing.T) {
	sock, err := NewSocket("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSocket: %v", err)
	}
	defer sock.Close()

	sockAddr := sock.LocalAddr().(*net.UDPAddr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = sock.Dial(ctx, sockAddr)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestSelectiveAck(t *testing.T) {
	// Verify selective ack extension can be encoded and decoded
	ackMask := &selectiveAckMask{
		bitmask: []byte{0b10101010, 0b01010101, 0b11110000, 0b00001111},
	}

	buf := make([]byte, 1024)
	pkt := packet{
		typ:          stData,
		ver:          1,
		connID:       1,
		seqNr:        5,
		ackNr:        2,
		selectiveAck: ackMask,
		payload:      []byte("data"),
	}
	n := pkt.encode(buf)

	decoded, _, err := decodePacket(buf[:n])
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if decoded.selectiveAck == nil {
		t.Fatal("selective ack not decoded")
	}
	if len(decoded.selectiveAck.bitmask) != 4 {
		t.Errorf("bitmask length %d, want 4", len(decoded.selectiveAck.bitmask))
	}
	if decoded.selectiveAck.bitmask[0] != 0b10101010 {
		t.Errorf("bitmask[0] = %08b, want 10101010", decoded.selectiveAck.bitmask[0])
	}
}

func TestLEDBATWindowDecreaseOnHighDelay(t *testing.T) {
	cc := newLEDBAT()

	cc.baseDelay = 10 * time.Millisecond
	cc.currentDelay = 200 * time.Millisecond
	cc.maxWindow = 50000

	initialMax := cc.maxWindow
	cc.updateWindow(50000, cc.maxWindow)
	cc.applyCC(time.Now())

	if cc.maxWindow >= initialMax {
		t.Errorf("window did not decrease: got %d, was %d (delay %v > target 100ms)",
			cc.maxWindow, initialMax, cc.currentDelay)
	}
}

func TestLEDBATWindowIncreaseOnLowDelay(t *testing.T) {
	cc := newLEDBAT()

	cc.baseDelay = 10 * time.Millisecond
	cc.currentDelay = 20 * time.Millisecond
	cc.maxWindow = 1000

	initialMax := cc.maxWindow
	cc.updateWindow(1000, cc.maxWindow)
	cc.applyCC(time.Now())

	if cc.maxWindow <= initialMax {
		t.Errorf("window did not increase: got %d, was %d (delay %v < target 100ms)",
			cc.maxWindow, initialMax, cc.currentDelay)
	}
}

func TestLEDBATMinWindow(t *testing.T) {
	cc := newLEDBAT()
	cc.maxWindow = 200

	cc.currentDelay = 500 * time.Millisecond
	cc.baseDelay = 10 * time.Millisecond

	cc.updateWindow(200, cc.maxWindow)
	cc.applyCC(time.Now())

	minWin := uint32(minPacketSize * 2)
	if cc.maxWindow < minWin {
		t.Errorf("window %d below minimum %d", cc.maxWindow, minWin)
	}
}

func TestSequenceNumberWraparound(t *testing.T) {
	// uTP uses 16-bit sequence numbers. Test that wraparound is handled.
	if seqDiff(65535, 0) != 65535 {
		t.Errorf("seqDiff(65535, 0) = %d, want 65535", seqDiff(65535, 0))
	}
	if seqDiff(0, 65535) != 1 {
		t.Errorf("seqDiff(0, 65535) = %d, want 1", seqDiff(0, 65535))
	}
	if seqDiff(1, 65535) != 2 {
		t.Errorf("seqDiff(1, 65535) = %d, want 2 (wraps around)", seqDiff(1, 65535))
	}
	if seqDiff(10, 5) != 5 {
		t.Errorf("seqDiff(10, 5) = %d, want 5", seqDiff(10, 5))
	}
	if seqDiff(5, 10) != 65531 {
		t.Errorf("seqDiff(5, 10) = %d, want 65531", seqDiff(5, 10))
	}
}

func TestLedbatBaseDelayUpdate(t *testing.T) {
	cc := newLEDBAT()

	cc.updateDelay(50 * time.Millisecond)
	cc.updateDelay(30 * time.Millisecond)
	cc.updateDelay(100 * time.Millisecond)
	cc.updateDelay(20 * time.Millisecond)
	cc.updateDelay(25 * time.Millisecond)

	if cc.baseDelay != 20*time.Millisecond {
		t.Errorf("baseDelay = %v, want 20ms (minimum of observed delays)", cc.baseDelay)
	}
}

func TestLedbatRTTUpdate(t *testing.T) {
	cc := newLEDBAT()

	cc.updateRTT(100 * time.Millisecond)
	if cc.rtt != 100*time.Millisecond {
		t.Errorf("rtt = %v, want 100ms after first sample", cc.rtt)
	}

	cc.updateRTT(200 * time.Millisecond)
	// rtt += (packet_rtt - rtt) / 8
	// rtt = 100 + (200 - 100) / 8 = 100 + 12.5 = 112.5ms
	expectedRTT := 100*time.Millisecond + (200*time.Millisecond-100*time.Millisecond)/8
	if cc.rtt != expectedRTT {
		t.Errorf("rtt = %v, want %v", cc.rtt, expectedRTT)
	}
}

func TestTimeoutCalculation(t *testing.T) {
	cc := newLEDBAT()
	cc.rtt = 100 * time.Millisecond
	cc.rttVar = 50 * time.Millisecond

	timeout := cc.timeout()
	expected := maxDuration(cc.rtt+cc.rttVar*4, 500*time.Millisecond)
	if timeout != expected {
		t.Errorf("timeout = %v, want %v", timeout, expected)
	}
}

func TestMinTimeout(t *testing.T) {
	cc := newLEDBAT()
	cc.rtt = 10 * time.Millisecond
	cc.rttVar = 1 * time.Millisecond

	timeout := cc.timeout()
	if timeout < 500*time.Millisecond {
		t.Errorf("timeout = %v, want at least 500ms", timeout)
	}
}

func TestNonDataPacketSequence(t *testing.T) {
	// ST_STATE, ST_FIN, ST_RESET do not increment seq_nr according to BEP 29
	// Actually, BEP says STATE packets without payload don't increase seq_nr.
	// FIN and RESET do consume a sequence number.
	pkt := packet{
		typ:    stState,
		ver:    1,
		connID: 1,
		seqNr:  5,
		ackNr:  3,
	}
	if pkt.hasPayload() {
		t.Error("ST_STATE should not have payload")
	}

	pkt2 := packet{
		typ:    stData,
		ver:    1,
		connID: 1,
		seqNr:  6,
		ackNr:  4,
	}
	if !pkt2.hasPayload() {
		t.Error("ST_DATA should have payload")
	}
}

func TestConnReadDeadline(t *testing.T) {
	sock, err := NewSocket("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSocket: %v", err)
	}
	defer sock.Close()

	sockAddr := sock.LocalAddr().(*net.UDPAddr)

	var serverConn *Conn
	var serverErr error
	var serverWg sync.WaitGroup

	serverWg.Add(1)
	go func() {
		defer serverWg.Done()
		ctx := context.Background()
		serverConn, serverErr = sock.Accept(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	ctx := context.Background()
	clientConn, err := sock.Dial(ctx, sockAddr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer clientConn.Close()

	serverWg.Wait()
	if serverErr != nil {
		t.Fatalf("Accept: %v", serverErr)
	}
	defer serverConn.Close()

	clientConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	buf := make([]byte, 1024)
	_, err = clientConn.Read(buf)
	if err == nil {
		t.Error("expected timeout error")
	}
	if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Errorf("expected net.Error with Timeout()==true, got %v", err)
	}
}

func TestLargeDataTransfer(t *testing.T) {
	sock, err := NewSocket("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSocket: %v", err)
	}
	defer sock.Close()

	sockAddr := sock.LocalAddr().(*net.UDPAddr)

	var serverConn *Conn
	var serverErr error
	var serverWg sync.WaitGroup

	serverWg.Add(1)
	go func() {
		defer serverWg.Done()
		ctx := context.Background()
		serverConn, serverErr = sock.Accept(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	ctx := context.Background()
	clientConn, err := sock.Dial(ctx, sockAddr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer clientConn.Close()

	serverWg.Wait()
	if serverErr != nil {
		t.Fatalf("Accept: %v", serverErr)
	}
	defer serverConn.Close()

	payload := bytes.Repeat([]byte("X"), 65536)

	var wg sync.WaitGroup
	wg.Add(2)

	var clientErr, serverErr2 error
	var received []byte

	go func() {
		defer wg.Done()
		_, clientErr = clientConn.Write(payload)
	}()

	go func() {
		defer wg.Done()
		buf := make([]byte, 70000)
		n, e := serverConn.Read(buf)
		if e != nil {
			serverErr2 = e
			return
		}
		received = buf[:n]
	}()

	wg.Wait()

	if clientErr != nil {
		t.Fatalf("client Write: %v", clientErr)
	}
	if serverErr2 != nil {
		t.Fatalf("server Read: %v", serverErr2)
	}
	if len(received) != len(payload) {
		t.Fatalf("received %d bytes, want %d", len(received), len(payload))
	}
	if !bytes.Equal(received, payload) {
		t.Error("received data mismatch")
	}
}

func TestSocketClose(t *testing.T) {
	sock, err := NewSocket("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSocket: %v", err)
	}

	if err := sock.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}

	if err := sock.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}

	ctx := context.Background()
	_, err = sock.Accept(ctx)
	if err == nil {
		t.Error("expected error from Accept on closed socket")
	}
}

func TestSocketLocalAddr(t *testing.T) {
	sock, err := NewSocket("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSocket: %v", err)
	}
	defer sock.Close()

	addr := sock.LocalAddr()
	if addr == nil {
		t.Fatal("LocalAddr is nil")
	}
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		t.Fatalf("LocalAddr is %T, want *net.UDPAddr", addr)
	}
	if udpAddr.Port == 0 {
		t.Error("expected non-zero port")
	}
}

func TestConnDualClose(t *testing.T) {
	sock, err := NewSocket("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSocket: %v", err)
	}
	defer sock.Close()

	sockAddr := sock.LocalAddr().(*net.UDPAddr)

	var serverConn *Conn
	var serverErr error
	var serverWg sync.WaitGroup

	serverWg.Add(1)
	go func() {
		defer serverWg.Done()
		ctx := context.Background()
		serverConn, serverErr = sock.Accept(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	ctx := context.Background()
	clientConn, err := sock.Dial(ctx, sockAddr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	serverWg.Wait()
	if serverErr != nil {
		t.Fatalf("Accept: %v", serverErr)
	}

	clientConn.Close()
	clientConn.Close()

	serverConn.Close()
	serverConn.Close()
}

func TestConcurrentWrites(t *testing.T) {
	sock, err := NewSocket("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSocket: %v", err)
	}
	defer sock.Close()

	sockAddr := sock.LocalAddr().(*net.UDPAddr)

	var serverConn *Conn
	var serverErr error
	var serverWg sync.WaitGroup

	serverWg.Add(1)
	go func() {
		defer serverWg.Done()
		ctx := context.Background()
		serverConn, serverErr = sock.Accept(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	ctx := context.Background()
	clientConn, err := sock.Dial(ctx, sockAddr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer clientConn.Close()

	serverWg.Wait()
	if serverErr != nil {
		t.Fatalf("Accept: %v", serverErr)
	}
	defer serverConn.Close()

	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			msg := []byte{byte(id)}
			_, err := clientConn.Write(msg)
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("concurrent write error: %v", err)
		}
	}
}

func TestSelectiveAckBitmaskBitOrder(t *testing.T) {
	// BEP 29: first byte represents packets [ack_nr+2, ack_nr+2+7] in reverse order.
	// LSB of first byte represents ack_nr+2.
	ack := &selectiveAckMask{bitmask: []byte{0x01, 0x00, 0x00, 0x00}}

	if !ack.isAcked(2) {
		t.Error("ack_nr+2 should be acked when LSB is set")
	}
	if ack.isAcked(3) {
		t.Error("ack_nr+3 should not be acked")
	}

	ack = &selectiveAckMask{bitmask: []byte{0x80, 0x00, 0x00, 0x00}}
	if !ack.isAcked(9) {
		t.Error("ack_nr+9 should be acked when MSB of first byte is set")
	}
}

func BenchmarkPacketEncode(b *testing.B) {
	pkt := packet{
		typ:     stData,
		ver:     1,
		payload: bytes.Repeat([]byte("x"), 1400),
	}
	buf := make([]byte, headerSize+1500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pkt.encode(buf)
	}
}

func BenchmarkPacketDecode(b *testing.B) {
	pkt := packet{
		typ:     stData,
		ver:     1,
		payload: bytes.Repeat([]byte("x"), 1400),
	}
	buf := make([]byte, headerSize+1500)
	pkt.encode(buf)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decodePacket(buf)
	}
}
