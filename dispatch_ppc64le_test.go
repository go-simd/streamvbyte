//go:build ppc64le

package streamvbyte

import (
	"bytes"
	"math/rand"
	"testing"

	"golang.org/x/sys/cpu"
)

// TestDispatchPPC64LE drives both the encode and decode kernels down both of
// their branches — the VSX kernel and the scalar codec fallback (encodeTail /
// decodeTail with groups==0). The kernels emit ISA-3.0 (POWER9) instructions
// (LXVB16X/STXVB16X) that raise SIGILL on POWER8, so the kernel-forcing branch
// runs only when the host is actually POWER9+ (mirroring the amd64 force tests).
// The scalar-fallback branch is always exercised. The power9-targeted QEMU CI job
// and the native POWER9/POWER10 farm runs cover the kernel branch. Each variant
// is checked against the independent refEncode reference plus a full round trip.
func TestDispatchPPC64LE(t *testing.T) {
	saved := hasVSX
	defer func() { hasVSX = saved }()

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
	hasVSX = false
	check("fallback")

	// VSX kernel: only force it on when the CPU is POWER9+, otherwise the
	// LXVB16X/STXVB16X in the kernels would SIGILL (e.g. on a POWER8 farm node).
	if !cpu.PPC64.IsPOWER9 {
		t.Log("CPU is pre-POWER9; VSX kernel branch not exercised on this host")
		return
	}
	hasVSX = true
	check("kernel")
}
