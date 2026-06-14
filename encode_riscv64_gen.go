//go:build ignore

// Command gen produces encode_riscv64.s with go-asmgen: the RVV Stream VByte
// compaction (encode) kernel encodeGroupsRVV (V extension, VLEN >= 128) — the
// inverse of decodeGroupsRVV.
//
// VSETVLI fixes VL = 16 bytes (e8, m1). Per group of four uint32:
//   - load the precomputed control byte ctrl[g];
//   - load the 16 source bytes of the group into V1 (VLE8V);
//   - load the 16-byte encodeShuffleTable[ctrl] mask into V2 (VLE8V);
//   - VRGATHERVV V2, V1, V3 — vd=V3, vs2=V1 (source), vs1=V2 (index): result[i] =
//     V1[V2[i]], and 0 whenever V2[i] >= VL (our zeroIndex 0xFF = 255). The mask
//     packs the low code+1 bytes of every lane contiguously and zeroes the
//     trailing don't-care bytes;
//   - store 16 result bytes at the running data cursor (data + dataWritten) with
//     VSE8V. Only lengthTable[ctrl] are significant; the trailing zeros are
//     overwritten by the next group (or left in EncodedMaxLen slack);
//   - advance the data cursor by lengthTable[ctrl] (1..16).
//
// riscv64 is little-endian; lane L's value occupies source bytes L*4..L*4+3 in
// little-endian order and the data stream is little-endian, so no endian fix-up is
// needed. Returns the total data bytes written.
//
// Run: go run encode_riscv64_gen.go
package main

import (
	"fmt"
	"os"

	"github.com/go-asmgen/asmgen/abi"
	"github.com/go-asmgen/asmgen/emit"
	"github.com/go-asmgen/asmgen/riscv64"
)

func main() {
	sig := abi.LayoutArgs(
		[]abi.Arg{
			abi.Scalar("data", abi.Ptr), abi.Scalar("ctrl", abi.Ptr), abi.Scalar("src", abi.Ptr),
			abi.Scalar("groups", abi.Int64),
			abi.Scalar("shuf", abi.Ptr), abi.Scalar("lens", abi.Ptr),
		},
		[]abi.Arg{abi.Scalar("ret", abi.Int64)},
	)
	b := riscv64.NewFunc("encodeGroupsRVV", sig, 0)
	b.LoadArg("data", "X5").
		LoadArg("ctrl", "X6").
		LoadArg("src", "X7").
		LoadArg("groups", "X8").
		LoadArg("shuf", "X9").
		LoadArg("lens", "X10").
		Raw("VSETVLI $16, E8, M1, TA, MA, X11"). // VL = 16 bytes
		Raw("MOV $0, X12").                      // g = 0
		Raw("MOV $0, X13").                      // dataWritten = 0
		Label("loop").
		Raw("BGE X12, X8, done"). // g >= groups -> done
		Raw("ADD X6, X12, X14").
		Raw("MOVBU (X14), X15"). // ctrl byte
		// mask address = shuf + ctrl*16
		Raw("SLLI $4, X15, X16").
		Raw("ADD X9, X16, X16").
		Raw("VLE8V (X16), V2"). // mask
		// source load at src + g*16
		Raw("SLLI $4, X12, X17").
		Raw("ADD X7, X17, X17").
		Raw("VLE8V (X17), V1").
		Raw("VRGATHERVV V2, V1, V3").
		// store at data + dataWritten
		Raw("ADD X5, X13, X18").
		Raw("VSE8V V3, (X18)").
		// advance dataWritten by lengthTable[ctrl]
		Raw("ADD X10, X15, X19").
		Raw("MOVBU (X19), X20").
		Raw("ADD X13, X20, X13").
		Raw("ADD $1, X12, X12").
		Raw("JMP loop").
		Label("done").
		StoreRet("X13", "ret").
		Ret()
	f := emit.NewFile("riscv64")
	f.Add(b.Func())
	if err := os.WriteFile("encode_riscv64.s", []byte(f.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote encode_riscv64.s")
}
