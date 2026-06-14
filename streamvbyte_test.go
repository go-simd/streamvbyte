package streamvbyte

import (
	"bytes"
	"math/rand"
	"testing"
)

// reference scalar encode used to cross-check the public Encode independently of
// any SIMD path (Encode itself is pure Go but this guards the wire layout).
func refEncode(src []uint32) []byte {
	n := len(src)
	if n == 0 {
		return nil
	}
	cl := (n + 3) / 4
	ctrl := make([]byte, cl)
	var data []byte
	var key byte
	shift := uint(0)
	ki := 0
	for c := 0; c < n; c++ {
		if shift == 8 {
			ctrl[ki] = key
			ki++
			key = 0
			shift = 0
		}
		v := src[c]
		switch {
		case v < 1<<8:
			data = append(data, byte(v))
		case v < 1<<16:
			data = append(data, byte(v), byte(v>>8))
			key |= 1 << shift
		case v < 1<<24:
			data = append(data, byte(v), byte(v>>8), byte(v>>16))
			key |= 2 << shift
		default:
			data = append(data, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
			key |= 3 << shift
		}
		shift += 2
	}
	ctrl[ki] = key
	return append(ctrl, data...)
}

var roundTripCases = [][]uint32{
	{},
	{0},
	{255},
	{256},
	{65535},
	{65536},
	{1 << 24},
	{1<<24 - 1},
	{0xFFFFFFFF},
	{0, 1, 2, 3},
	{255, 256, 65535, 65536},
	{1 << 24, 0xFFFFFFFF, 0, 300},
	{1, 2, 3, 4, 5},
	{0, 0, 0, 0, 0, 0, 0, 0, 0},
	{0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF},
}

func TestRoundTripTable(t *testing.T) {
	for i, src := range roundTripCases {
		dst := make([]byte, EncodedMaxLen(len(src)))
		w := Encode(dst, src)
		// Cross-check wire format against the independent reference encoder.
		if ref := refEncode(src); !bytes.Equal(dst[:w], ref) {
			t.Fatalf("case %d: wire mismatch\n got %v\nwant %v", i, dst[:w], ref)
		}
		out := make([]uint32, len(src))
		r := Decode(out, dst[:w], len(src))
		if r != w {
			t.Errorf("case %d: Decode consumed %d, Encode wrote %d", i, r, w)
		}
		if len(src) == 0 {
			continue
		}
		if !equalU32(out, src) {
			t.Errorf("case %d: round-trip mismatch\n got %v\nwant %v", i, out, src)
		}
	}
}

func equalU32(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRoundTripSizes exercises every length from 0..200 across all four code
// widths and at group boundaries, plus all-same-width runs that stress each
// shuffle-table family.
func TestRoundTripSizes(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	widthVal := func(w int) uint32 {
		switch w {
		case 0:
			return uint32(rng.Intn(256))
		case 1:
			return uint32(256 + rng.Intn(65536-256))
		case 2:
			return uint32(1<<16 + rng.Intn(1<<24-1<<16))
		default:
			return uint32(1<<24 + rng.Intn(1))
		}
	}
	for n := 0; n <= 200; n++ {
		src := make([]uint32, n)
		for i := range src {
			src[i] = widthVal(rng.Intn(4))
		}
		checkRoundTrip(t, src)
	}
	// Fixed-width runs hit uniform control bytes (0x00, 0x55, 0xAA, 0xFF).
	for w := 0; w < 4; w++ {
		for _, n := range []int{1, 4, 5, 7, 8, 16, 33, 64, 100} {
			src := make([]uint32, n)
			for i := range src {
				src[i] = widthVal(w)
			}
			checkRoundTrip(t, src)
		}
	}
}

func checkRoundTrip(t *testing.T, src []uint32) {
	t.Helper()
	dst := make([]byte, EncodedMaxLen(len(src)))
	w := Encode(dst, src)
	out := make([]uint32, len(src))
	r := Decode(out, dst[:w], len(src))
	if r != w {
		t.Fatalf("n=%d: Decode consumed %d != %d written", len(src), r, w)
	}
	if !equalU32(out, src) {
		t.Fatalf("n=%d: round-trip mismatch\n got %v\nwant %v", len(src), out, src)
	}
}

func TestEncodedMaxLen(t *testing.T) {
	if EncodedMaxLen(0) != 0 {
		t.Error("EncodedMaxLen(0) should be 0")
	}
	if EncodedMaxLen(-3) != 0 {
		t.Error("EncodedMaxLen(negative) should be 0")
	}
	if got := EncodedMaxLen(4); got != 1+16 {
		t.Errorf("EncodedMaxLen(4) = %d, want 17", got)
	}
	if got := EncodedMaxLen(5); got != 2+20 {
		t.Errorf("EncodedMaxLen(5) = %d, want 22", got)
	}
}

func TestEncodeEmpty(t *testing.T) {
	if n := Encode(nil, nil); n != 0 {
		t.Errorf("Encode(nil,nil) = %d, want 0", n)
	}
	out := make([]uint32, 0)
	if n := Decode(out, nil, 0); n != 0 {
		t.Errorf("Decode(_,_,0) = %d, want 0", n)
	}
}

func TestEncodePanicsShortDst(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on short dst")
		}
	}()
	Encode(make([]byte, 0), []uint32{1, 2, 3, 4, 5})
}

func TestDecodePanicsShortDst(t *testing.T) {
	src := []uint32{1, 2, 3, 4, 5}
	enc := make([]byte, EncodedMaxLen(len(src)))
	w := Encode(enc, src)
	defer func() {
		if recover() == nil {
			t.Error("expected panic on short dst")
		}
	}()
	Decode(make([]uint32, 2), enc[:w], len(src))
}

func TestTablesConsistent(t *testing.T) {
	for key := 0; key < 256; key++ {
		want := 4
		for lane := 0; lane < 4; lane++ {
			want += (key >> (uint(lane) * 2)) & 0x3
		}
		if int(lengthTable[key]) != want {
			t.Fatalf("lengthTable[%d] = %d, want %d", key, lengthTable[key], want)
		}
	}
}

func FuzzRoundTrip(f *testing.F) {
	for _, c := range roundTripCases {
		b := make([]byte, len(c)*4)
		for i, v := range c {
			b[i*4] = byte(v)
			b[i*4+1] = byte(v >> 8)
			b[i*4+2] = byte(v >> 16)
			b[i*4+3] = byte(v >> 24)
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		n := len(raw) / 4
		if n > 4096 {
			n = 4096
		}
		src := make([]uint32, n)
		for i := 0; i < n; i++ {
			src[i] = uint32(raw[i*4]) | uint32(raw[i*4+1])<<8 |
				uint32(raw[i*4+2])<<16 | uint32(raw[i*4+3])<<24
		}
		dst := make([]byte, EncodedMaxLen(n))
		w := Encode(dst, src)
		out := make([]uint32, n)
		r := Decode(out, dst[:w], n)
		if r != w {
			t.Fatalf("consumed %d != written %d", r, w)
		}
		if !equalU32(out, src) {
			t.Fatalf("round-trip mismatch for n=%d", n)
		}
	})
}
