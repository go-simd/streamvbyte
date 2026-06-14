package streamvbyte

// encodeTail encodes integers [groups*4, n) — the part the SIMD kernels leave to
// scalar code — writing control bytes into dst[:cl] and the significant data
// bytes into dst[cl:] starting at data offset dataConsumed, and returns the TOTAL
// data bytes written. The portable (non-SIMD) build calls it with groups == 0 and
// dataConsumed == 0, so this is also the complete scalar reference encoder; every
// architecture shares this one path, keeping the wire format in a single place.
//
// It writes one control byte per group (every four integers) and a partial
// control byte for any final < 4 group, so it is correct for any remaining count,
// not just the < 4 SIMD remainder.
func encodeTail(dst []byte, src []uint32, n, groups, dataConsumed int) int {
	done := groups * 4
	if done >= n {
		return dataConsumed
	}
	cl := controlLen(n)
	ctrl := dst[:cl]
	data := dst[cl:]
	dp := dataConsumed
	ki := groups
	var key byte
	shift := uint(0)
	for c := done; c < n; c++ {
		if shift == 8 {
			ctrl[ki] = key
			ki++
			key = 0
			shift = 0
		}
		val := src[c]
		var cd byte
		switch {
		case val < 1<<8:
			data[dp] = byte(val)
			dp++
			cd = 0
		case val < 1<<16:
			data[dp] = byte(val)
			data[dp+1] = byte(val >> 8)
			dp += 2
			cd = 1
		case val < 1<<24:
			data[dp] = byte(val)
			data[dp+1] = byte(val >> 8)
			data[dp+2] = byte(val >> 16)
			dp += 3
			cd = 2
		default:
			data[dp] = byte(val)
			data[dp+1] = byte(val >> 8)
			data[dp+2] = byte(val >> 16)
			data[dp+3] = byte(val >> 24)
			dp += 4
			cd = 3
		}
		key |= cd << shift
		shift += 2
	}
	ctrl[ki] = key // last (partial) key
	return dp
}

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
