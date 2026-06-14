//go:build ignore

// Command gen produces decode_s390x.s with go-asmgen: the vector-facility VPERM
// Stream VByte decode kernel decodeGroupsVX for z13+ (baseline, no dispatch).
//
// Per group of four uint32:
//   - load control byte ctrl[g];
//   - VL 16 data bytes into V0. s390x is big-endian: VL puts the lowest memory
//     address into element 0, so a VPERM index equal to a data-stream offset
//     selects that byte directly;
//   - VL the permTableBE[ctrl] mask into V2 (the selector vector);
//   - VPERM V0, V1, V2, V3 — Va=V0 (data bytes, indices 0..15), Vb=V1 (the
//     all-zero register from VZERO, indices 16..31), Vc=V2 (selector), Vd=V3:
//     result element i = pair[V2[i] & 0x1F]. permTableBE places each value byte
//     (LSB-first in the stream) at the big-endian element that makes the stored
//     uint32 most-significant-byte-first, and points unused high bytes at the
//     zero register (index 16);
//   - VST V3 to the dst cursor (element 0 -> lowest address, the inverse of VL);
//   - advance the data cursor by lengthTable[ctrl] (1..16).
//
// REGISTERS: s390x reserves R10..R15 (REGTMP, REGTMP2, REGCTXT, G, LR, SP), so
// this kernel uses only R1..R9 and bumps cursor pointers (dst, ctrl, data) rather
// than recomputing g*16 each iteration, which keeps it within nine GPRs.
//
// BIG-ENDIAN: the only big-endian target. The per-lane byte reversal lives in
// permTableBE (see tables.go), so the kernel itself is a straight permute; the
// element/byte order produced here is pinned by a position-dependent qemu test.
// Returns the total data bytes consumed.
//
// Run: go run decode_s390x_gen.go
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
			abi.Scalar("dst", abi.Ptr), abi.Scalar("ctrl", abi.Ptr), abi.Scalar("data", abi.Ptr),
			abi.Scalar("groups", abi.Int64),
			abi.Scalar("perm", abi.Ptr), abi.Scalar("lens", abi.Ptr),
		},
		[]abi.Arg{abi.Scalar("ret", abi.Int64)},
	)
	b := s390x.NewFunc("decodeGroupsVX", sig, 0)
	b.LoadArg("dst", "R1"). // dst cursor
				LoadArg("ctrl", "R2"). // ctrl cursor
				LoadArg("data", "R3"). // data cursor
				LoadArg("groups", "R4").
				LoadArg("perm", "R5").
				LoadArg("lens", "R6").
				Raw("MOVD $0, R7").    // dataConsumed (return value)
				Raw("ADD R2, R4, R4"). // R4 = ctrl end = ctrl + groups
				Raw("VZERO V1").       // V1 = all zeros (companion for zeroing)
				Label("loop").
				Raw("CMPBGE R2, R4, done"). // ctrl cursor >= end -> done
				Raw("MOVBZ (R2), R9").      // ctrl byte
		// mask address = perm + ctrl*16
		Raw("SLD $4, R9, R8").
		Raw("ADD R5, R8, R8").
		Raw("VL (R8), V2"). // selector
		// data load at the data cursor
		Raw("VL (R3), V0"). // 16 data bytes
		Raw("VPERM V0, V1, V2, V3").
		Raw("VST V3, (R1)"). // store four uint32 at dst cursor
		// advance data cursor and dataConsumed by lengthTable[ctrl]
		Raw("ADD R6, R9, R8").
		Raw("MOVBZ (R8), R9"). // length (1..16)
		Raw("ADD R9, R3, R3"). // data cursor += len
		Raw("ADD R9, R7, R7"). // dataConsumed += len
		// advance dst cursor (16 bytes) and ctrl cursor (1 byte)
		Raw("ADD $16, R1, R1").
		Raw("ADD $1, R2, R2").
		Raw("BR loop").
		Label("done").
		StoreRet("R7", "ret").
		Ret()
	f := emit.NewFile("s390x")
	f.Add(b.Func())
	if err := os.WriteFile("decode_s390x.s", []byte(f.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote decode_s390x.s")
}
