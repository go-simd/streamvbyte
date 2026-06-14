package streamvbyte

// decodeTail decodes integers [groups*4, n) — the part the SIMD kernels leave to
// scalar code — into dst, resuming at control byte index groups and the given
// data offset, and returns the TOTAL data bytes consumed. The portable
// (non-SIMD) build calls it with groups == 0 and dataConsumed == 0, so this is
// also the complete scalar reference decoder; every architecture shares this one
// path, keeping the codec's correctness in a single place.
//
// It reloads the next control byte whenever a group boundary is crossed, so it
// is correct for any remaining count, not just the < 4 SIMD remainder.
func decodeTail(dst []uint32, src []byte, n, groups, dataConsumed int) int {
	done := groups * 4
	if done >= n {
		return dataConsumed
	}
	cl := controlLen(n)
	ctrl := src[:cl]
	data := src[cl:]
	dp := dataConsumed
	ki := groups
	key := uint32(ctrl[ki])
	shift := uint(0)
	for c := done; c < n; c++ {
		if shift == 8 {
			ki++
			key = uint32(ctrl[ki])
			shift = 0
		}
		code := (key >> shift) & 0x3
		var val uint32
		switch code {
		case 0:
			val = uint32(data[dp])
			dp++
		case 1:
			val = uint32(data[dp]) | uint32(data[dp+1])<<8
			dp += 2
		case 2:
			val = uint32(data[dp]) | uint32(data[dp+1])<<8 | uint32(data[dp+2])<<16
			dp += 3
		default:
			val = uint32(data[dp]) | uint32(data[dp+1])<<8 | uint32(data[dp+2])<<16 | uint32(data[dp+3])<<24
			dp += 4
		}
		dst[c] = val
		shift += 2
	}
	return dp
}
