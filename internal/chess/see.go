package chess

// SEE (Static Exchange Evaluation): simula a sequência de capturas numa casa,
// sempre com a peça menos valiosa, e devolve o saldo material do lance.
// Serve para ordenar capturas e descartar as perdedoras.

var SEEValue = [6]int{100, 320, 330, 500, 900, 20000}

func (p *Position) attackersTo(sq int, occ Bitboard) Bitboard {
	w, b := &p.Pieces[White], &p.Pieces[Black]
	return PawnAtt[Black][sq]&w[Pawn] | PawnAtt[White][sq]&b[Pawn] |
		KnightAtt[sq]&(w[Knight]|b[Knight]) |
		KingAtt[sq]&(w[King]|b[King]) |
		BishopAttacks(sq, occ)&(w[Bishop]|b[Bishop]|w[Queen]|b[Queen]) |
		RookAttacks(sq, occ)&(w[Rook]|b[Rook]|w[Queen]|b[Queen])
}

func (p *Position) SEE(m Move) int {
	from, to := m.From(), m.To()
	if m&FlagCastle != 0 {
		return 0
	}
	var gain [32]int
	occ := p.All ^ SqBB(from)
	if m&FlagEP != 0 {
		gain[0] = SEEValue[Pawn]
		if p.Side == White {
			occ ^= SqBB(to - 8)
		} else {
			occ ^= SqBB(to + 8)
		}
	} else if p.Board[to] >= 0 {
		gain[0] = SEEValue[int(p.Board[to])%6]
	}
	attacker := int(p.Board[from]) % 6
	if pr := m.Promo(); pr != 0 {
		gain[0] += SEEValue[pr] - SEEValue[Pawn]
		attacker = pr
	}

	diag := p.Pieces[White][Bishop] | p.Pieces[Black][Bishop] | p.Pieces[White][Queen] | p.Pieces[Black][Queen]
	orth := p.Pieces[White][Rook] | p.Pieces[Black][Rook] | p.Pieces[White][Queen] | p.Pieces[Black][Queen]
	attackers := p.attackersTo(to, occ) & occ
	side := p.Side ^ 1
	d := 0
	for {
		d++
		gain[d] = SEEValue[attacker] - gain[d-1]
		if max(-gain[d-1], gain[d]) < 0 || d == 31 {
			break
		}
		mine := attackers & p.Occ[side]
		if mine == 0 {
			break
		}
		next := -1
		for pt := Pawn; pt <= King; pt++ {
			if bb := mine & p.Pieces[side][pt]; bb != 0 {
				from = LSB(bb)
				next = pt
				break
			}
		}
		// O rei só captura se a casa não estiver mais defendida.
		if next == King && attackers&p.Occ[side^1]&^SqBB(from) != 0 {
			break
		}
		attacker = next
		occ ^= SqBB(from)
		// Revela atacantes em raio-x.
		attackers |= BishopAttacks(to, occ)&diag | RookAttacks(to, occ)&orth
		attackers &= occ
		side ^= 1
	}
	for d--; d > 0; d-- {
		gain[d-1] = -max(-gain[d-1], gain[d])
	}
	return gain[0]
}
