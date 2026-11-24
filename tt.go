package main

import "sync/atomic"

// Tabela de transposição compartilhada entre threads, sem travas.
// Cada entrada são dois uint64: dados e (chave XOR dados). Se outra thread
// escrever no meio da leitura, o XOR não bate e a entrada é ignorada.
//
// dados: lance (20 bits) | nota+offset (18 bits) | profundidade (8) | tipo (2)
//        | eval estática (16 bits, int16; evalNone = não calculada)

const (
	ttExact = iota + 1 // nota exata
	ttLower            // nota >= valor (houve corte beta)
	ttUpper            // nota <= valor (nenhum lance superou alfa)
)

const ttScoreOffset = 1 << 17

// evalNone: sem eval estática (posição em xeque).
const evalNone = -32768

type ttEntry struct {
	move  Move
	score int
	depth int
	flag  int
	eval  int
}

type TT struct {
	slots []atomic.Uint64
	mask  uint64
}

func NewTT(mb int) *TT {
	if mb < 1 {
		mb = 1
	}
	n := uint64(mb) * 1024 * 1024 / 16
	size := uint64(1)
	for size*2 <= n {
		size *= 2
	}
	return &TT{slots: make([]atomic.Uint64, 2*size), mask: size - 1}
}

func (t *TT) Clear() {
	for i := range t.slots {
		t.slots[i].Store(0)
	}
}

func packTT(m Move, score, depth, flag, eval int) uint64 {
	if depth < 0 {
		depth = 0
	}
	if depth > 255 {
		depth = 255
	}
	eval = max(evalNone, min(32767, eval))
	return uint64(m)&0xFFFFF | uint64(score+ttScoreOffset)&0x3FFFF<<20 | uint64(depth)<<38 | uint64(flag)<<46 |
		uint64(uint16(int16(eval)))<<48
}

func unpackTT(d uint64) ttEntry {
	return ttEntry{
		move:  Move(d & 0xFFFFF),
		score: int(d>>20&0x3FFFF) - ttScoreOffset,
		depth: int(d >> 38 & 0xFF),
		flag:  int(d >> 46 & 3),
		eval:  int(int16(uint16(d >> 48))),
	}
}

func (t *TT) probe(key uint64) (ttEntry, bool) {
	i := (key & t.mask) * 2
	data := t.slots[i+1].Load()
	if data == 0 || t.slots[i].Load()^data != key {
		return ttEntry{}, false
	}
	return unpackTT(data), true
}

func (t *TT) store(key uint64, move Move, score, depth, flag, eval int) {
	i := (key & t.mask) * 2
	old := t.slots[i+1].Load()
	if old != 0 && t.slots[i].Load()^old == key {
		e := unpackTT(old)
		if move == NoMove {
			move = e.move
		}
		// Mantém entradas mais profundas da mesma posição.
		if e.depth > depth+2 && flag != ttExact {
			return
		}
	}
	data := packTT(move, score, depth, flag, eval)
	t.slots[i].Store(key ^ data)
	t.slots[i+1].Store(data)
}

// Hashfull: fração ocupada (por mil), estimada nas primeiras 1000 entradas.
func (t *TT) Hashfull() int {
	n, used := 0, 0
	for i := uint64(0); i < 1000 && i <= t.mask; i++ {
		n++
		if t.slots[2*i+1].Load() != 0 {
			used++
		}
	}
	if n == 0 {
		return 0
	}
	return used * 1000 / n
}

func scoreToTT(score, ply int) int {
	if score > Mate-2*MaxPly {
		return score + ply
	}
	if score < -Mate+2*MaxPly {
		return score - ply
	}
	return score
}

func scoreFromTT(score, ply int) int {
	if score > Mate-2*MaxPly {
		return score - ply
	}
	if score < -Mate+2*MaxPly {
		return score + ply
	}
	return score
}
