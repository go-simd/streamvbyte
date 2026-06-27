//go:build ppc64le

package streamvbyte

import (
	"bytes"
	"math/rand"
	"testing"
)

// TestDispatchPPC64LE drives both the encode and decode kernels down both of
// their branches — the VSX kernel and the scalar codec fallback (encodeTail /
// decodeTail with groups==0). The kernels are built from ISA-2.07
// (POWER8-baseline) ops only (the element-order load/store is emitted as
// LXVD2X/STXVD2X + a VPERM byte-reversal, not the ISA-3.0 LXVB16X/STXVB16X), so
// neither SIGILLs on POWER8; both kernel branches are exercised unconditionally
// here. (At runtime the encode kernel is POWER9-gated on perf grounds — see
// encode_ppc64le.go — but for differential correctness the test forces it on
// regardless.) The QEMU power8 and power9 CI jobs plus the native POWER8E/POWER9
// farm runs all cover the kernel branch. Each variant is checked against the
// independent refEncode reference plus a full round trip.
func TestDispatchPPC64LE(t *testing.T) {
	savedD, savedE := hasVSXDecode, hasVSXEncode
	defer func() { hasVSXDecode, hasVSXEncode = savedD, savedE }()

	rng := rand.New(rand.NewSource(7))
	check := func(label string) {
		for _, n := range []int{0, 1, 2, 3, 4, 5, 7, 8, 15, 16, 17, 31, 64, 100, 256, 1000} {
			src := make([]uint32, n)
			for i := range src {
				// Mix of all four width classes so every control code is hit.
				switch i & 3 {
				case 0:
					src[i] = uint32(rng.Intn(256))
				case 1:
					src[i] = uint32(rng.Intn(1 << 16))
				case 2:
					src[i] = uint32(rng.Intn(1 << 24))
				default:
					src[i] = rng.Uint32()
				}
			}
			dst := make([]byte, EncodedMaxLen(n))
			w := Encode(dst, src)
			if ref := refEncode(src); !bytes.Equal(dst[:w], ref) {
				t.Fatalf("%s n=%d: Encode mismatch vs reference", label, n)
			}
			out := make([]uint32, n)
			r := Decode(out, dst[:w], n)
			if r != w {
				t.Fatalf("%s n=%d: Decode consumed %d want %d", label, n, r, w)
			}
			for i := range src {
				if out[i] != src[i] {
					t.Fatalf("%s n=%d: out[%d]=%d want %d", label, n, i, out[i], src[i])
				}
			}
		}
	}

	// Scalar fallback: always safe on every ppc64le host (POWER8 included).
	hasVSXDecode, hasVSXEncode = false, false
	check("fallback")

	// VSX kernels: ISA-2.07 baseline, so both run on every ppc64le host (POWER8+);
	// force them on unconditionally for differential correctness.
	hasVSXDecode, hasVSXEncode = true, true
	check("kernel")
}

// forceEncodeKernel forces the (POWER9-gated) encode VSX kernel on for the
// duration of a test, so the encodeSafeGroups tight-buffer guard is exercised on
// POWER8 too (where the kernel is otherwise off at runtime). Restored on cleanup.
func forceEncodeKernel(t *testing.T) {
	t.Helper()
	saved := hasVSXEncode
	t.Cleanup(func() { hasVSXEncode = saved })
	hasVSXEncode = true
}
