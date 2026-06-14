//go:build ignore

// Command gen produces decode_amd64.s with go-asmgen: the SSSE3 Stream VByte
// decode kernel decodeGroupsSSE.
//
// Per group of four uint32:
//   - load control byte ctrl[g];
//   - load 16 data bytes from the data cursor into X0;
//   - load the 16-byte mask shuffleTable[ctrl] (address shuf + ctrl*16) into X1;
//   - PSHUFB X1, X0 — result byte i = X0[X1[i] & 0x0F], or 0 when X1[i] has bit 7
//     set (our zeroIndex 0xFF), which zero-extends each lane;
//   - store the 16 result bytes to dst + g*16;
//   - advance the data cursor by lengthTable[ctrl] (1..16).
//
// amd64 is little-endian; the packed data is a little-endian byte stream and the
// mask drops each integer's bytes into lane-byte order, so no endian fix-up is
// needed. Returns the total data bytes consumed.
//
// Run: go run decode_amd64_gen.go
package main

import (
	"fmt"
	"os"

	"github.com/go-asmgen/asmgen/abi"
	"github.com/go-asmgen/asmgen/amd64"
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
	b := amd64.NewFunc("decodeGroupsSSE", sig, 0)
	b.LoadArg("dst", "DI").
		LoadArg("ctrl", "SI").
		LoadArg("data", "DX").
		LoadArg("groups", "CX").
		LoadArg("shuf", "R8").
		LoadArg("lens", "R9").
		Raw("XORQ R10, R10"). // g = 0
		Raw("XORQ R11, R11"). // dataConsumed = 0
		Label("loop").
		Raw("CMPQ R10, CX").
		Raw("JGE done").
		Raw("MOVBLZX (SI)(R10*1), R12"). // ctrl byte
		// mask address = shuf + ctrl*16
		Raw("MOVQ R12, R13").
		Raw("SHLQ $4, R13").
		Raw("ADDQ R8, R13").
		Raw("MOVOU (R13), X1"). // mask
		// data load at data + dataConsumed
		Raw("MOVOU (DX)(R11*1), X0").
		Raw("PSHUFB X1, X0").
		// store at dst + g*16
		Raw("MOVQ R10, R14").
		Raw("SHLQ $4, R14").
		Raw("MOVOU X0, (DI)(R14*1)").
		// advance dataConsumed by lengthTable[ctrl]
		Raw("MOVBLZX (R9)(R12*1), R15").
		Raw("ADDQ R15, R11").
		Raw("INCQ R10").
		Raw("JMP loop").
		Label("done").
		StoreRet("R11", "ret").
		Ret()
	f := emit.NewFile("amd64")
	f.Add(b.Func())
	if err := os.WriteFile("decode_amd64.s", []byte(f.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote decode_amd64.s")
}
