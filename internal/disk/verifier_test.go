package disk

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/smartass08/aria2go/internal/hash"
)

func TestVerifierEmptyHashes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")
	const size int64 = 256

	sf, err := NewSingleFile(p, size, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	sf.SetPieceCount(4)

	v := NewVerifier(sf, nil, hash.SHA1)
	bad, err := v.Verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 0 {
		t.Errorf("badIndices = %v, want nil for nil hashes", bad)
	}

	v2 := NewVerifier(sf, [][]byte{}, hash.SHA1)
	bad, err = v2.Verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 0 {
		t.Errorf("badIndices = %v, want nil for empty hashes", bad)
	}
}

func TestVerifierAllPass(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	// Create 4 pieces, 16 bytes each = 64 bytes total.
	const pieceLen = 16
	const numPieces = 4
	const totalSize = pieceLen * numPieces

	// Seed data for each piece.
	pieceData := [][]byte{
		[]byte("AAAAAAAABBBBBBBB"),
		[]byte("CCCCCCCCDDDDDDDD"),
		[]byte("EEEEEEEEFFFFFFFF"),
		[]byte("GGGGGGGGHHHHHHHH"),
	}

	sf, err := NewSingleFile(p, totalSize, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	sf.SetPieceCount(numPieces)

	hasher, err := hash.New(hash.SHA1)
	if err != nil {
		t.Fatal(err)
	}

	pieceHashes := make([][]byte, numPieces)
	for i := 0; i < numPieces; i++ {
		if _, err := sf.WriteAt(pieceData[i], int64(i)*pieceLen); err != nil {
			t.Fatal(err)
		}
		hasher.Reset()
		hasher.Write(pieceData[i])
		pieceHashes[i] = hasher.Sum(nil)
		sf.MarkPiece(i, true)
	}

	v := NewVerifier(sf, pieceHashes, hash.SHA1)
	bad, err := v.Verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 0 {
		t.Errorf("badIndices = %v, want nil", bad)
	}
}

func TestVerifierNonEvenPieceLen(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	// totalSize not evenly divisible: verifier derives pieceLen = 30/4 = 7.
	const totalSize = 30
	const numPieces = 4

	data := make([]byte, totalSize)
	for i := range data {
		data[i] = byte(i)
	}

	sf, err := NewSingleFile(p, totalSize, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	if _, err := sf.WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}

	sf.SetPieceCount(numPieces)

	// Compute expected piece hashes the same way the verifier does:
	// pieceLen = size/numPieces, actualLen = min(pieceLen, size - i*pieceLen).
	pieceLen := int64(totalSize / numPieces) // 7
	hasher, err := hash.New(hash.SHA1)
	if err != nil {
		t.Fatal(err)
	}

	pieceHashes := make([][]byte, numPieces)
	for i := 0; i < numPieces; i++ {
		actualLen := pieceLen
		if rem := int64(totalSize) - int64(i)*pieceLen; rem < pieceLen {
			actualLen = rem
		}
		hasher.Reset()
		hasher.Write(data[i*int(pieceLen) : i*int(pieceLen)+int(actualLen)])
		pieceHashes[i] = hasher.Sum(nil)
		sf.MarkPiece(i, true)
	}

	v := NewVerifier(sf, pieceHashes, hash.SHA1)
	bad, err := v.Verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 0 {
		t.Errorf("badIndices = %v, want nil", bad)
	}
}

func TestVerifierCorruptedData(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	const pieceLen = 16
	const numPieces = 4
	const totalSize = pieceLen * numPieces

	pieceData := [][]byte{
		[]byte("AAAAAAAABBBBBBBB"),
		[]byte("CCCCCCCCDDDDDDDD"),
		[]byte("EEEEEEEEFFFFFFFF"),
		[]byte("GGGGGGGGHHHHHHHH"),
	}

	sf, err := NewSingleFile(p, totalSize, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	sf.SetPieceCount(numPieces)

	hasher, err := hash.New(hash.SHA1)
	if err != nil {
		t.Fatal(err)
	}

	pieceHashes := make([][]byte, numPieces)
	for i := 0; i < numPieces; i++ {
		if _, err := sf.WriteAt(pieceData[i], int64(i)*pieceLen); err != nil {
			t.Fatal(err)
		}
		hasher.Reset()
		hasher.Write(pieceData[i])
		pieceHashes[i] = hasher.Sum(nil)
		sf.MarkPiece(i, true)
	}

	// Corrupt piece 1 and 3.
	if _, err := sf.WriteAt([]byte("XXXXXXXXYYYYYYYY"), 1*pieceLen); err != nil {
		t.Fatal(err)
	}
	if _, err := sf.WriteAt([]byte("ZZZZZZZZWWWWWWWW"), 3*pieceLen); err != nil {
		t.Fatal(err)
	}

	v := NewVerifier(sf, pieceHashes, hash.SHA1)
	bad, err := v.Verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 2 {
		t.Fatalf("badIndices = %v, want length 2", bad)
	}
	if bad[0] != 1 || bad[1] != 3 {
		t.Errorf("badIndices = %v, want [1, 3]", bad)
	}
}

func TestVerifierContextCancelled(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	const pieceLen = 16
	const numPieces = 4
	const totalSize = pieceLen * numPieces

	pieceData := []byte("AAAAAAAAAAAAAAAA")

	sf, err := NewSingleFile(p, totalSize, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	sf.SetPieceCount(numPieces)
	for i := 0; i < numPieces; i++ {
		if _, err := sf.WriteAt(pieceData, int64(i)*pieceLen); err != nil {
			t.Fatal(err)
		}
		sf.MarkPiece(i, true)
	}

	hasher, err := hash.New(hash.SHA1)
	if err != nil {
		t.Fatal(err)
	}
	hasher.Write(pieceData)
	pHash := hasher.Sum(nil)

	pieceHashes := make([][]byte, numPieces)
	for i := range pieceHashes {
		pieceHashes[i] = pHash
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	v := NewVerifier(sf, pieceHashes, hash.SHA1)
	bad, err := v.Verify(ctx)
	if err != context.Canceled {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if len(bad) != 0 {
		t.Errorf("badIndices = %v, want nil", bad)
	}
}

func TestVerifierTimeout(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	const totalSize = 64

	sf, err := NewSingleFile(p, totalSize, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	data := make([]byte, totalSize)
	for i := range data {
		data[i] = byte(i)
	}
	if _, err := sf.WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}

	sf.SetPieceCount(4)
	for i := 0; i < 4; i++ {
		sf.MarkPiece(i, true)
	}

	hasher, err := hash.New(hash.SHA1)
	if err != nil {
		t.Fatal(err)
	}

	pieceHashes := make([][]byte, 4)
	for i := 0; i < 4; i++ {
		start := i * 16
		hasher.Reset()
		hasher.Write(data[start : start+16])
		pieceHashes[i] = hasher.Sum(nil)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)

	v := NewVerifier(sf, pieceHashes, hash.SHA1)
	_, err = v.Verify(ctx)
	if err == nil {
		t.Error("expected context deadline exceeded or canceled")
	}
}

func TestVerifierOnlyHavesChecked(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	const pieceLen = 16
	const numPieces = 4
	const totalSize = pieceLen * numPieces

	pieceData := [][]byte{
		[]byte("AAAAAAAABBBBBBBB"),
		[]byte("CCCCCCCCDDDDDDDD"),
		[]byte("EEEEEEEEFFFFFFFF"),
		[]byte("GGGGGGGGHHHHHHHH"),
	}

	sf, err := NewSingleFile(p, totalSize, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	sf.SetPieceCount(numPieces)

	hasher, err := hash.New(hash.SHA1)
	if err != nil {
		t.Fatal(err)
	}

	pieceHashes := make([][]byte, numPieces)
	for i := 0; i < numPieces; i++ {
		if _, err := sf.WriteAt(pieceData[i], int64(i)*pieceLen); err != nil {
			t.Fatal(err)
		}
		hasher.Reset()
		hasher.Write(pieceData[i])
		pieceHashes[i] = hasher.Sum(nil)
	}

	// Corrupt piece 1 but don't mark it as Have.
	if _, err := sf.WriteAt([]byte("XXXXXXXXYYYYYYYY"), 1*pieceLen); err != nil {
		t.Fatal(err)
	}

	// Mark only pieces 0 and 2 as Have. Pieces 1 and 3 are not Have.
	sf.MarkPiece(0, true)
	sf.MarkPiece(2, true)

	v := NewVerifier(sf, pieceHashes, hash.SHA1)
	bad, err := v.Verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 0 {
		t.Errorf("badIndices = %v, want nil (only Have pieces checked)", bad)
	}
	// Piece 0 and 2 are good, should be re-marked.
	if !sf.Have(0) {
		t.Error("piece 0 should still be Have after successful verify")
	}
	if !sf.Have(2) {
		t.Error("piece 2 should still be Have after successful verify")
	}
}

func TestVerifierReadErrorBecomesBadIndex(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	sf, err := NewSingleFile(p, 64, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}

	sf.SetPieceCount(4)
	sf.MarkPiece(0, true)
	sf.MarkPiece(1, true)

	// Close the file so reads fail.
	sf.Close()

	hasher, err := hash.New(hash.SHA1)
	if err != nil {
		t.Fatal(err)
	}
	hasher.Write([]byte("data"))
	pHash := hasher.Sum(nil)

	pieceHashes := make([][]byte, 4)
	for i := range pieceHashes {
		pieceHashes[i] = pHash
	}

	v := NewVerifier(sf, pieceHashes, hash.SHA1)
	bad, err := v.Verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 2 {
		t.Fatalf("badIndices = %v, want length 2 (read errors)", bad)
	}
	if bad[0] != 0 || bad[1] != 1 {
		t.Errorf("badIndices = %v, want [0, 1]", bad)
	}
}
