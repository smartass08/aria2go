package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/smartass08/aria2go/internal/bencode"
	"github.com/smartass08/aria2go/internal/core"
	"github.com/smartass08/aria2go/internal/sessionfile"
)

func main() {
	if err := genTorrentFixtures("internal/torrent/testdata"); err != nil {
		log.Fatalf("torrent: %v", err)
	}
	if err := genSessionFixtures("internal/sessionfile/testdata"); err != nil {
		log.Fatalf("session: %v", err)
	}
	if err := genBencodeFixtures("internal/bencode/testdata"); err != nil {
		log.Fatalf("bencode: %v", err)
	}
	fmt.Println("binary fixtures generated.")
}

func genTorrentFixtures(dir string) error {
	// single.torrent: 1 file, 1024 bytes, 1 piece
	single := buildTorrent("hello.txt", 1024, nil, 65536, 1)
	if err := os.WriteFile(filepath.Join(dir, "single.torrent"), single, 0644); err != nil {
		return err
	}

	// multi.torrent: 2 files, 500 + 700 = 1200 bytes, pieceLength=600 => 2 pieces
	multi := buildTorrent("mydir", 0, []fileSpec{
		{path: []string{"sub", "file1.bin"}, length: 500},
		{path: []string{"file2.bin"}, length: 700},
	}, 600, 2)
	if err := os.WriteFile(filepath.Join(dir, "multi.torrent"), multi, 0644); err != nil {
		return err
	}

	return nil
}

type fileSpec struct {
	path   []string
	length int64
}

func buildTorrent(name string, length int64, files []fileSpec, pieceLen int64, numPieces int) []byte {
	info := bencode.NewDict()
	info.Set("name", bencode.NewString(name))
	info.Set("piece length", bencode.IntVal{I: pieceLen})

	pieces := make([]byte, numPieces*20)
	info.Set("pieces", bencode.NewString(string(pieces)))

	if files != nil {
		var fileList []bencode.Value
		for _, f := range files {
			fd := bencode.NewDict()
			fd.Set("length", bencode.IntVal{I: f.length})
			var pathComps []bencode.Value
			for _, p := range f.path {
				pathComps = append(pathComps, bencode.NewString(p))
			}
			fd.Set("path", bencode.ListVal{L: pathComps})
			fileList = append(fileList, fd)
		}
		info.Set("files", bencode.ListVal{L: fileList})
	} else {
		info.Set("length", bencode.IntVal{I: length})
	}

	d := bencode.NewDict()
	d.Set("announce", bencode.NewString("http://tracker.example.com/announce"))
	d.Set("creation date", bencode.IntVal{I: 1700000000})
	d.Set("comment", bencode.NewString("test fixture"))
	d.Set("info", info)

	data, err := bencode.Marshal(d)
	if err != nil {
		panic(err)
	}
	return data
}

func genSessionFixtures(dir string) error {
	// basic.session: single HTTP download
	basic := []sessionfile.Entry{{
		URIs:   []string{"https://example.com/file.zip"},
		GID:    core.GID(0x0000000000000001),
		Status: core.StatusPaused,
		Options: map[string]string{
			"dir":   "/home/user/downloads",
			"out":   "file.zip",
			"split": "5",
		},
	}}
	if err := sessionfile.AtomicSave(filepath.Join(dir, "basic.session"), basic, false); err != nil {
		return err
	}

	// multi.session: multiple entries with various options
	multi := []sessionfile.Entry{
		{
			URIs:   []string{"https://example.com/file1.iso"},
			GID:    core.GID(0x000000000000000a),
			Status: core.StatusWaiting,
			Options: map[string]string{
				"dir":        "/downloads",
				"out":        "file1.iso",
				"max-tries":  "3",
				"user-agent": "aria2go/1.0",
			},
		},
		{
			URIs:   []string{"https://example.com/file2.zip", "https://mirror.example.com/file2.zip"},
			GID:    core.GID(0x000000000000000b),
			Status: core.StatusPaused,
			Options: map[string]string{
				"dir":                    "/downloads",
				"out":                    "file2.zip",
				"split":                  "8",
				"check-integrity":        "true",
				"enable-http-keep-alive": "true",
			},
		},
		{
			URIs:   []string{"magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			GID:    core.GID(0x000000000000000c),
			Status: core.StatusWaiting,
			Options: map[string]string{
				"dir":          "/downloads",
				"bt-tracker":   "http://t1.example.com/announce\nhttp://t2.example.com/announce",
				"bt-max-peers": "55",
			},
		},
	}
	if err := sessionfile.AtomicSave(filepath.Join(dir, "multi.session"), multi, false); err != nil {
		return err
	}

	// gzip.session.gz
	gzipEntry := []sessionfile.Entry{{
		URIs:   []string{"https://example.com/compressed.bin"},
		GID:    core.GID(0x0000000000000014),
		Status: core.StatusActive,
		Options: map[string]string{
			"dir":    "/tmp",
			"out":    "compressed.bin",
			"header": "Accept: application/octet-stream",
		},
	}}
	if err := sessionfile.AtomicSave(filepath.Join(dir, "gzip.session.gz"), gzipEntry, true); err != nil {
		return err
	}

	return nil
}

func genBencodeFixtures(dir string) error {
	// sample.bencode: complex nested bencode data
	innerDict := bencode.NewDict()
	innerDict.Set("192.168.1.1", bencode.IntVal{I: 6881})
	innerDict.Set("10.0.0.1", bencode.IntVal{I: 6882})

	fileDict := bencode.NewDict()
	fileDict.Set("length", bencode.IntVal{I: 1024})
	fileDict.Set("path", bencode.ListVal{L: []bencode.Value{
		bencode.NewString("dir"),
		bencode.NewString("example.dat"),
	}})

	d := bencode.NewDict()
	d.Set("announce", bencode.NewString("http://tracker.example.com/announce"))
	d.Set("comment", bencode.NewString("complex bencode fixture"))
	d.Set("creation date", bencode.IntVal{I: 1700000000})
	d.Set("nodes", bencode.ListVal{L: []bencode.Value{
		bencode.ListVal{L: []bencode.Value{bencode.NewString("dht.example.com"), bencode.IntVal{I: 6881}}},
		bencode.ListVal{L: []bencode.Value{bencode.NewString("dht2.example.com"), bencode.IntVal{I: 6882}}},
	}})
	d.Set("url-list", bencode.ListVal{L: []bencode.Value{
		bencode.NewString("http://seed1/file"),
		bencode.NewString("http://seed2/file"),
	}})
	d.Set("private", bencode.IntVal{I: 0})
	d.Set("encoding", bencode.NewString("UTF-8"))

	info := bencode.NewDict()
	info.Set("name", bencode.NewString("complex.bin"))
	info.Set("piece length", bencode.IntVal{I: 262144})
	info.Set("pieces", bencode.NewString(string(make([]byte, 20))))
	info.Set("files", bencode.ListVal{L: []bencode.Value{fileDict}})
	d.Set("info", info)

	data, err := bencode.Marshal(d)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "sample.bencode"), data, 0644)
}
