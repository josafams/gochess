package chess

// Legalidade sem testar ataques ao rei a cada lance.
//
// Fora de xeque, um lance que não é do rei nem en passant só é ilegal se a peça
// estiver cravada e sair da linha da cravada. Com as peças cravadas calculadas
// uma vez por nó, Make dispensa o teste Attacked para esses lances.

var lineBB, betweenBB [64][64]Bitboard

func init() {
	dirs := [8][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {-1, -1}, {1, -1}, {-1, 1}} // pares opostos: i e i^1
	ray := func(a int, d [2]int) (out []int) {
		f, r := a%8+d[0], a/8+d[1]
		for f >= 0 && f < 8 && r >= 0 && r < 8 {
			out = append(out, r*8+f)
			f, r = f+d[0], r+d[1]
		}
		return out
	}
	for a := 0; a < 64; a++ {
		for i, d := range dirs {
			full := SqBB(a)
			for _, b := range ray(a, d) {
				full |= SqBB(b)
			}
			for _, b := range ray(a, dirs[i^1]) {
				full |= SqBB(b)
			}
			var between Bitboard
			for _, b := range ray(a, d) {
				lineBB[a][b] = full
				betweenBB[a][b] = between
				between |= SqBB(b)
			}
		}
	}
}

// pinned: peças de quem joga cravadas contra o próprio rei.
func (p *Position) Pinned() Bitboard {
	us, them := p.Side, p.Side^1
	ksq := LSB(p.Pieces[us][King])
	pc := &p.Pieces[them]
	snipers := RookAttacks(ksq, 0)&(pc[Rook]|pc[Queen]) | BishopAttacks(ksq, 0)&(pc[Bishop]|pc[Queen])
	var pinned Bitboard
	for snipers != 0 {
		s := PopLSB(&snipers)
		if b := betweenBB[ksq][s] & p.All; b != 0 && b&(b-1) == 0 {
			pinned |= b & p.Occ[us]
		}
	}
	return pinned
}

// MakeFast faz o lance como Make, mas decide a legalidade pelas cravadas quando
// pode. pinned deve vir de p.pinned(); inCheck de p.InCheck().
func (p *Position) MakeFast(m Move, pinned Bitboard, inCheck bool) (Position, bool) {
	from := m.From()
	if inCheck || m&FlagEP != 0 || int(p.Board[from])%6 == King {
		return p.Make(m)
	}
	if pinned&SqBB(from) != 0 && lineBB[from][LSB(p.Pieces[p.Side][King])]&SqBB(m.To()) == 0 {
		return Position{}, false
	}
	return p.makeMove(m), true
}
