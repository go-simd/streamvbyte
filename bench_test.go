package streamvbyte

import (
	"math/rand"
	"testing"
)

func benchData(n int, rng *rand.Rand) []uint32 {
	s := make([]uint32, n)
	for i := range s {
		// Mixed widths typical of real id/posting data.
		switch rng.Intn(4) {
		case 0:
			s[i] = uint32(rng.Intn(256))
		case 1:
			s[i] = uint32(rng.Intn(65536))
		case 2:
			s[i] = uint32(rng.Intn(1 << 24))
		default:
			s[i] = rng.Uint32()
		}
	}
	return s
}

func BenchmarkEncode(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src := benchData(4096, rng)
	dst := make([]byte, EncodedMaxLen(len(src)))
	b.SetBytes(int64(len(src) * 4))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Encode(dst, src)
	}
}

func BenchmarkDecode(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src := benchData(4096, rng)
	enc := make([]byte, EncodedMaxLen(len(src)))
	w := Encode(enc, src)
	enc = enc[:w]
	out := make([]uint32, len(src))
	b.SetBytes(int64(len(src) * 4))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Decode(out, enc, len(src))
	}
}

// BenchmarkDecodeScalar measures the portable path on the same data, so the
// SIMD speedup is visible on architectures with a kernel.
func BenchmarkDecodeScalar(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src := benchData(4096, rng)
	enc := make([]byte, EncodedMaxLen(len(src)))
	w := Encode(enc, src)
	enc = enc[:w]
	out := make([]uint32, len(src))
	b.SetBytes(int64(len(src) * 4))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decodeTail(out, enc, len(src), 0, 0)
	}
}
