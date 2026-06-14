//go:build ignore

// Command gen produces encode_arm64.s with go-asmgen: the NEON Stream VByte
// compaction (encode) kernel encodeGroupsNEON — the inverse of decodeGroupsNEON.
//
// Per group of four uint32:
//   - load the precomputed control byte ctrl[g];
//   - form the mask address shuf + ctrl*16 and load 16 mask bytes (V1);
//   - load the 16 source bytes of the group from src + g*16 (V0);
//   - VTBL V1.B16, [V0.B16], V2.B16 — gather: result byte i = V0[V1[i]], and 0
//     whenever V1[i] >= 16 (our zeroIndex 0xFF). The mask packs the low code+1
//     bytes of every lane contiguously and zeroes the trailing don't-care bytes;
//   - VST1 the 16 result bytes at the running data cursor (data + dataWritten).
//     Only lengthTable[ctrl] are significant; the trailing zeros are overwritten
//     by the next group (or left in EncodedMaxLen slack);
//   - advance the data cursor by lengthTable[ctrl] (1..16).
//
// arm64 is little-endian; lane L's value occupies source bytes L*4..L*4+3 in
// little-endian order and the data stream is little-endian, so no endian fix-up is
// needed. Returns the total data bytes written.
//
// Run: go run encode_arm64_gen.go
package main

import (
	"fmt"
	"os"

	"github.com/go-asmgen/asmgen/abi"
	"github.com/go-asmgen/asmgen/arm64"
	"github.com/go-asmgen/asmgen/emit"
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
	b := arm64.NewFunc("encodeGroupsNEON", sig, 0)
	b.LoadArg("data", "R0").
		LoadArg("ctrl", "R1").
		LoadArg("src", "R2").
		LoadArg("groups", "R3").
		LoadArg("shuf", "R4").
		LoadArg("lens", "R5").
		Raw("MOVD $0, R6"). // g = 0
		Raw("MOVD $0, R7"). // dataWritten = 0
		Label("loop").
		Raw("CMP R3, R6").
		Raw("BGE done").           // g >= groups -> done
		Raw("MOVBU (R1)(R6), R8"). // ctrl byte
		// mask address = shuf + ctrl*16
		Raw("LSL $4, R8, R9").
		Raw("ADD R4, R9, R9").
		Raw("VLD1 (R9), [V1.B16]"). // shuffle mask
		// source load at src + g*16
		Raw("LSL $4, R6, R10").
		Raw("ADD R2, R10, R10").
		Raw("VLD1 (R10), [V0.B16]"). // 4 uint32
		Raw("VTBL V1.B16, [V0.B16], V2.B16").
		// store 16 result bytes at data + dataWritten
		Raw("ADD R0, R7, R11").
		Raw("VST1 [V2.B16], (R11)").
		// advance dataWritten by lengthTable[ctrl]
		Raw("MOVBU (R5)(R8), R12").
		Raw("ADD R12, R7, R7").
		Raw("ADD $1, R6, R6").
		Raw("JMP loop").
		Label("done").
		StoreRet("R7", "ret").
		Ret()
	f := emit.NewFile("arm64")
	f.Add(b.Func())
	if err := os.WriteFile("encode_arm64.s", []byte(f.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote encode_arm64.s")
}
