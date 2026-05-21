package config

import (
	"os"
	"strings"
	"testing"
)

func TestParseConfBasicKeyValue(t *testing.T) {
	input := "dir=/tmp/aria2\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Dir != "/tmp/aria2" {
		t.Errorf("Dir = %q, want /tmp/aria2", o.Dir)
	}
}

func TestParseConfWhitespaceStripping(t *testing.T) {
	input := "  dir  =  /tmp/aria2  \n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Dir != "/tmp/aria2" {
		t.Errorf("Dir = %q, want /tmp/aria2", o.Dir)
	}
}

func TestParseConfComments(t *testing.T) {
	input := "# this is a comment\ndir=/tmp/aria2\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Dir != "/tmp/aria2" {
		t.Errorf("Dir = %q, want /tmp/aria2", o.Dir)
	}
}

func TestParseConfNoInlineComments(t *testing.T) {
	input := "dir=/tmp/aria2  # attempt inline comment\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Dir != "/tmp/aria2  # attempt inline comment" {
		t.Errorf("Dir = %q, want value with trailing text", o.Dir)
	}
}

func TestParseConfDuplicateOptionsLastWins(t *testing.T) {
	input := "dir=/tmp/first\ndir=/tmp/second\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Dir != "/tmp/second" {
		t.Errorf("Dir = %q, want /tmp/second", o.Dir)
	}
}

func TestParseConfBooleanTrue(t *testing.T) {
	input := "enable-rpc=true\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !o.EnableRPC {
		t.Errorf("EnableRPC = false, want true")
	}
}

func TestParseConfBooleanFalse(t *testing.T) {
	input := "enable-rpc=false\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.EnableRPC {
		t.Errorf("EnableRPC = true, want false")
	}
}

func TestParseConfInvalidBoolean(t *testing.T) {
	input := "enable-rpc=yes\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err == nil {
		t.Fatalf("expected error for invalid boolean, got nil")
	}
}

func TestParseConfInteger(t *testing.T) {
	input := "max-concurrent-downloads=10\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.MaxConcurrentDownloads != 10 {
		t.Errorf("MaxConcurrentDownloads = %d, want 10", o.MaxConcurrentDownloads)
	}
}

func TestParseConfUnknownOptionSilentlyIgnored(t *testing.T) {
	input := "made-up-option=somevalue\ndir=/tmp/aria2\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Dir != "/tmp/aria2" {
		t.Errorf("Dir = %q, want /tmp/aria2", o.Dir)
	}
}

func TestParseConfCumulativeHeader(t *testing.T) {
	input := "header=X-One: value1\nheader=X-Two: value2\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(o.Header) != 2 {
		t.Fatalf("Header len = %d, want 2", len(o.Header))
	}
	if o.Header[0] != "X-One: value1" {
		t.Errorf("Header[0] = %q, want X-One: value1", o.Header[0])
	}
	if o.Header[1] != "X-Two: value2" {
		t.Errorf("Header[1] = %q, want X-Two: value2", o.Header[1])
	}
}

func TestParseConfCumulativeIndexOut(t *testing.T) {
	input := "index-out=1=/out/file1.dat\nindex-out=4=/out/file4.dat\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(o.IndexOut) != 2 {
		t.Fatalf("IndexOut len = %d, want 2", len(o.IndexOut))
	}
	if o.IndexOut[0] != "1=/out/file1.dat" {
		t.Errorf("IndexOut[0] = %q", o.IndexOut[0])
	}
	if o.IndexOut[1] != "4=/out/file4.dat" {
		t.Errorf("IndexOut[1] = %q", o.IndexOut[1])
	}
}

func TestParseConfAccumulativeBTTracker(t *testing.T) {
	input := "bt-tracker=udp://t1:6969/announce\nbt-tracker=udp://t2:6969/announce\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(o.BTTracker) != 2 {
		t.Fatalf("BTTracker len = %d, want 2", len(o.BTTracker))
	}
	if o.BTTracker[0] != "udp://t1:6969/announce" {
		t.Errorf("BTTracker[0] = %q", o.BTTracker[0])
	}
	if o.BTTracker[1] != "udp://t2:6969/announce" {
		t.Errorf("BTTracker[1] = %q", o.BTTracker[1])
	}
}

func TestParseConfAccumulativeDHTEntryPoint(t *testing.T) {
	input := "dht-entry-point=host1:6881\ndht-entry-point=host2:6882\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(o.DHTEntryPoint) != 2 {
		t.Fatalf("DHTEntryPoint len = %d, want 2", len(o.DHTEntryPoint))
	}
}

func TestParseConfMultipleEqualsInValue(t *testing.T) {
	input := "header=Authorization: Bearer token=abc123\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(o.Header) != 1 {
		t.Fatalf("Header len = %d, want 1", len(o.Header))
	}
	if o.Header[0] != "Authorization: Bearer token=abc123" {
		t.Errorf("Header[0] = %q", o.Header[0])
	}
}

func TestParseConfEmptyValue(t *testing.T) {
	input := "http-user=\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.HTTPUser != "" {
		t.Errorf("HTTPUser = %q, want empty", o.HTTPUser)
	}
}

func TestParseConfLineOnlyEqualsSkipped(t *testing.T) {
	input := "=\ndir=/tmp/aria2\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Dir != "/tmp/aria2" {
		t.Errorf("Dir = %q, want /tmp/aria2", o.Dir)
	}
}

func TestParseConfCRLFLineEndings(t *testing.T) {
	input := "dir=/tmp/aria2\r\nenable-rpc=true\r\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Dir != "/tmp/aria2" {
		t.Errorf("Dir = %q, want /tmp/aria2", o.Dir)
	}
	if !o.EnableRPC {
		t.Errorf("EnableRPC = false, want true")
	}
}

func TestParseConfBlankLines(t *testing.T) {
	input := "\n\ndir=/tmp/aria2\n\nenable-rpc=true\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Dir != "/tmp/aria2" {
		t.Errorf("Dir = %q", o.Dir)
	}
	if !o.EnableRPC {
		t.Errorf("EnableRPC = false, want true")
	}
}

func TestParseConfWhitespaceOnlyLines(t *testing.T) {
	input := "   \ndir=/tmp/aria2\n\t\nenable-rpc=true\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Dir != "/tmp/aria2" {
		t.Errorf("Dir = %q", o.Dir)
	}
	if !o.EnableRPC {
		t.Errorf("EnableRPC = false, want true")
	}
}

func TestParseConfIntegerZero(t *testing.T) {
	input := "max-connection-per-server=0\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.MaxConnectionPerServer != 0 {
		t.Errorf("MaxConnectionPerServer = %d, want 0", o.MaxConnectionPerServer)
	}
}

func TestParseConfIntegerNegative(t *testing.T) {
	input := "max-concurrent-downloads=-1\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.MaxConcurrentDownloads != -1 {
		t.Errorf("MaxConcurrentDownloads = %d, want -1", o.MaxConcurrentDownloads)
	}
}

func TestParseConfNonNumericInteger(t *testing.T) {
	input := "max-concurrent-downloads=abc\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err == nil {
		t.Fatalf("expected error for non-numeric integer, got nil")
	}
}

func TestParseConfStringSizeValue(t *testing.T) {
	input := "min-split-size=20M\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.MinSplitSize != "20M" {
		t.Errorf("MinSplitSize = %q, want 20M", o.MinSplitSize)
	}
}

func TestParseConfStringTimeValue(t *testing.T) {
	input := "connect-timeout=60\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.ConnectTimeout != "60" {
		t.Errorf("ConnectTimeout = %q, want 60", o.ConnectTimeout)
	}
}

func TestParseConfMultipleOptions(t *testing.T) {
	input := "dir=/downloads\nmax-concurrent-downloads=10\ncheck-integrity=true\nftp-pasv=false\nsplit=8\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Dir != "/downloads" {
		t.Errorf("Dir = %q", o.Dir)
	}
	if o.MaxConcurrentDownloads != 10 {
		t.Errorf("MaxConcurrentDownloads = %d", o.MaxConcurrentDownloads)
	}
	if !o.CheckIntegrity {
		t.Errorf("CheckIntegrity = false, want true")
	}
	if o.FTPPasv {
		t.Errorf("FTPPasv = true, want false")
	}
	if o.Split != 8 {
		t.Errorf("Split = %d", o.Split)
	}
}

func TestParseConfNoEqualsSignSkipped(t *testing.T) {
	input := "sometext\ndir=/tmp/aria2\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Dir != "/tmp/aria2" {
		t.Errorf("Dir = %q, want /tmp/aria2", o.Dir)
	}
}

func TestParseConfEmptyInput(t *testing.T) {
	var o Options
	err := ParseConf(strings.NewReader(""), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseConfOnlyComments(t *testing.T) {
	input := "# comment one\n# comment two\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseConfBTExcludeTrackerAccumulative(t *testing.T) {
	input := "bt-exclude-tracker=udp://t1:6969/announce\nbt-exclude-tracker=udp://t2:6969/announce\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(o.BTExcludeTracker) != 2 {
		t.Fatalf("BTExcludeTracker len = %d, want 2", len(o.BTExcludeTracker))
	}
}

func TestParseConfDHTEntryPoint6Accumulative(t *testing.T) {
	input := "dht-entry-point6=host1:6881\ndht-entry-point6=host2:6882\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(o.DHTEntryPoint6) != 2 {
		t.Fatalf("DHTEntryPoint6 len = %d, want 2", len(o.DHTEntryPoint6))
	}
}

func TestParseConfBooleanTrueFalseCaseSensitive(t *testing.T) {
	input1 := "check-integrity=True\n"
	var o1 Options
	err := ParseConf(strings.NewReader(input1), &o1)
	if err == nil {
		t.Fatalf("expected error for 'True', got nil")
	}

	input2 := "check-integrity=FALSE\n"
	var o2 Options
	err = ParseConf(strings.NewReader(input2), &o2)
	if err == nil {
		t.Fatalf("expected error for 'FALSE', got nil")
	}
}

func TestParseConfStderrBoolean(t *testing.T) {
	input := "stderr=true\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !o.Stderr {
		t.Errorf("Stderr = false, want true")
	}
}

func TestParseConfRPCListenPort(t *testing.T) {
	input := "rpc-listen-port=6800\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.RPCListenPort != 6800 {
		t.Errorf("RPCListenPort = %d, want 6800", o.RPCListenPort)
	}
}

func TestParseConfMaxDownloadResult(t *testing.T) {
	input := "max-download-result=1000\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.MaxDownloadResult != 1000 {
		t.Errorf("MaxDownloadResult = %d, want 1000", o.MaxDownloadResult)
	}
}

func TestParseConfAria2ConfFullExample(t *testing.T) {
	input := `dir=${HOME}/downloads
max-concurrent-downloads=10
check-integrity=true
continue=true
split=5
max-connection-per-server=5
min-split-size=20M
quiet=false
show-console-readout=true
human-readable=true
summary-interval=60
force-sequential=false
stderr=false
http-user=myuser
http-passwd=mypassword
user-agent=aria2/1.37.0
enable-http-keep-alive=true
http-accept-gzip=false
check-certificate=true
header=X-Forwarded-For: 203.0.113.1
ftp-pasv=true
ftp-type=binary
connect-timeout=60
timeout=120
max-tries=5
remote-time=false
reuse-uri=true
rpc-listen-port=6800
rpc-listen-all=false
enable-rpc=true
pause=false
max-download-result=1000
enable-color=true
bt-max-peers=55
bt-enable-lpd=false
enable-dht=true
enable-peer-exchange=true
follow-metalink=true
metalink-enable-unique-protocol=true
`
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Dir != "${HOME}/downloads" {
		t.Errorf("Dir = %q", o.Dir)
	}
	if o.MaxConcurrentDownloads != 10 {
		t.Errorf("MaxConcurrentDownloads = %d", o.MaxConcurrentDownloads)
	}
	if !o.CheckIntegrity {
		t.Errorf("CheckIntegrity = false, want true")
	}
	if o.Split != 5 {
		t.Errorf("Split = %d", o.Split)
	}
	if o.MinSplitSize != "20M" {
		t.Errorf("MinSplitSize = %q", o.MinSplitSize)
	}
	if o.HTTPUser != "myuser" {
		t.Errorf("HTTPUser = %q", o.HTTPUser)
	}
	if o.UserAgent != "aria2/1.37.0" {
		t.Errorf("UserAgent = %q", o.UserAgent)
	}
	if o.RPCListenPort != 6800 {
		t.Errorf("RPCListenPort = %d", o.RPCListenPort)
	}
	if !o.EnableRPC {
		t.Errorf("EnableRPC = false, want true")
	}
	if o.BTMaxPeers != 55 {
		t.Errorf("BTMaxPeers = %d", o.BTMaxPeers)
	}
	if !o.EnableDHT {
		t.Errorf("EnableDHT = false, want true")
	}
	if o.MaxDownloadResult != 1000 {
		t.Errorf("MaxDownloadResult = %d", o.MaxDownloadResult)
	}
}

func TestParseConfRPCStringOptions(t *testing.T) {
	input := "rpc-secret=mysecrettoken\nrpc-user=admin\nrpc-passwd=secret\n"
	var o Options
	err := ParseConf(strings.NewReader(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.RPCSecret != "mysecrettoken" {
		t.Errorf("RPCSecret = %q", o.RPCSecret)
	}
	if o.RPCUser != "admin" {
		t.Errorf("RPCUser = %q", o.RPCUser)
	}
	if o.RPCPasswd != "secret" {
		t.Errorf("RPCPasswd = %q", o.RPCPasswd)
	}
}

func TestParseAria2ConfTestdata(t *testing.T) {
	data, err := os.ReadFile("testdata/aria2.conf")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var o Options
	err = ParseConf(strings.NewReader(string(data)), &o)
	if err != nil {
		t.Fatalf("ParseConf: %v", err)
	}
	if o.Dir != "/home/user/downloads" {
		t.Errorf("Dir = %q", o.Dir)
	}
	if o.MaxConcurrentDownloads != 10 {
		t.Errorf("MaxConcurrentDownloads = %d", o.MaxConcurrentDownloads)
	}
	if !o.CheckIntegrity {
		t.Error("CheckIntegrity should be true")
	}
	if o.Split != 5 {
		t.Errorf("Split = %d", o.Split)
	}
	if o.HTTPUser != "myuser" {
		t.Errorf("HTTPUser = %q", o.HTTPUser)
	}
	if o.UserAgent != "aria2/1.37.0" {
		t.Errorf("UserAgent = %q", o.UserAgent)
	}
	if o.RPCListenPort != 6800 {
		t.Errorf("RPCListenPort = %d", o.RPCListenPort)
	}
	if !o.EnableRPC {
		t.Error("EnableRPC should be true")
	}
	if o.BTMaxPeers != 55 {
		t.Errorf("BTMaxPeers = %d", o.BTMaxPeers)
	}
	if !o.EnableDHT {
		t.Error("EnableDHT should be true")
	}
}
