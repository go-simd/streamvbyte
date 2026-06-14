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

// encodeSafeGroups returns the largest number of leading complete groups for
// which the SIMD encode kernel can perform its 16-byte data STORE at every group
// without writing past the end of dst's data region. The kernel compacts one
// group's significant bytes with a 16-byte vector store at the running data
// offset, advancing by lengthTable[ctrl]; a group is therefore safe only while
// dataOffset+16 <= len(data). It also fills the per-group control bytes (ctrl[g])
// as it walks, since the compaction shuffle is keyed by them. This is O(g) and
// runs once per Encode call; the heavy per-integer work stays in the kernel.
func encodeSafeGroups(ctrl []byte, src []uint32, groups, dataLen int) int {
	off := 0
	safe := groups
	for g := 0; g < groups; g++ {
		key := controlByte(src[g*4], src[g*4+1], src[g*4+2], src[g*4+3])
		ctrl[g] = key
		if safe == groups && off+16 > dataLen {
			safe = g
		}
		off += int(lengthTable[key])
	}
	return safe
}
