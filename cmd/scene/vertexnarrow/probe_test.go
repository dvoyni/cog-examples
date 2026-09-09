package main

import (
	"testing"
	"unsafe"
)

func TestBandDataIsNonZero(t *testing.T) {
	band := bandGeometry(8, 18.5*tileWorld, bandHeight, 18.5)
	t.Logf("sizeof=%d  offsets: pos=%d nrm=%d f32=%d u16=%d f16=%d",
		unsafe.Sizeof(UVVertex{}),
		unsafe.Offsetof(UVVertex{}.Position), unsafe.Offsetof(UVVertex{}.Normal),
		unsafe.Offsetof(UVVertex{}.UVf32), unsafe.Offsetof(UVVertex{}.UVu16),
		unsafe.Offsetof(UVVertex{}.UVf16))
	for _, i := range []int{0, 1, 6, 7, len(band) - 2, len(band) - 1} {
		v := band[i]
		t.Logf("v[%d] uv=%v u32=%d f16=%v", i, v.UVf32, v.UVu16, v.UVf16)
	}
}
