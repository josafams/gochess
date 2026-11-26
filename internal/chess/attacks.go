package chess

import "math/bits"

// Tabelas de ataque pré-calculadas.
var (
	KnightAtt [64]Bitboard
	KingAtt   [64]Bitboard
	PawnAtt   [2][64]Bitboard // [cor][casa]: casas que um peão dessa cor ataca
	rays      [8][64]Bitboard // raios a partir de cada casa, em 8 direções
)

// Direções (df, dr). As 4 primeiras aumentam o índice da casa (N, E, NE, NW);
// as 4 últimas diminuem (S, W, SE, SW). Isso importa para achar o primeiro bloqueador.
var dirDelta = [8][2]int{
	{0, 1}, {1, 0}, {1, 1}, {-1, 1},
	{0, -1}, {-1, 0}, {1, -1}, {-1, -1},
}

func init() {
	for sq := 0; sq < 64; sq++ {
		f, r := sq%8, sq/8
		add := func(bb *Bitboard, df, dr int) {
			nf, nr := f+df, r+dr
			if nf >= 0 && nf < 8 && nr >= 0 && nr < 8 {
				*bb |= SqBB(nr*8 + nf)
			}
		}
		for _, d := range [][2]int{{1, 2}, {2, 1}, {2, -1}, {1, -2}, {-1, -2}, {-2, -1}, {-2, 1}, {-1, 2}} {
			add(&KnightAtt[sq], d[0], d[1])
		}
		for df := -1; df <= 1; df++ {
			for dr := -1; dr <= 1; dr++ {
				if df != 0 || dr != 0 {
					add(&KingAtt[sq], df, dr)
				}
			}
		}
		add(&PawnAtt[White][sq], -1, 1)
		add(&PawnAtt[White][sq], 1, 1)
		add(&PawnAtt[Black][sq], -1, -1)
		add(&PawnAtt[Black][sq], 1, -1)

		for d := 0; d < 8; d++ {
			nf, nr := f+dirDelta[d][0], r+dirDelta[d][1]
			for nf >= 0 && nf < 8 && nr >= 0 && nr < 8 {
				rays[d][sq] |= SqBB(nr*8 + nf)
				nf += dirDelta[d][0]
				nr += dirDelta[d][1]
			}
		}
	}
}

// slide devolve o ataque de uma peça deslizante numa direção, parando no
// primeiro bloqueador (que fica incluído: pode ser uma captura).
// É a técnica "classical ray attacks"; o Stockfish usa magic bitboards/PEXT.
func slide(d, sq int, occ Bitboard) Bitboard {
	att := rays[d][sq]
	if blockers := att & occ; blockers != 0 {
		var b int
		if d < 4 {
			b = bits.TrailingZeros64(uint64(blockers))
		} else {
			b = 63 - bits.LeadingZeros64(uint64(blockers))
		}
		att ^= rays[d][b]
	}
	return att
}

// bishopAttacks e rookAttacks: ver magic.go.

// Attacked diz se a casa sq é atacada por alguma peça da cor `by`.
// Truque: olha "de dentro para fora", gerando ataques a partir de sq.
func (p *Position) Attacked(sq, by int) bool {
	pc := &p.Pieces[by]
	if PawnAtt[by^1][sq]&pc[Pawn] != 0 ||
		KnightAtt[sq]&pc[Knight] != 0 ||
		KingAtt[sq]&pc[King] != 0 {
		return true
	}
	if BishopAttacks(sq, p.All)&(pc[Bishop]|pc[Queen]) != 0 {
		return true
	}
	return RookAttacks(sq, p.All)&(pc[Rook]|pc[Queen]) != 0
}

func (p *Position) InCheck() bool {
	return p.Attacked(LSB(p.Pieces[p.Side][King]), p.Side^1)
}
