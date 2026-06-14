//go:build ignore

// Command gen produces decode_riscv64.s with go-asmgen: the RVV Stream VByte
// decode kernel decodeGroupsRVV (V extension, VLEN >= 128).
//
// VSETVLI fixes VL = 16 bytes (e8, m1). Per group of four uint32:
//   - load control byte ctrl[g];
//   - load 16 data bytes into V1 (VLE8V);
//   - load the 16-byte shuffleTable[ctrl] mask into V2 (VLE8V);
//   - VRGATHERVV V2, V1, V3 — vd=V3, vs2=V1 (data), vs1=V2 (index): result[i] =
//     V1[V2[i]], and 0 whenever V2[i] >= VL (our zeroIndex 0xFF = 255), which
//     zero-extends each lane;
//   - store 16 result bytes to dst + g*16 (VSE8V);
//   - advance the data cursor by lengthTable[ctrl] (1..16).
//
// riscv64 is little-endian; the packed bytes are a little-endian stream and the
// gather drops each integer's bytes into lane-byte order, so no endian fix-up is
// needed. Returns the total data bytes consumed.
//
// Run: go run decode_riscv64_gen.go
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
			abi.Scalar("dst", abi.Ptr), abi.Scalar("ctrl", abi.Ptr), abi.Scalar("data", abi.Ptr),
			abi.Scalar("groups", abi.Int64),
			abi.Scalar("shuf", abi.Ptr), abi.Scalar("lens", abi.Ptr),
		},
		[]abi.Arg{abi.Scalar("ret", abi.Int64)},
	)
	b := riscv64.NewFunc("decodeGroupsRVV", sig, 0)
	b.LoadArg("dst", "X5").
		LoadArg("ctrl", "X6").
		LoadArg("data", "X7").
		LoadArg("groups", "X8").
		LoadArg("shuf", "X9").
		LoadArg("lens", "X10").
		Raw("VSETVLI $16, E8, M1, TA, MA, X11"). // VL = 16 bytes
		Raw("MOV $0, X12").                      // g = 0
		Raw("MOV $0, X13").                      // dataConsumed = 0
		Label("loop").
		Raw("BGE X12, X8, done"). // g >= groups -> done
		Raw("ADD X6, X12, X14").
		Raw("MOVBU (X14), X15"). // ctrl byte
		// mask address = shuf + ctrl*16
		Raw("SLLI $4, X15, X16").
		Raw("ADD X9, X16, X16").
		Raw("VLE8V (X16), V2"). // mask
		// data load at data + dataConsumed
		Raw("ADD X7, X13, X17").
		Raw("VLE8V (X17), V1").
		Raw("VRGATHERVV V2, V1, V3").
		// store at dst + g*16
		Raw("SLLI $4, X12, X18").
		Raw("ADD X5, X18, X18").
		Raw("VSE8V V3, (X18)").
		// advance dataConsumed by lengthTable[ctrl]
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
	if err := os.WriteFile("decode_riscv64.s", []byte(f.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote decode_riscv64.s")
}
