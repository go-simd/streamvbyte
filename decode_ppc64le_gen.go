//go:build ignore

// Command gen produces decode_ppc64le.s with go-asmgen: the VMX VPERM Stream
// VByte decode kernel decodeGroupsVSX. It is built from ISA-2.07 (POWER8-baseline)
// ops only, so it runs natively on POWER8 (no POWER9 gate). The element-order
// vector loads/stores a byte-order-stable codec wants would be the ISA-3.0
// LXVB16X/STXVB16X; those are emitted instead as an ISA-2.07 LXVD2X/STXVD2X plus
// one VPERM against a fixed byte-reversal control vrev (see emitLoadB16/
// emitStoreB16). vrev is bootstrapped by a plain LXVD2X of {0..15}: its scrambled
// load layout is exactly the involution VPERM needs to reproduce LXVB16X element
// order. Verified on cfarm433 (POWER9) and cfarm112 (POWER8E).
//
// Per group of four uint32:
//   - load control byte ctrl[g];
//   - load 16 data bytes into V0 (LXVB16X-equivalent: element k == memory byte k);
//   - load the permTable[ctrl] mask into V1;
//   - VPERM V0, V2, V1, V3 — Vd=V3, indexes the 32-byte pair (V0 = data bytes
//     0..15, V2 = the all-zero register, bytes 16..31): result byte i =
//     pair[V1[i] & 0x1F]. A mask byte of 16 (permZeroIndex) selects V2 byte 0 = 0,
//     zero-extending each lane;
//   - store the result (V3) to dst + g*16 (the inverse, STXVB16X-equivalent);
//   - advance the data cursor by lengthTable[ctrl] (1..16).
//
// VSX↔VMX aliasing: AltiVec register Vn is the SAME physical register as VS(32+n).
// vrev lives in V31 (VS63); the store scratch is V30 (VS62).
//
// ENDIANNESS: ppc64le is little-endian; the LXVD2X+VPERM(vrev) load makes the
// in-register byte numbering match memory order (element k == memory byte k),
// exactly as the old LXVB16X did, and the permTable is built in that same order,
// so the kernel is endian-clean. Pinned by a position-dependent qemu test plus
// the cfarm112/cfarm433 differential runs. Returns the total data bytes consumed.
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

const vrevVS = "VS63"
const vrevV = "V31"

// emitLoadB16 emits the ISA-2.07 equivalent of "LXVB16X (addrExpr), vs" (v is the
// aliased Vnn name): LXVD2X then VPERM v,v,vrev.
func emitLoadB16(bld *ppc64.Builder, addrExpr, vs, v string) {
	bld.Raw("LXVD2X %s, %s", addrExpr, vs).
		Raw("VPERM %s, %s, %s, %s", v, v, vrevV, v)
}

// emitStoreB16 emits the ISA-2.07 equivalent of "STXVB16X vs, (addrExpr)" via the
// V30/VS62 scratch; v is left clobbered.
func emitStoreB16(bld *ppc64.Builder, vs, v, addrExpr string) {
	bld.Raw("VPERM %s, %s, %s, V30", v, v, vrevV).
		Raw("STXVD2X VS62, %s", addrExpr)
}

func main() {
	sig := abi.LayoutArgs(
		[]abi.Arg{
			abi.Scalar("dst", abi.Ptr), abi.Scalar("ctrl", abi.Ptr), abi.Scalar("data", abi.Ptr),
			abi.Scalar("groups", abi.Int64),
			abi.Scalar("perm", abi.Ptr), abi.Scalar("lens", abi.Ptr),
		},
		[]abi.Arg{abi.Scalar("ret", abi.Int64)},
	)
	f := emit.NewFile("ppc64le")
	// Byte-reversal control for the LXVB16X/STXVB16X emulation (loaded via plain
	// LXVD2X into the involution layout VPERM needs).
	rev := f.Data("svbrev", []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15})

	b := ppc64.NewFunc("decodeGroupsVSX", sig, 0)
	bld := b.LoadArg("dst", "R3").
		LoadArg("ctrl", "R4").
		LoadArg("data", "R5").
		LoadArg("groups", "R6").
		LoadArg("perm", "R7").
		LoadArg("lens", "R8").
		Raw("MOVD $0, R9").     // g = 0
		Raw("MOVD $0, R10").    // dataConsumed = 0
		Raw("VSPLTISB $0, V2"). // V2 = all zeros (companion for zeroing)
		// Byte-reversal control vrev in V31 (plain LXVD2X of {0..15}); enables the
		// ISA-2.07 LXVB16X/STXVB16X emulation.
		Raw("MOVD $%s(SB), R11", rev).Raw("LXVD2X (R11)(R0), %s", vrevVS).
		Label("loop").
		Raw("CMP R9, R6").
		Raw("BGE done"). // g >= groups -> done
		Raw("ADD R4, R9, R11").
		Raw("MOVBZ (R11), R12"). // ctrl byte
		// mask address = perm + ctrl*16
		Raw("SLD $4, R12, R14").
		Raw("ADD R7, R14, R14")
	emitLoadB16(bld, "(R14)(R0)", "VS33", "V1") // V1 = mask
	// data load at data + dataConsumed
	bld.Raw("ADD R5, R10, R15")
	emitLoadB16(bld, "(R15)(R0)", "VS32", "V0") // V0 = 16 data bytes
	bld.Raw("VPERM V0, V2, V1, V3").            // V3 = gathered (zeros via V2)
		// store at dst + g*16
		Raw("SLD $4, R9, R16").
		Raw("ADD R3, R16, R16")
	emitStoreB16(bld, "VS35", "V3", "(R16)(R0)") // V3 -> dst
	// advance dataConsumed by lengthTable[ctrl]
	bld.Raw("ADD R8, R12, R17").
		Raw("MOVBZ (R17), R18").
		Raw("ADD R10, R18, R10").
		Raw("ADD $1, R9, R9").
		Raw("BR loop").
		Label("done").
		StoreRet("R10", "ret").
		Ret()
	f.Add(b.Func())
	if err := os.WriteFile("decode_ppc64le.s", []byte(f.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote decode_ppc64le.s")
}
