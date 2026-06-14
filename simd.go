//go:build amd64 || arm64 || riscv64 || loong64 || ppc64le || s390x

package streamvbyte

// safeGroups returns the largest number of leading complete groups (g <= groups)
// for which the SIMD kernel can perform its 16-byte data load at every group
// without reading past the end of src. The kernel loads 16 bytes at each group's
// data offset, so a group is safe only while dataOffset+16 <= len(data).
//
// We walk the control bytes accumulating per-group data lengths (lengthTable),
// stopping at the first group whose 16-byte load would over-read. This is O(g)
// and runs once per Decode call; the heavy per-integer work stays in the kernel.
func safeGroups(src []byte, cl, groups int) int {
	dataLen := len(src) - cl
	off := 0
	for g := 0; g < groups; g++ {
		if off+16 > dataLen {
			return g
		}
		off += int(lengthTable[src[g]])
	}
	return groups
}
