//go:build !amd64 && !arm64 && !riscv64 && !loong64 && !ppc64le && !s390x

package streamvbyte

// decode is the portable scalar fallback used where no SIMD kernel is built: it
// decodes every integer with decodeTail (groups=0), which is the shared
// reference path. Returns the number of data bytes consumed.
func decode(dst []uint32, src []byte, n int) int {
	return decodeTail(dst, src, n, 0, 0)
}
