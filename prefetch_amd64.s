//go:build amd64 && !purego

#include "textflag.h"

// func prefetchAddr(addr uintptr)
// Pede à CPU para trazer a linha de cache para o L1 enquanto a busca
// continua; quando o filho ler a tabela, a entrada já estará no cache.
TEXT ·prefetchAddr(SB), NOSPLIT, $0-8
	MOVQ addr+0(FP), AX
	PREFETCHT0 (AX)
	RET
