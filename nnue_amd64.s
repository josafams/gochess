//go:build amd64 && !purego

#include "textflag.h"

// func cpuid(eaxArg, ecxArg uint32) (a, b, c, d uint32)
TEXT ·cpuid(SB), NOSPLIT, $0-24
	MOVL eaxArg+0(FP), AX
	MOVL ecxArg+4(FP), CX
	CPUID
	MOVL AX, a+8(FP)
	MOVL BX, b+12(FP)
	MOVL CX, c+16(FP)
	MOVL DX, d+20(FP)
	RET

// func xgetbv() (eax, edx uint32)
TEXT ·xgetbv(SB), NOSPLIT, $0-8
	MOVL $0, CX
	XGETBV
	MOVL AX, eax+0(FP)
	MOVL DX, edx+4(FP)
	RET

// func addSubAVX2(dst, src, add, sub *int16, n int)
// dst = src + add - sub (n múltiplo de 16)
TEXT ·addSubAVX2(SB), NOSPLIT, $0-40
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ add+16(FP), AX
	MOVQ sub+24(FP), BX
	MOVQ n+32(FP), CX
	SHRQ $4, CX
as_loop:
	VMOVDQU (SI), Y0
	VPADDW  (AX), Y0, Y0
	VPSUBW  (BX), Y0, Y0
	VMOVDQU Y0, (DI)
	ADDQ $32, SI
	ADDQ $32, AX
	ADDQ $32, BX
	ADDQ $32, DI
	DECQ CX
	JNZ  as_loop
	VZEROUPPER
	RET

// func addSubSubAVX2(dst, src, add, sub1, sub2 *int16, n int)
// dst = src + add - sub1 - sub2
TEXT ·addSubSubAVX2(SB), NOSPLIT, $0-48
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ add+16(FP), AX
	MOVQ sub1+24(FP), BX
	MOVQ sub2+32(FP), DX
	MOVQ n+40(FP), CX
	SHRQ $4, CX
ass_loop:
	VMOVDQU (SI), Y0
	VPADDW  (AX), Y0, Y0
	VPSUBW  (BX), Y0, Y0
	VPSUBW  (DX), Y0, Y0
	VMOVDQU Y0, (DI)
	ADDQ $32, SI
	ADDQ $32, AX
	ADDQ $32, BX
	ADDQ $32, DX
	ADDQ $32, DI
	DECQ CX
	JNZ  ass_loop
	VZEROUPPER
	RET

// func screluDotAVX2(x, w *int16, n int, out *[8]int32)
// out[lane] += soma de clamp(x,0,255)² * w, em 8 lanes de int32.
// Usa v*(v*w): v*w cabe em int16 se |w| <= 128 (garantido no carregamento).
TEXT ·screluDotAVX2(SB), NOSPLIT, $0-32
	MOVQ x+0(FP), SI
	MOVQ w+8(FP), AX
	MOVQ n+16(FP), CX
	MOVQ out+24(FP), DX
	SHRQ $4, CX
	VPXOR Y5, Y5, Y5
	VPXOR Y6, Y6, Y6
	MOVQ $0x00ff00ff00ff00ff, R8
	MOVQ R8, X7
	VPBROADCASTQ X7, Y7
sd_loop:
	VMOVDQU  (SI), Y0
	VPMAXSW  Y6, Y0, Y0
	VPMINSW  Y7, Y0, Y0
	VPMULLW  (AX), Y0, Y1
	VPMADDWD Y1, Y0, Y1
	VPADDD   Y1, Y5, Y5
	ADDQ $32, SI
	ADDQ $32, AX
	DECQ CX
	JNZ  sd_loop
	VMOVDQU (DX), Y1
	VPADDD  Y1, Y5, Y5
	VMOVDQU Y5, (DX)
	VZEROUPPER
	RET
