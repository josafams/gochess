package search

import (
	"sync/atomic"
	"unsafe"

	"gochess/internal/chess"
)

// Tabela de transposição compartilhada entre threads, sem travas.
// Cada entrada são dois uint64: dados e (chave XOR dados). Se outra thread
// escrever no meio da leitura, o XOR não bate e a entrada é ignorada.
//
// Entradas em buckets de 4 (uma linha de cache), com idade para substituição.

const (
	ttExact = iota + 1 // nota exata
	ttLower            // nota >= valor (houve corte beta)
	ttUpper            // nota <= valor (nenhum lance superou alfa)
)

const ttScoreOffset = 1 << 16

// evalNone: sem eval estática (posição em xeque).
const evalNone = -32768

// Cada bucket tem 4 entradas de 16 bytes = 64 bytes, uma linha de cache:
// uma leitura da memória traz as 4 candidatas.
const ttBucket = 4

type ttEntry struct {
	move  chess.Move
	score int
	depth int
	flag  int
	eval  int
}

type TT struct {
	slots []atomic.Uint64 // 2 por entrada, 8 por bucket, alinhado a 64 bytes
	mask  uint64          // número de buckets - 1
	gen   atomic.Uint32   // idade da busca atual (mod 4)
}

func NewTT(mb int) *TT {
	if mb < 1 {
		mb = 1
	}
	n := uint64(mb) * 1024 * 1024 / 64
	size := uint64(1)
	for size*2 <= n {
		size *= 2
	}
	// Folga de uma linha para alinhar o início a 64 bytes.
	raw := make([]atomic.Uint64, size*2*ttBucket+8)
	off := 0
	for uintptr(unsafe.Pointer(&raw[off]))%64 != 0 {
		off++
	}
	return &TT{slots: raw[off : off+int(size)*2*ttBucket], mask: size - 1}
}

// NewSearch avança a idade: entradas de buscas antigas viram as primeiras
// candidatas à substituição.
func (t *TT) NewSearch() { t.gen.Add(1) }

func (t *TT) Clear() {
	for i := range t.slots {
		t.slots[i].Store(0)
	}
}

// prefetch antecipa a leitura do bucket de uma posição que será buscada em seguida.
func (t *TT) prefetch(key uint64) {
	prefetchAddr(uintptr(unsafe.Pointer(&t.slots[(key&t.mask)*2*ttBucket])))
}

// dados: lance (20) | nota+offset (17) | profundidade (7) | tipo (2) | idade (2) | eval (16)
func packTT(m chess.Move, score, depth, flag, eval int, age uint32) uint64 {
	depth = max(0, min(127, depth))
	score = max(1-ttScoreOffset, min(ttScoreOffset-1, score)) // ±Inf não cabe em 17 bits: vira ±65535, ainda além de qualquer nota de mate
	eval = max(evalNone, min(32767, eval))
	return uint64(m)&0xFFFFF | uint64(score+ttScoreOffset)&0x1FFFF<<20 | uint64(depth)<<37 | uint64(flag)<<44 |
		uint64(age&3)<<46 | uint64(uint16(int16(eval)))<<48
}

func unpackTT(d uint64) ttEntry {
	return ttEntry{
		move:  chess.Move(d & 0xFFFFF),
		score: int(d>>20&0x1FFFF) - ttScoreOffset,
		depth: int(d >> 37 & 0x7F),
		flag:  int(d >> 44 & 3),
		eval:  int(int16(uint16(d >> 48))),
	}
}

func (t *TT) probe(key uint64) (ttEntry, bool) {
	b := t.slots[(key&t.mask)*2*ttBucket:]
	for i := 0; i < 2*ttBucket; i += 2 {
		data := b[i+1].Load()
		if data != 0 && b[i].Load()^data == key {
			return unpackTT(data), true
		}
	}
	return ttEntry{}, false
}

func (t *TT) store(key uint64, move chess.Move, score, depth, flag, eval int) {
	b := t.slots[(key&t.mask)*2*ttBucket:]
	gen := t.gen.Load()
	victim, worst := 0, 1<<30
	for i := 0; i < 2*ttBucket; i += 2 {
		old := b[i+1].Load()
		if old == 0 {
			if worst > -1<<20 {
				victim, worst = i, -1<<20 // vazia: melhor candidata, mas a mesma chave ainda tem prioridade
			}
			continue
		}
		if b[i].Load()^old == key {
			e := unpackTT(old)
			if move == chess.NoMove {
				move = e.move
			}
			// Mantém entradas mais profundas da mesma posição (da busca atual).
			if e.depth > depth+2 && flag != ttExact && uint32(old>>46&3) == gen&3 {
				return
			}
			victim = i
			break
		}
		// Substitui a entrada mais rasa, descontando 8 de profundidade por busca de idade.
		age := int((gen - uint32(old>>46)) & 3)
		if v := int(old>>37&0x7F) - 8*age; v < worst {
			victim, worst = i, v
		}
	}
	data := packTT(move, score, depth, flag, eval, gen)
	b[victim].Store(key ^ data)
	b[victim+1].Store(data)
}

// Hashfull: fração ocupada pela busca atual (por mil), nas primeiras 1000 entradas.
func (t *TT) Hashfull() int {
	gen := t.gen.Load() & 3
	n, used := 0, 0
	for i := 0; i < 1000 && 2*i+1 < len(t.slots); i++ {
		n++
		if d := t.slots[2*i+1].Load(); d != 0 && uint32(d>>46&3) == gen {
			used++
		}
	}
	if n == 0 {
		return 0
	}
	return used * 1000 / n
}

func scoreToTT(score, ply int) int {
	if score > Mate-2*chess.MaxPly {
		return score + ply
	}
	if score < -Mate+2*chess.MaxPly {
		return score - ply
	}
	return score
}

func scoreFromTT(score, ply int) int {
	if score > Mate-2*chess.MaxPly {
		return score - ply
	}
	if score < -Mate+2*chess.MaxPly {
		return score + ply
	}
	return score
}
