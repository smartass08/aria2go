package progress

import (
	"encoding/binary"
	"io"
	"math"
	"os"
	"sync"

	"github.com/smartass08/aria2go/internal/core"
)

// Suffix appended to the download path for the progress info file.
const Suffix = ".aria2"

// Max info hash length (SHA-1).
const infoHashLength = 20

// Sentinel errors.
var (
	ErrBadVersion      = core.NewError(core.ExitTorrentParseError, "unsupported progress file version")
	ErrBadInfoHash     = core.NewError(core.ExitTorrentParseError, "invalid info hash length")
	ErrZeroPieceLength = core.NewError(core.ExitTorrentParseError, "piece length must not be 0")
	ErrFileTruncated   = core.NewError(core.ExitTorrentParseError, "progress file truncated")
)

var infoPool = sync.Pool{
	New: func() any { return &Info{} },
}

func getInfo() *Info {
	return infoPool.Get().(*Info)
}

func putInfo(info *Info) {
	info.InfoHash = nil
	info.Bitfield = nil
	info.InFlight = nil
	info.PieceLength = 0
	info.TotalLength = 0
	info.UploadLength = 0
	infoPool.Put(info)
}

var (
	headerBT    = [6]byte{0x00, 0x01, 0x00, 0x00, 0x00, 0x01}
	headerNonBT = [6]byte{0x00, 0x01, 0x00, 0x00, 0x00, 0x00}
)

// Info holds the BT progress state serialized to a .aria2 file.
type Info struct {
	InfoHash     []byte
	PieceLength  int64
	TotalLength  int64
	UploadLength int64
	Bitfield     []byte
	InFlight     []InFlightPiece
}

// InFlightPiece describes a piece currently being downloaded.
type InFlightPiece struct {
	Index    int
	Length   int32
	Bitfield []byte
}

// Save writes the progress info to <path>.aria2 using atomic-temp-file semantics.
func Save(path string, info *Info) error {
	tmpPath := path + Suffix + "__temp"

	f, err := os.Create(tmpPath)
	if err != nil {
		return core.WrapError(core.ExitFileIOError, "failed to create progress temp file", err)
	}

	wErr := encode(f, info)
	cErr := f.Close()
	if wErr != nil {
		os.Remove(tmpPath)
		return wErr
	}
	if cErr != nil {
		os.Remove(tmpPath)
		return core.WrapError(core.ExitFileIOError, "failed to close progress temp file", cErr)
	}

	if err := os.Rename(tmpPath, path+Suffix); err != nil {
		os.Remove(tmpPath)
		return core.WrapError(core.ExitFileIOError, "failed to rename progress temp file", err)
	}

	return nil
}

// Load reads the progress info from <path>.aria2.
func Load(path string) (*Info, error) {
	f, err := os.Open(path + Suffix)
	if err != nil {
		return nil, core.WrapError(core.ExitFileIOError, "failed to open progress file", err)
	}
	defer f.Close()

	return decode(f)
}

func encode(w io.Writer, info *Info) error {
	if info.PieceLength <= 0 || info.PieceLength > math.MaxUint32 {
		return core.NewError(core.ExitTorrentParseError, "piece length out of range")
	}

	bufLen := 2 + 4
	bufLen += 4 + len(info.InfoHash)
	bufLen += 4 + 8 + 8
	bufLen += 4 + len(info.Bitfield)
	bufLen += 4
	for _, p := range info.InFlight {
		bufLen += 4 + 4 + 4 + len(p.Bitfield)
	}

	buf := make([]byte, bufLen)
	pos := 0

	if len(info.InfoHash) > 0 {
		copy(buf[pos:], headerBT[:])
	} else {
		copy(buf[pos:], headerNonBT[:])
	}
	pos += 6

	binary.BigEndian.PutUint32(buf[pos:], uint32(len(info.InfoHash)))
	pos += 4
	if n := copy(buf[pos:], info.InfoHash); n > 0 {
		pos += n
	}

	binary.BigEndian.PutUint32(buf[pos:], uint32(info.PieceLength))
	pos += 4

	binary.BigEndian.PutUint64(buf[pos:], uint64(info.TotalLength))
	pos += 8

	binary.BigEndian.PutUint64(buf[pos:], uint64(info.UploadLength))
	pos += 8

	binary.BigEndian.PutUint32(buf[pos:], uint32(len(info.Bitfield)))
	pos += 4
	if n := copy(buf[pos:], info.Bitfield); n > 0 {
		pos += n
	}

	binary.BigEndian.PutUint32(buf[pos:], uint32(len(info.InFlight)))
	pos += 4
	for _, p := range info.InFlight {
		binary.BigEndian.PutUint32(buf[pos:], uint32(p.Index))
		pos += 4
		binary.BigEndian.PutUint32(buf[pos:], uint32(p.Length))
		pos += 4
		binary.BigEndian.PutUint32(buf[pos:], uint32(len(p.Bitfield)))
		pos += 4
		if n := copy(buf[pos:], p.Bitfield); n > 0 {
			pos += n
		}
	}

	if _, err := w.Write(buf); err != nil {
		return core.WrapError(core.ExitFileIOError, "failed to write progress data", err)
	}
	return nil
}

func decode(r io.Reader) (*Info, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, core.WrapError(core.ExitFileIOError, "failed to read progress data", err)
	}
	return decodeFromBytes(data)
}

func decodeFromBytes(data []byte) (*Info, error) {
	info := getInfo()
	if err := parseIntoInfo(data, info); err != nil {
		putInfo(info)
		return nil, err
	}
	return info, nil
}

func parseIntoInfo(data []byte, info *Info) error {
	if len(data) < 2 {
		return core.WrapError(core.ExitFileIOError, "failed to read version", ErrFileTruncated)
	}

	useNetworkOrder := true
	switch {
	case data[0] == 0x00 && data[1] == 0x01:
		useNetworkOrder = true
	case data[0] == 0x00 && data[1] == 0x00:
		useNetworkOrder = false
	default:
		return ErrBadVersion
	}

	if len(data) < 6 {
		return core.WrapError(core.ExitFileIOError, "failed to read extension", ErrFileTruncated)
	}

	btMode := (data[5] & 1) != 0
	pos := 6

	if pos+4 > len(data) {
		return core.WrapError(core.ExitFileIOError, "failed to read info hash length", ErrFileTruncated)
	}
	ihl := uint32FromBytes(data[pos:], useNetworkOrder)
	pos += 4

	if ihl > infoHashLength {
		return ErrBadInfoHash
	}
	if btMode && ihl != infoHashLength {
		return ErrBadInfoHash
	}

	if ihl > 0 {
		if pos+int(ihl) > len(data) {
			return core.WrapError(core.ExitFileIOError, "failed to read info hash", ErrFileTruncated)
		}
		info.InfoHash = make([]byte, ihl)
		copy(info.InfoHash, data[pos:pos+int(ihl)])
		pos += int(ihl)
	}

	if pos+4 > len(data) {
		return core.WrapError(core.ExitFileIOError, "failed to read piece length", ErrFileTruncated)
	}
	pl := int64(uint32FromBytes(data[pos:], useNetworkOrder))
	pos += 4
	if pl == 0 {
		return ErrZeroPieceLength
	}
	info.PieceLength = pl

	if pos+8 > len(data) {
		return core.WrapError(core.ExitFileIOError, "failed to read total length", ErrFileTruncated)
	}
	info.TotalLength = int64(uint64FromBytes(data[pos:], useNetworkOrder))
	pos += 8

	if pos+8 > len(data) {
		return core.WrapError(core.ExitFileIOError, "failed to read upload length", ErrFileTruncated)
	}
	info.UploadLength = int64(uint64FromBytes(data[pos:], useNetworkOrder))
	pos += 8

	if pos+4 > len(data) {
		return core.WrapError(core.ExitFileIOError, "failed to read bitfield length", ErrFileTruncated)
	}
	bfl := int(uint32FromBytes(data[pos:], useNetworkOrder))
	pos += 4

	if bfl > 0 {
		if pos+bfl > len(data) {
			return core.WrapError(core.ExitFileIOError, "failed to read bitfield", ErrFileTruncated)
		}
		info.Bitfield = make([]byte, bfl)
		copy(info.Bitfield, data[pos:pos+bfl])
		pos += bfl
	}

	if pos+4 > len(data) {
		return core.WrapError(core.ExitFileIOError, "failed to read in-flight count", ErrFileTruncated)
	}
	nif := int(uint32FromBytes(data[pos:], useNetworkOrder))
	pos += 4

	info.InFlight = make([]InFlightPiece, nif)
	for i := 0; i < nif; i++ {
		if pos+4 > len(data) {
			return core.WrapError(core.ExitFileIOError, "failed to read in-flight index", ErrFileTruncated)
		}
		idx := uint32FromBytes(data[pos:], useNetworkOrder)
		pos += 4

		if pos+4 > len(data) {
			return core.WrapError(core.ExitFileIOError, "failed to read in-flight length", ErrFileTruncated)
		}
		length := uint32FromBytes(data[pos:], useNetworkOrder)
		pos += 4

		if pos+4 > len(data) {
			return core.WrapError(core.ExitFileIOError, "failed to read in-flight bitfield length", ErrFileTruncated)
		}
		pbfLen := int(uint32FromBytes(data[pos:], useNetworkOrder))
		pos += 4

		pBitfield := make([]byte, pbfLen)
		if pbfLen > 0 {
			if pos+pbfLen > len(data) {
				return core.WrapError(core.ExitFileIOError, "failed to read piece bitfield", ErrFileTruncated)
			}
			copy(pBitfield, data[pos:pos+pbfLen])
			pos += pbfLen
		}
		info.InFlight[i] = InFlightPiece{
			Index:    int(idx),
			Length:   int32(length),
			Bitfield: pBitfield,
		}
	}

	return nil
}

func uint32FromBytes(b []byte, networkOrder bool) uint32 {
	if networkOrder {
		return binary.BigEndian.Uint32(b)
	}
	return binary.NativeEndian.Uint32(b)
}

func uint64FromBytes(b []byte, networkOrder bool) uint64 {
	if networkOrder {
		return binary.BigEndian.Uint64(b)
	}
	return binary.NativeEndian.Uint64(b)
}
