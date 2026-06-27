package streamvbyte

import "testing"

// TestEncodeTightBuffer drives the unexported encode with a dst sized to the
// EXACT encoded length (not EncodedMaxLen). The SIMD kernels store 16 bytes per
// group, so the final groups have < 16 bytes of store room; encodeSafeGroups must
// stop the kernel before it would over-write and hand the remaining groups to the
// scalar encodeTail. This exercises (a) encodeSafeGroups' early-exit fallback and
// (b) encodeTail spanning more than one control byte (the shift==8 path) on the
// SIMD builds, where the normal < 4 tail never reaches them. The public Encode
// keeps the simpler EncodedMaxLen contract; this white-box test validates the
// guard that makes a tight buffer safe.
func TestEncodeTightBuffer(t *testing.T) {
	forceEncodeKernel(t)
	for _, n := range []int{1, 4, 5, 8, 9, 16, 17, 33, 64, 100} {
		src := make([]uint32, n)
		for i := range src {
			// Small (1-byte) values: each group's actual data is only 4 bytes,
			// far below the kernel's 16-byte store width, so on an exactly-sized
			// (tight) buffer off+16 > dataLen fires from the very first group and
			// the whole stream falls back to the scalar encoder.
			src[i] = uint32(i & 0xFF)
		}
		exact := make([]byte, EncodedMaxLen(n))
		w := Encode(exact, src) // reference via the public (loose-buffer) path
		exact = exact[:w]

		tight := make([]byte, w)
		got := encode(tight, src, n)
		if got != w {
			t.Fatalf("n=%d: tight encode wrote %d, want %d", n, got, w)
		}
		for i := range exact {
			if tight[i] != exact[i] {
				t.Fatalf("n=%d: byte %d = %#x, want %#x", n, i, tight[i], exact[i])
			}
		}
		// And it must still decode back to src.
		out := make([]uint32, n)
		if r := Decode(out, tight, n); r != w {
			t.Fatalf("n=%d: Decode consumed %d, want %d", n, r, w)
		}
		if !equalU32(out, src) {
			t.Fatalf("n=%d: tight round-trip mismatch", n)
		}
	}
}
