//go:build ignore

// Command gen produces encode_ppc64le.s with go-asmgen: the VMX VPERM Stream
// VByte compaction (encode) kernel encodeGroupsVSX — the inverse of
// decodeGroupsVSX. The kernel uses ISA-3.0 (POWER9) instructions
// (LXVB16X/STXVB16X), which raise SIGILL on POWER8, so the dispatcher gates it on
// cpu.PPC64.IsPOWER9 and falls back to the scalar codec (encodeTail) on POWER8.
//
// Per group of four uint32:
//   - load the precomputed control byte ctrl[g];
//   - LXVB16X the 16 source bytes of the group into VS32 (= V0). LXVB16X is a
//     byte-order-stable load: memory byte k lands in vector element k, so lane L's
//     value-byte j sits at element L*4+j and a VPERM index equal to that offset
//     selects it directly;
//   - LXVB16X the encodeShuffleTablePerm[ctrl] mask into VS33 (= V1);
//   - VPERM V0, V2, V1, V3 — Vd=V3, indexes the 32-byte pair (V0 = source bytes
//     0..15, V2 = the all-zero register, bytes 16..31): result byte i =
//     pair[V1[i] & 0x1F]. The mask packs the low code+1 bytes of every lane
//     contiguously; a mask byte of 16 (permZeroIndex) selects V2 byte 0 = 0 for
//     the unused trailing positions;
//   - STXVB16X the result (VS35 = V3) at the running data cursor (data +
//     dataWritten), the inverse of LXVB16X. Only lengthTable[ctrl] bytes are
//     significant; the trailing zeros are overwritten by the next group (or left
//     in EncodedMaxLen slack);
//   - advance the data cursor by lengthTable[ctrl] (1..16).
//
// VSX↔VMX aliasing: AltiVec register Vn is the SAME physical register as VS(32+n)
// (NOT VSn). LXVB16X/STXVB16X name VS registers; VPERM/VSPLTISB name V registers.
// We load into VS32/VS33 and refer to them as V0/V1, build the zero register with
// VSPLTISB $0, V2, and store from V3 via VS35.
//
// ENDIANNESS: ppc64le is little-endian, but LXVB16X/STXVB16X make the in-register
// byte numbering match memory order, and encodeShuffleTablePerm is built in that
// same order, so the kernel is endian-clean (LXVB16X, not LXVD2X). Returns the
// total data bytes written.
//
// Run: go run encode_ppc64le_gen.go
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
			abi.Scalar("data", abi.Ptr), abi.Scalar("ctrl", abi.Ptr), abi.Scalar("src", abi.Ptr),
			abi.Scalar("groups", abi.Int64),
			abi.Scalar("perm", abi.Ptr), abi.Scalar("lens", abi.Ptr),
		},
		[]abi.Arg{abi.Scalar("ret", abi.Int64)},
	)
	b := ppc64.NewFunc("encodeGroupsVSX", sig, 0)
	b.LoadArg("data", "R3").
		LoadArg("ctrl", "R4").
		LoadArg("src", "R5").
		LoadArg("groups", "R6").
		LoadArg("perm", "R7").
		LoadArg("lens", "R8").
		Raw("MOVD $0, R9").     // g = 0
		Raw("MOVD $0, R10").    // dataWritten = 0
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
		// source load at src + g*16
		Raw("SLD $4, R9, R15").
		Raw("ADD R5, R15, R15").
		Raw("LXVB16X (R15), VS32").  // V0 = 4 uint32
		Raw("VPERM V0, V2, V1, V3"). // V3 = compacted (zeros via V2)
		// store at data + dataWritten
		Raw("ADD R3, R10, R16").
		Raw("STXVB16X VS35, (R16)"). // V3 -> data
		// advance dataWritten by lengthTable[ctrl]
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
	if err := os.WriteFile("encode_ppc64le.s", []byte(f.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote encode_ppc64le.s")
}
