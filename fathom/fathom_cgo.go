//go:build cgo

// Package fathom lê as tablebases Syzygy (resultado perfeito de finais com
// poucas peças) usando a biblioteca Fathom, em C (licença MIT, ver LICENSE).
// O código C fica em arquivos .inc incluídos aqui, para que builds sem cgo
// (WebAssembly, por exemplo) continuem funcionando com o stub em fathom_stub.go.
package fathom

/*
#cgo CFLAGS: -O3 -std=gnu11 -w
#include <stdlib.h>
#include "tbprobe.c.inc"

static unsigned gc_probe_wdl(uint64_t w, uint64_t b, uint64_t k, uint64_t q, uint64_t r,
                          uint64_t bi, uint64_t n, uint64_t p, unsigned ep, int turn) {
	return tb_probe_wdl(w, b, k, q, r, bi, n, p, 0, 0, ep, turn != 0);
}

static unsigned gc_probe_root(uint64_t w, uint64_t b, uint64_t k, uint64_t q, uint64_t r,
                           uint64_t bi, uint64_t n, uint64_t p, unsigned rule50,
                           unsigned ep, int turn, unsigned *results) {
	return tb_probe_root(w, b, k, q, r, bi, n, p, rule50, 0, ep, turn != 0, results);
}
*/
import "C"

import (
	"sync"
	"unsafe"
)

// Resultados WDL (do ponto de vista de quem joga).
const (
	Loss        = 0 // derrota
	BlessedLoss = 1 // derrota, mas empate pela regra dos 50 lances
	Draw        = 2
	CursedWin   = 3 // vitória, mas empate pela regra dos 50 lances
	Win         = 4
	Failed      = 0xFFFFFFFF
	maxMoves    = 193
)

// Position: bitboards no mesmo formato do GoChess (a1 = bit 0).
type Position struct {
	White, Black                                  uint64
	Kings, Queens, Rooks, Bishops, Knights, Pawns uint64
	Rule50                                        uint
	EP                                            uint // casa de en passant ou 0
	WhiteToMove                                   bool
}

// RootMove: um lance legal da raiz com o resultado da tablebase.
type RootMove struct {
	From, To int
	Promo    int // 0 nenhuma, 1 dama, 2 torre, 3 bispo, 4 cavalo
	WDL      int
	DTZ      int
}

var mu sync.Mutex

// Available: há suporte compilado (cgo).
const Available = true

// Init carrega as tablebases do caminho dado (vários diretórios separados por
// ":" no Linux). Devolve o número máximo de peças coberto (0 = nenhuma).
func Init(path string) int {
	mu.Lock()
	defer mu.Unlock()
	cs := C.CString(path)
	defer C.free(unsafe.Pointer(cs))
	if !C.tb_init(cs) {
		return 0
	}
	return int(C.TB_LARGEST)
}

func turn(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// ProbeWDL consulta a tabela WDL. Só vale sem roque e com a regra dos 50 zerada
// (logo após captura ou lance de peão); fora disso devolve Failed.
// Pode ser chamada por várias threads ao mesmo tempo.
func ProbeWDL(p *Position) uint {
	if p.Rule50 != 0 {
		return Failed
	}
	return uint(C.gc_probe_wdl(C.uint64_t(p.White), C.uint64_t(p.Black), C.uint64_t(p.Kings),
		C.uint64_t(p.Queens), C.uint64_t(p.Rooks), C.uint64_t(p.Bishops), C.uint64_t(p.Knights),
		C.uint64_t(p.Pawns), C.unsigned(p.EP), turn(p.WhiteToMove)))
}

// ProbeRoot devolve o resultado de cada lance legal da raiz (usa DTZ).
// Não é segura para várias threads: chamar só uma vez por busca, na raiz.
func ProbeRoot(p *Position) ([]RootMove, bool) {
	mu.Lock()
	defer mu.Unlock()
	var res [maxMoves]C.unsigned
	r := C.gc_probe_root(C.uint64_t(p.White), C.uint64_t(p.Black), C.uint64_t(p.Kings),
		C.uint64_t(p.Queens), C.uint64_t(p.Rooks), C.uint64_t(p.Bishops), C.uint64_t(p.Knights),
		C.uint64_t(p.Pawns), C.unsigned(p.Rule50), C.unsigned(p.EP), turn(p.WhiteToMove), &res[0])
	if uint(r) == Failed {
		return nil, false
	}
	var out []RootMove
	for _, v := range res {
		x := uint(v)
		if x == Failed {
			break
		}
		out = append(out, RootMove{
			From:  int(x >> 10 & 0x3F),
			To:    int(x >> 4 & 0x3F),
			Promo: int(x >> 16 & 0x7),
			WDL:   int(x & 0xF),
			DTZ:   int(x >> 20),
		})
	}
	return out, true
}
