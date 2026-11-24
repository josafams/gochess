package main

import "math/bits"

// Magic bitboards: o ataque de bispo/torre vira uma consulta de tabela.
// (ocupação & máscara) * número mágico >> deslocamento dá um índice único
// para cada configuração de bloqueadores relevante. Os números mágicos são
// achados por busca aleatória na inicialização (semente fixa, ~20 ms).

type magicEntry struct {
	mask   Bitboard
	magic  uint64
	shift  uint8
	offset uint32
}

var (
	rookMagic   [64]magicEntry
	bishopMagic [64]magicEntry
	magicTable  []Bitboard
)

func slowAttacks(sq int, occ Bitboard, dirs [4]int) Bitboard {
	var a Bitboard
	for _, d := range dirs {
		a |= slide(d, sq, occ)
	}
	return a
}

// edgeless: máscara sem as bordas (uma peça na borda não muda o ataque).
func relevantMask(sq int, dirs [4]int) Bitboard {
	var m Bitboard
	for _, d := range dirs {
		r := rays[d][sq]
		// tira a última casa de cada raio
		if r != 0 {
			var last int
			if d < 4 {
				last = 63 - bits.LeadingZeros64(uint64(r))
			} else {
				last = bits.TrailingZeros64(uint64(r))
			}
			r &^= sqBB(last)
		}
		m |= r
	}
	return m
}

func init() {
	rookDirs := [4]int{0, 1, 4, 5}
	bishopDirs := [4]int{2, 3, 6, 7}
	seed := uint64(0x9E3779B97F4A7C15)
	rnd := func() uint64 { // xorshift64*
		seed ^= seed >> 12
		seed ^= seed << 25
		seed ^= seed >> 27
		return seed * 2685821657736338717
	}
	sparse := func() uint64 { return rnd() & rnd() & rnd() }

	var occs, atts [4096]Bitboard
	var used [4096]uint32
	epoch := uint32(0)
	for _, kind := range []struct {
		tab  *[64]magicEntry
		dirs [4]int
	}{{&rookMagic, rookDirs}, {&bishopMagic, bishopDirs}} {
		for sq := 0; sq < 64; sq++ {
			mask := relevantMask(sq, kind.dirs)
			nbits := mask.Count()
			n := 0
			// Enumera todos os subconjuntos da máscara (Carry-Rippler).
			for sub := Bitboard(0); ; {
				occs[n], atts[n] = sub, slowAttacks(sq, sub, kind.dirs)
				n++
				sub = (sub - mask) & mask
				if sub == 0 {
					break
				}
			}
			e := magicEntry{mask: mask, shift: uint8(64 - nbits), offset: uint32(len(magicTable))}
			table := make([]Bitboard, 1<<nbits)
			for {
				magic := sparse()
				if bits.OnesCount64((uint64(mask)*magic)>>56) < 6 {
					continue
				}
				epoch++
				ok := true
				for i := 0; i < n; i++ {
					idx := (uint64(occs[i]) * magic) >> e.shift
					if used[idx] != epoch {
						used[idx] = epoch
						table[idx] = atts[i]
					} else if table[idx] != atts[i] {
						ok = false
						break
					}
				}
				if ok {
					e.magic = magic
					break
				}
			}
			kind.tab[sq] = e
			magicTable = append(magicTable, table...)
		}
	}
}

func rookAttacks(sq int, occ Bitboard) Bitboard {
	e := &rookMagic[sq]
	return magicTable[e.offset+uint32((uint64(occ&e.mask)*e.magic)>>e.shift)]
}

func bishopAttacks(sq int, occ Bitboard) Bitboard {
	e := &bishopMagic[sq]
	return magicTable[e.offset+uint32((uint64(occ&e.mask)*e.magic)>>e.shift)]
}
