//go:build ignore

// Command gen produces decode_loong64.s with go-asmgen: the LSX Stream VByte
// decode kernel decodeGroupsLSX.
//
// Per group of four uint32:
//   - load control byte ctrl[g];
//   - load 16 data bytes into V0 (VMOVQ);
//   - load the 16-byte permTable[ctrl] mask into V1 (VMOVQ);
//   - VSHUFB V1, V0, V3, V2 — vshuf.b vd=V2, va=V1 (index/selector), vk=V0 (data,
//     selected by indices 0..15), vj=V3 (the all-zero register, selected by
//     indices 16..31). Go operand order is (va, vk, vj, vd): the index/selector
//     is the FIRST source (OP_RRRR field r1). result[i] = data[V1[i]] for V1[i]
//     in 0..15, and 0 when V1[i] == 16 (permZeroIndex -> zero register), which
//     zero-extends each lane. vshuf.b does NOT zero on a high index bit, so a
//     zero companion register is required (verified by a qemu probe).
//   - store 16 result bytes to dst + g*16 (VMOVQ);
//   - advance the data cursor by lengthTable[ctrl] (1..16).
//
// loong64 is little-endian; the packed bytes are a little-endian stream and the
// shuffle drops each integer's bytes into lane-byte order, so no endian fix-up is
// needed. The exact VSHUFB source-operand order is pinned by a qemu test.
// Returns the total data bytes consumed.
//
// Run: go run decode_loong64_gen.go
package main

import (
	"fmt"
	"os"

	"github.com/go-asmgen/asmgen/abi"
	"github.com/go-asmgen/asmgen/emit"
	"github.com/go-asmgen/asmgen/loong64"
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
	b := loong64.NewFunc("decodeGroupsLSX", sig, 0)
	b.LoadArg("dst", "R4").
		LoadArg("ctrl", "R5").
		LoadArg("data", "R6").
		LoadArg("groups", "R7").
		LoadArg("perm", "R8").
		LoadArg("lens", "R9").
		Raw("MOVV $0, R10").     // g = 0
		Raw("MOVV $0, R11").     // dataConsumed = 0
		Raw("VXORV V3, V3, V3"). // V3 = all zeros (companion for zeroing)
		Label("loop").
		Raw("BGE R10, R7, done"). // g >= groups -> done
		Raw("ADDVU R5, R10, R12").
		Raw("MOVBU (R12), R13"). // ctrl byte
		// mask address = shuf + ctrl*16
		Raw("SLLV $4, R13, R14").
		Raw("ADDVU R8, R14, R14").
		Raw("VMOVQ (R14), V1"). // mask
		// data load at data + dataConsumed
		Raw("ADDVU R6, R11, R15").
		Raw("VMOVQ (R15), V0"). // 16 data bytes
		Raw("VSHUFB V1, V0, V3, V2").
		// store at dst + g*16
		Raw("SLLV $4, R10, R16").
		Raw("ADDVU R4, R16, R16").
		Raw("VMOVQ V2, (R16)").
		// advance dataConsumed by lengthTable[ctrl]
		Raw("ADDVU R9, R13, R17").
		Raw("MOVBU (R17), R18").
		Raw("ADDVU R11, R18, R11").
		Raw("ADDVU $1, R10, R10").
		Raw("JMP loop").
		Label("done").
		StoreRet("R11", "ret").
		Ret()
	f := emit.NewFile("loong64")
	f.Add(b.Func())
	if err := os.WriteFile("decode_loong64.s", []byte(f.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote decode_loong64.s")
}
