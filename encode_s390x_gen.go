//go:build ignore

// Command gen produces encode_s390x.s with go-asmgen: the vector-facility VPERM
// Stream VByte compaction (encode) kernel encodeGroupsVX for z13+ (baseline, no
// dispatch) — the inverse of decodeGroupsVX.
//
// Per group of four uint32:
//   - load the precomputed control byte ctrl[g];
//   - VL the 16 source bytes of the group into V0. s390x is big-endian: VL puts
//     the lowest memory address into element 0, so a source uint32 is laid out
//     MSB-first — lane L's value-byte j (j=0 is the LSB) sits at element L*4+(3-j);
//   - VL the encodeShuffleTablePermBE[ctrl] mask into V2 (the selector vector);
//   - VPERM V0, V1, V2, V3 — Va=V0 (source bytes, indices 0..15), Vb=V1 (the
//     all-zero register from VZERO, indices 16..31), Vc=V2 (selector), Vd=V3:
//     result element i = pair[V2[i] & 0x1F]. encodeShuffleTablePermBE pulls each
//     lane's value bytes LSB-first from element L*4+(3-j), packing them
//     contiguously into a little-endian data stream, and points the unused
//     trailing positions at the zero register (index 16);
//   - VST V3 at the running data cursor (element 0 -> lowest address, the inverse
//     of VL). Only lengthTable[ctrl] bytes are significant; the trailing zeros are
//     overwritten by the next group (or left in EncodedMaxLen slack);
//   - advance the data cursor by lengthTable[ctrl] (1..16) and the source cursor
//     by 16.
//
// REGISTERS: s390x reserves R10..R15, so this kernel uses only R1..R9 and bumps
// cursor pointers (data, ctrl, src) rather than recomputing g*16 each iteration.
//
// BIG-ENDIAN: the only big-endian target. The per-lane byte reversal lives in
// encodeShuffleTablePermBE (see tables.go), the inverse of the decode permTableBE;
// the element/byte order is pinned by a position-dependent qemu test. Returns the
// total data bytes written.
//
// Run: go run encode_s390x_gen.go
package main

import (
	"fmt"
	"os"

	"github.com/go-asmgen/asmgen/abi"
	"github.com/go-asmgen/asmgen/emit"
	"github.com/go-asmgen/asmgen/s390x"
)

func main() {
	sig := abi.LayoutArgs(
		[]abi.Arg{
			abi.Scalar("data", abi.Ptr), abi.Scalar("ctrl", abi.Ptr), abi.Scalar("src", abi.Ptr),
			abi.Scalar("groups", abi.Int64),
			abi.Scalar("perm", abi.Ptr), abi.Scalar("lens", abi.Ptr),
		},
		[]abi.Arg{abi.Scalar("ret", abi.Int64)},
	)
	b := s390x.NewFunc("encodeGroupsVX", sig, 0)
	b.LoadArg("data", "R1"). // data cursor
					LoadArg("ctrl", "R2"). // ctrl cursor
					LoadArg("src", "R3").  // src cursor
					LoadArg("groups", "R4").
					LoadArg("perm", "R5").
					LoadArg("lens", "R6").
					Raw("MOVD $0, R7").    // dataWritten (return value)
					Raw("ADD R2, R4, R4"). // R4 = ctrl end = ctrl + groups
					Raw("VZERO V1").       // V1 = all zeros (companion for zeroing)
					Label("loop").
					Raw("CMPBGE R2, R4, done"). // ctrl cursor >= end -> done
					Raw("MOVBZ (R2), R9").      // ctrl byte
		// mask address = perm + ctrl*16
		Raw("SLD $4, R9, R8").
		Raw("ADD R5, R8, R8").
		Raw("VL (R8), V2"). // selector
		// source load at the src cursor
		Raw("VL (R3), V0"). // 4 uint32
		Raw("VPERM V0, V1, V2, V3").
		Raw("VST V3, (R1)"). // store compacted bytes at data cursor
		// advance data cursor and dataWritten by lengthTable[ctrl]
		Raw("ADD R6, R9, R8").
		Raw("MOVBZ (R8), R9"). // length (1..16)
		Raw("ADD R9, R1, R1"). // data cursor += len
		Raw("ADD R9, R7, R7"). // dataWritten += len
		// advance src cursor (16 bytes) and ctrl cursor (1 byte)
		Raw("ADD $16, R3, R3").
		Raw("ADD $1, R2, R2").
		Raw("BR loop").
		Label("done").
		StoreRet("R7", "ret").
		Ret()
	f := emit.NewFile("s390x")
	f.Add(b.Func())
	if err := os.WriteFile("encode_s390x.s", []byte(f.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote encode_s390x.s")
}
