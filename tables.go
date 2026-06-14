package streamvbyte

// lengthTable[key] is the total number of data bytes consumed by the four
// integers a control byte encodes: 4 + sum of the four 2-bit codes. The SIMD
// decode loop uses it to advance the data pointer after each shuffle.
var lengthTable [256]uint8

// shuffleTable[key] is the 16-byte PSHUFB/VTBL/VPERM-style mask that spreads the
// packed little-endian bytes of four integers into four zero-extended uint32
// lanes. Byte j of lane L (L in 0..3, j in 0..3) is sourced from data offset
// "cursor" for j < (code_L+1), else set to a zeroing index. The zeroing index is
// 0xFF for x86 PSHUFB / ppc little-endian semantics; architectures whose
// table-lookup zeroes differently translate it (see the per-arch asm comment).
var shuffleTable [256][16]uint8

// permTable[key] is the same mask family for the VPERM-based targets (ppc64le,
// s390x). VPERM has no "high bit zeroes" rule: it indexes a 32-byte pair formed
// by concatenating the data register (bytes 0..15) with a SECOND register the
// kernel sets to all-zero (bytes 16..31). A lane byte that must be zeroed
// therefore points at index 16 (the first byte of that zero register) instead of
// the 0xFF used by PSHUFB/VTBL/vrgather/VSHUFB.
var permTable [256][16]uint8

// zeroIndex marks an out-of-range source for the high-bit-zeroing table lookups:
//   - amd64 PSHUFB and loong64 VSHUFB write zero when the index's bit 7 is set;
//   - arm64 VTBL and riscv64 vrgather write zero when the index >= the table
//     length (16 here).
//
// 0xFF satisfies all four.
const zeroIndex = 0xFF

// permZeroIndex points VPERM at byte 0 of the all-zero companion register.
const permZeroIndex = 16

// permTableBE is the VPERM mask for the BIG-ENDIAN s390x target. There the data
// stream is still little-endian (LSB first), but a decoded uint32 is stored
// most-significant byte first: lane L occupies dst memory bytes L*4..L*4+3 with
// L*4+0 the MSB. s390x VL/VST keep memory order in the vector (element 0 = lowest
// address = MSB), so within each lane the value's byte b (b=0 is the LSB) must
// land at output element L*4 + (3-b), and the unused high bytes (lower element
// indices) are zeroed via the companion register. This per-lane reversal is the
// crux of the big-endian handling and is pinned by a position-dependent qemu
// test.
var permTableBE [256][16]uint8

// encodeShuffleTable[key] is the COMPACTION mask used by the SIMD encoder: the
// inverse of shuffleTable. The encode kernel loads the four source uint32 as 16
// bytes (lane L's value occupies vector bytes L*4..L*4+3, little-endian) and
// gathers, for each lane, its low code_L+1 significant bytes contiguously into
// the data stream. Output byte position p (0..lengthTable[key]-1) names the
// source vector byte to pull: lane L, value-byte j -> source index L*4+j. The
// trailing positions (lengthTable[key]..15) are filled with a zeroing index and
// never stored (only lengthTable[key] bytes are written), so their value only has
// to be a legal "produce zero" marker for the lookup primitive — the same
// zeroIndex/permZeroIndex conventions as the decode tables.
var encodeShuffleTable [256][16]uint8

// encodeShuffleTablePerm is the VPERM/vshuf.b variant of encodeShuffleTable for
// targets without a high-bit-zeroes rule (ppc64le little-endian, loong64): the
// trailing don't-care positions point at the all-zero companion register (index
// permZeroIndex) instead of 0xFF.
var encodeShuffleTablePerm [256][16]uint8

// encodeShuffleTablePermBE is the compaction mask for BIG-ENDIAN s390x. There the
// four source uint32 are loaded with VL, so element 0 is lane 0's most-
// significant byte: lane L value-byte j (j=0 is the LSB) sits at vector element
// L*4 + (3-j). The emitted data stream must still be LSB-first contiguous, so
// output position p pulls source element L*4 + (3-j). Trailing don't-care
// positions point at the zero companion register. This per-lane reversal mirrors
// permTableBE and is pinned by a position-dependent qemu test.
var encodeShuffleTablePermBE [256][16]uint8

func init() {
	for key := 0; key < 256; key++ {
		cursor := 0
		var mask, perm, permBE [16]uint8
		var enc, encPerm, encPermBE [16]uint8
		for i := range enc {
			enc[i] = zeroIndex
			encPerm[i] = permZeroIndex
			encPermBE[i] = permZeroIndex
		}
		// Big-endian: zero-fill, then place value bytes LSB->lowest position.
		for i := range permBE {
			permBE[i] = permZeroIndex
		}
		total := 0
		for lane := 0; lane < 4; lane++ {
			code := (key >> (uint(lane) * 2)) & 0x3
			n := code + 1
			total += n
			for j := 0; j < 4; j++ {
				if j < n {
					mask[lane*4+j] = uint8(cursor)
					perm[lane*4+j] = uint8(cursor)
					// byte j (LSB-first) -> big-endian element 3-j of this lane.
					permBE[lane*4+(3-j)] = uint8(cursor)
					// Encode (inverse): output position `cursor` pulls this
					// source byte. LE arches: lane L value-byte j at vector byte
					// L*4+j. BE (s390x VL): at vector element L*4+(3-j).
					enc[cursor] = uint8(lane*4 + j)
					encPerm[cursor] = uint8(lane*4 + j)
					encPermBE[cursor] = uint8(lane*4 + (3 - j))
					cursor++
				} else {
					mask[lane*4+j] = zeroIndex
					perm[lane*4+j] = permZeroIndex
				}
			}
		}
		shuffleTable[key] = mask
		permTable[key] = perm
		permTableBE[key] = permBE
		encodeShuffleTable[key] = enc
		encodeShuffleTablePerm[key] = encPerm
		encodeShuffleTablePermBE[key] = encPermBE
		lengthTable[key] = uint8(total)
	}
}
