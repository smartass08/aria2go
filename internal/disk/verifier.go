package disk

import (
	"bytes"
	"context"
	"sync"

	"github.com/smartass08/aria2go/internal/hash"
)

// verifyBufPool reuses piece-sized read buffers across Verify calls to avoid
// repeated large (256KB–16MB) allocations.
var verifyBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0)
		return &b
	},
}

// Verifier checks piece hash integrity for a download Adaptor.
type Verifier struct {
	adaptor     Adaptor
	pieceHashes [][]byte
	hashKind    hash.Kind
	pieceLen    int64
}

// NewVerifier creates a Verifier that will verify piece hashes using the
// given Adaptor and piece length.
func NewVerifier(a Adaptor, pieceHashes [][]byte, hashKind hash.Kind, pieceLen int64) *Verifier {
	return &Verifier{
		adaptor:     a,
		pieceHashes: pieceHashes,
		hashKind:    hashKind,
		pieceLen:    pieceLen,
	}
}

// Verify checks all pieces marked as Have. It returns sorted indices of bad
// pieces. Read errors for a piece cause the piece to be collected as a bad
// index instead of returning an error immediately. On successful verification
// of a piece, a.MarkPiece(i, true) is called. If pieceHashes is nil or empty,
// nothing is verified. Respects context cancellation.
func (v *Verifier) Verify(ctx context.Context) (badIndices []int, err error) {
	if len(v.pieceHashes) == 0 {
		return nil, nil
	}

	size := v.adaptor.Size()
	pieceLen := v.pieceLen
	if pieceLen == 0 {
		return nil, nil
	}

	buf := v.getBuf(pieceLen)
	defer v.putBuf(buf)

	h, err := hash.New(v.hashKind)
	if err != nil {
		return nil, err
	}
	digestSize := v.hashKind.Size()

	for i := range v.pieceHashes {
		select {
		case <-ctx.Done():
			return badIndices, ctx.Err()
		default:
		}

		if !v.adaptor.Have(i) {
			continue
		}

		start := int64(i) * pieceLen
		if start >= size {
			break
		}
		actualLen := pieceLen
		if start+pieceLen > size {
			actualLen = size - start
		}

		if int64(cap(buf)) < actualLen {
			v.putBuf(buf)
			buf = v.getBuf(actualLen)
		}
		b := buf[:actualLen]

		n, readErr := v.adaptor.ReadAt(b, int64(i)*pieceLen)
		if readErr != nil {
			badIndices = append(badIndices, i)
			continue
		}
		if int64(n) < actualLen {
			badIndices = append(badIndices, i)
			continue
		}

		h.Reset()
		h.Write(b)
		got := h.Sum(nil)

		if len(got) != digestSize {
			badIndices = append(badIndices, i)
			continue
		}
		if !bytes.Equal(got, v.pieceHashes[i]) {
			badIndices = append(badIndices, i)
			continue
		}

		v.adaptor.MarkPiece(i, true)
	}

	return badIndices, nil
}

func (v *Verifier) getBuf(minSize int64) []byte {
	bp := verifyBufPool.Get().(*[]byte)
	b := *bp
	if int64(cap(b)) >= minSize {
		return b[:cap(b)]
	}
	return make([]byte, minSize)
}

func (v *Verifier) putBuf(b []byte) {
	bp := &b
	verifyBufPool.Put(bp)
}
