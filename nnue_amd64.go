//go:build amd64 && !purego

package main

func cpuid(eaxArg, ecxArg uint32) (a, b, c, d uint32)
func xgetbv() (eax, edx uint32)

//go:noescape
func addSubAVX2(dst, src, add, sub *int16, n int)

//go:noescape
func addSubSubAVX2(dst, src, add, sub1, sub2 *int16, n int)

//go:noescape
func screluDotAVX2(x, w *int16, n int, out *[8]int32)

//go:noescape
func screluDot2AVX2(us, them, w *int16, n int) int64

// hasAVX2: CPU com AVX2 e sistema salvando os registradores YMM.
var hasAVX2 = func() bool {
	max, _, _, _ := cpuid(0, 0)
	if max < 7 {
		return false
	}
	_, _, c, _ := cpuid(1, 0)
	if c&(1<<27) == 0 || c&(1<<28) == 0 { // OSXSAVE, AVX
		return false
	}
	if eax, _ := xgetbv(); eax&6 != 6 {
		return false
	}
	_, b, _, _ := cpuid(7, 0)
	return b&(1<<5) != 0
}()
