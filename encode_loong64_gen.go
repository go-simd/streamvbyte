//go:build ignore

// Command gen produces encode_loong64.s with go-asmgen: the LSX Stream VByte
// compaction (encode) kernel encodeGroupsLSX — the inverse of decodeGroupsLSX.
//
// Per group of four uint32:
//   - load the precomputed control byte ctrl[g];
//   - load the 16 source bytes of the group into V0 (VMOVQ);
//   - load the 16-byte encodeShuffleTablePerm[ctrl] mask into V1 (VMOVQ);
//   - VSHUFB V1, V0, V3, V2 — vshuf.b vd=V2, va=V1 (index/selector), vk=V0
//     (source, selected by indices 0..15), vj=V3 (the all-zero register, selected
//     by indices 16..31). Go operand order is (va, vk, vj, vd): result[i] =
//     src[V1[i]] for V1[i] in 0..15, and 0 when V1[i] == 16 (permZeroIndex -> zero
//     register). The mask packs the low code+1 bytes of every lane contiguously
//     and zeroes the trailing don't-care bytes. vshuf.b has no high-bit-zeroes
//     rule, so a zero companion register is required (verified by a qemu probe);
//   - store 16 result bytes at the running data cursor (data + dataWritten) with
//     VMOVQ. Only lengthTable[ctrl] are significant; the trailing zeros are
//     overwritten by the next group (or left in EncodedMaxLen slack);
//   - advance the data cursor by lengthTable[ctrl] (1..16).
//
// loong64 is little-endian; lane L's value occupies source bytes L*4..L*4+3 in
// little-endian order and the data stream is little-endian, so no endian fix-up is
// needed. The exact VSHUFB source-operand order is pinned by a qemu test. Returns
// the total data bytes written.
//
// Run: go run encode_loong64_gen.go
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
			abi.Scalar("data", abi.Ptr), abi.Scalar("ctrl", abi.Ptr), abi.Scalar("src", abi.Ptr),
			abi.Scalar("groups", abi.Int64),
			abi.Scalar("perm", abi.Ptr), abi.Scalar("lens", abi.Ptr),
		},
		[]abi.Arg{abi.Scalar("ret", abi.Int64)},
	)
	b := loong64.NewFunc("encodeGroupsLSX", sig, 0)
	b.LoadArg("data", "R4").
		LoadArg("ctrl", "R5").
		LoadArg("src", "R6").
		LoadArg("groups", "R7").
		LoadArg("perm", "R8").
		LoadArg("lens", "R9").
		Raw("MOVV $0, R10").     // g = 0
		Raw("MOVV $0, R11").     // dataWritten = 0
		Raw("VXORV V3, V3, V3"). // V3 = all zeros (companion for zeroing)
		Label("loop").
		Raw("BGE R10, R7, done"). // g >= groups -> done
		Raw("ADDVU R5, R10, R12").
		Raw("MOVBU (R12), R13"). // ctrl byte
		// mask address = perm + ctrl*16
		Raw("SLLV $4, R13, R14").
		Raw("ADDVU R8, R14, R14").
		Raw("VMOVQ (R14), V1"). // mask
		// source load at src + g*16
		Raw("SLLV $4, R10, R15").
		Raw("ADDVU R6, R15, R15").
		Raw("VMOVQ (R15), V0"). // 4 uint32
		Raw("VSHUFB V1, V0, V3, V2").
		// store at data + dataWritten
		Raw("ADDVU R4, R11, R16").
		Raw("VMOVQ V2, (R16)").
		// advance dataWritten by lengthTable[ctrl]
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
	if err := os.WriteFile("encode_loong64.s", []byte(f.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote encode_loong64.s")
}
