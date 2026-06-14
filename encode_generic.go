//go:build !amd64 && !arm64 && !riscv64 && !loong64 && !ppc64le && !s390x

package streamvbyte

// encode is the portable scalar fallback used where no SIMD kernel is built: it
// encodes every integer with encodeTail (groups=0), which is the shared reference
// path. Returns the total number of bytes written (control + data).
func encode(dst []byte, src []uint32, n int) int {
	cl := controlLen(n)
	dp := encodeTail(dst, src, n, 0, 0)
	return cl + dp
}
