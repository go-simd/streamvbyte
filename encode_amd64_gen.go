//go:build ignore

// Command gen produces encode_amd64.s with go-asmgen: the SSSE3 Stream VByte
// compaction (encode) kernel encodeGroupsSSE — the inverse of decodeGroupsSSE.
//
// Per group of four uint32:
//   - load the precomputed control byte ctrl[g];
//   - load the 16 source bytes of the group (four uint32) from src + g*16 into X0;
//   - load the 16-byte mask encodeShuffleTable[ctrl] (address shuf + ctrl*16) into
//     X1;
//   - PSHUFB X1, X0 — result byte i = X0[X1[i] & 0x0F], or 0 when X1[i] has bit 7
//     set (our zeroIndex 0xFF). The mask names, for each output position, the
//     source byte (lane*4 + value-byte) to keep, packing the low code+1 bytes of
//     every lane contiguously and zeroing the trailing don't-care bytes;
//   - store the 16 result bytes at the running data cursor (data + dataWritten).
//     Only lengthTable[ctrl] of them are significant; the trailing zeros are
//     overwritten by the next group's store (or left in EncodedMaxLen slack);
//   - advance the data cursor by lengthTable[ctrl] (1..16).
//
// amd64 is little-endian; lane L's value occupies source bytes L*4..L*4+3 in
// little-endian order, and the compacted data stream is little-endian, so no
// endian fix-up is needed. Returns the total data bytes written.
//
// Run: go run encode_amd64_gen.go
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
			abi.Scalar("data", abi.Ptr), abi.Scalar("ctrl", abi.Ptr), abi.Scalar("src", abi.Ptr),
			abi.Scalar("groups", abi.Int64),
			abi.Scalar("shuf", abi.Ptr), abi.Scalar("lens", abi.Ptr),
		},
		[]abi.Arg{abi.Scalar("ret", abi.Int64)},
	)
	b := amd64.NewFunc("encodeGroupsSSE", sig, 0)
	b.LoadArg("data", "DI").
		LoadArg("ctrl", "SI").
		LoadArg("src", "DX").
		LoadArg("groups", "CX").
		LoadArg("shuf", "R8").
		LoadArg("lens", "R9").
		Raw("XORQ R10, R10"). // g = 0
		Raw("XORQ R11, R11"). // dataWritten = 0
		Label("loop").
		Raw("CMPQ R10, CX").
		Raw("JGE done").
		Raw("MOVBLZX (SI)(R10*1), R12"). // ctrl byte
		// mask address = shuf + ctrl*16
		Raw("MOVQ R12, R13").
		Raw("SHLQ $4, R13").
		Raw("ADDQ R8, R13").
		Raw("MOVOU (R13), X1"). // mask
		// source load at src + g*16
		Raw("MOVQ R10, R14").
		Raw("SHLQ $4, R14").
		Raw("MOVOU (DX)(R14*1), X0"). // 4 uint32
		Raw("PSHUFB X1, X0").
		// store at data + dataWritten
		Raw("MOVOU X0, (DI)(R11*1)").
		// advance dataWritten by lengthTable[ctrl]
		Raw("MOVBLZX (R9)(R12*1), R15").
		Raw("ADDQ R15, R11").
		Raw("INCQ R10").
		Raw("JMP loop").
		Label("done").
		StoreRet("R11", "ret").
		Ret()
	f := emit.NewFile("amd64")
	f.Add(b.Func())
	if err := os.WriteFile("encode_amd64.s", []byte(f.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote encode_amd64.s")
}
