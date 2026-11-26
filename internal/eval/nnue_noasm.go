//go:build !amd64 || purego

package eval

const hasAVX2 = false

func addSubAVX2(dst, src, add, sub *int16, n int)           { panic("sem AVX2") }
func addSubSubAVX2(dst, src, add, sub1, sub2 *int16, n int) { panic("sem AVX2") }
func screluDotAVX2(x, w *int16, n int, out *[8]int32)       { panic("sem AVX2") }

func screluDot2AVX2(us, them, w *int16, n int) int64 { panic("sem AVX2") }
