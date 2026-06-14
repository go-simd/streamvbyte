//go:build ignore

// Command gen produces decode_arm64.s with go-asmgen: the NEON Stream VByte
// decode kernel decodeGroupsNEON.
//
// Per group of four uint32:
//   - load the control byte ctrl[g];
//   - form the shuffle-mask address shuf + ctrl*16 and load 16 mask bytes (V1);
//   - load 16 data bytes from the data cursor (V0);
//   - VTBL V1.B16, [V0.B16], V2.B16 — gather: result byte i = V0[V1[i]], and 0
//     whenever V1[i] >= 16 (our zeroIndex 0xFF), which zero-extends each lane;
//   - VST1 the 16 result bytes to dst;
//   - advance the data cursor by lengthTable[ctrl] (1..16).
//
// arm64 is little-endian; the packed bytes are a little-endian stream and the
// shuffle places byte j of an integer into lane-byte j, so no endian fix-up is
// needed. Returns the total data bytes consumed.
//
// Run: go run decode_arm64_gen.go
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
			abi.Scalar("dst", abi.Ptr), abi.Scalar("ctrl", abi.Ptr), abi.Scalar("data", abi.Ptr),
			abi.Scalar("groups", abi.Int64),
			abi.Scalar("shuf", abi.Ptr), abi.Scalar("lens", abi.Ptr),
		},
		[]abi.Arg{abi.Scalar("ret", abi.Int64)},
	)
	b := arm64.NewFunc("decodeGroupsNEON", sig, 0)
	b.LoadArg("dst", "R0").
		LoadArg("ctrl", "R1").
		LoadArg("data", "R2").
		LoadArg("groups", "R3").
		LoadArg("shuf", "R4").
		LoadArg("lens", "R5").
		Raw("MOVD $0, R6"). // g = 0
		Raw("MOVD $0, R7"). // dataConsumed = 0
		Label("loop").
		Raw("CMP R3, R6").
		Raw("BGE done").           // g >= groups -> done
		Raw("MOVBU (R1)(R6), R8"). // ctrl byte
		// mask address = shuf + ctrl*16
		Raw("LSL $4, R8, R9").
		Raw("ADD R4, R9, R9").
		Raw("VLD1 (R9), [V1.B16]"). // shuffle mask
		// data load at data + dataConsumed
		Raw("ADD R2, R7, R10").
		Raw("VLD1 (R10), [V0.B16]"). // 16 data bytes
		Raw("VTBL V1.B16, [V0.B16], V2.B16").
		// store 16 result bytes at dst + g*16
		Raw("LSL $4, R6, R11").
		Raw("ADD R0, R11, R11").
		Raw("VST1 [V2.B16], (R11)").
		// advance dataConsumed by lengthTable[ctrl]
		Raw("MOVBU (R5)(R8), R12").
		Raw("ADD R12, R7, R7").
		Raw("ADD $1, R6, R6").
		Raw("JMP loop").
		Label("done").
		StoreRet("R7", "ret").
		Ret()
	f := emit.NewFile("arm64")
	f.Add(b.Func())
	if err := os.WriteFile("decode_arm64.s", []byte(f.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote decode_arm64.s")
}
