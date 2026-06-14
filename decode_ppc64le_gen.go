//go:build ignore

// Command gen produces decode_ppc64le.s with go-asmgen: the VMX VPERM Stream
// VByte decode kernel decodeGroupsVSX for POWER8+ (VSX/VMX baseline, no runtime
// dispatch).
//
// Per group of four uint32:
//   - load control byte ctrl[g];
//   - LXVB16X 16 data bytes into VS32 (= V0). LXVB16X is a byte-order-stable load:
//     memory byte k lands in vector element k (big-endian element numbering), so a
//     VPERM index equal to a memory offset selects that byte directly;
//   - LXVB16X the permTable[ctrl] mask into VS33 (= V1);
//   - VPERM V0, V2, V1, V3 — Vd=V3, indexes the 32-byte pair (V0 = data bytes
//     0..15, V2 = the all-zero register, bytes 16..31): result byte i =
//     pair[V1[i] & 0x1F]. A mask byte of 16 (permZeroIndex) selects V2 byte 0 = 0,
//     zero-extending each lane;
//   - STXVB16X the result (VS35 = V3) to dst + g*16 (byte-order-stable store, the
//     inverse of LXVB16X);
//   - advance the data cursor by lengthTable[ctrl] (1..16).
//
// VSX↔VMX aliasing: AltiVec register Vn is the SAME physical register as VS(32+n)
// (NOT VSn). LXVB16X/STXVB16X name VS registers; VPERM/VSPLTISB name V registers.
// We load into VS32/VS33 and refer to them as V0/V1, build the zero register with
// VSPLTISB $0, V2, and store from V3 via VS35.
//
// ENDIANNESS: ppc64le is little-endian, but LXVB16X/STXVB16X make the in-register
// byte numbering match memory order, and the permTable is built in that same
// order, so the kernel is endian-clean. This LXVB16X choice (vs LXVD2X) is what
// keeps the index == memory-offset identity; it is pinned by a position-dependent
// qemu test. Returns the total data bytes consumed.
//
// Run: go run decode_ppc64le_gen.go
package main

import (
	"fmt"
	"os"

	"github.com/go-asmgen/asmgen/abi"
	"github.com/go-asmgen/asmgen/emit"
	"github.com/go-asmgen/asmgen/ppc64"
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
	b := ppc64.NewFunc("decodeGroupsVSX", sig, 0)
	b.LoadArg("dst", "R3").
		LoadArg("ctrl", "R4").
		LoadArg("data", "R5").
		LoadArg("groups", "R6").
		LoadArg("perm", "R7").
		LoadArg("lens", "R8").
		Raw("MOVD $0, R9").     // g = 0
		Raw("MOVD $0, R10").    // dataConsumed = 0
		Raw("VSPLTISB $0, V2"). // V2 = all zeros (companion for zeroing)
		Label("loop").
		Raw("CMP R9, R6").
		Raw("BGE done"). // g >= groups -> done
		Raw("ADD R4, R9, R11").
		Raw("MOVBZ (R11), R12"). // ctrl byte
		// mask address = perm + ctrl*16
		Raw("SLD $4, R12, R14").
		Raw("ADD R7, R14, R14").
		Raw("LXVB16X (R14), VS33"). // V1 = mask
		// data load at data + dataConsumed
		Raw("ADD R5, R10, R15").
		Raw("LXVB16X (R15), VS32").  // V0 = 16 data bytes
		Raw("VPERM V0, V2, V1, V3"). // V3 = gathered (zeros via V2)
		// store at dst + g*16
		Raw("SLD $4, R9, R16").
		Raw("ADD R3, R16, R16").
		Raw("STXVB16X VS35, (R16)"). // V3 -> dst
		// advance dataConsumed by lengthTable[ctrl]
		Raw("ADD R8, R12, R17").
		Raw("MOVBZ (R17), R18").
		Raw("ADD R10, R18, R10").
		Raw("ADD $1, R9, R9").
		Raw("BR loop").
		Label("done").
		StoreRet("R10", "ret").
		Ret()
	f := emit.NewFile("ppc64le")
	f.Add(b.Func())
	if err := os.WriteFile("decode_ppc64le.s", []byte(f.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote decode_ppc64le.s")
}
