package chess

// Geração em etapas usada pelo seletor de lances da busca (movepick.go).

// generateQuietsInto gera só os lances que não são capturas nem promoções,
// na mesma ordem relativa de generateInto.
func (p *Position) GenerateQuietsInto(moves []Move) []Move {
	us, them := p.Side, p.Side^1
	empty := ^p.All
	fwd, startRank, promoRank := 8, 1, 6
	if us == Black {
		fwd, startRank, promoRank = -8, 6, 1
	}
	pawns := p.Pieces[us][Pawn]
	for pawns != 0 {
		from := PopLSB(&pawns)
		rank := from / 8
		if rank == promoRank {
			continue
		}
		to := from + fwd
		if SqBB(to)&empty != 0 {
			moves = append(moves, NewMove(from, to, 0, 0))
			if rank == startRank && SqBB(to+fwd)&empty != 0 {
				moves = append(moves, NewMove(from, to+fwd, 0, FlagDouble))
			}
		}
	}
	for pt := Knight; pt <= King; pt++ {
		bb := p.Pieces[us][pt]
		for bb != 0 {
			from := PopLSB(&bb)
			var att Bitboard
			switch pt {
			case Knight:
				att = KnightAtt[from]
			case Bishop:
				att = BishopAttacks(from, p.All)
			case Rook:
				att = RookAttacks(from, p.All)
			case Queen:
				att = BishopAttacks(from, p.All) | RookAttacks(from, p.All)
			case King:
				att = KingAtt[from]
			}
			att &= empty
			for att != 0 {
				moves = append(moves, NewMove(from, PopLSB(&att), 0, 0))
			}
		}
	}
	base := 0
	kSide, qSide := uint8(CastleWK), uint8(CastleWQ)
	if us == Black {
		base, kSide, qSide = 56, CastleBK, CastleBQ
	}
	e := base + 4
	if p.Castle&kSide != 0 && p.All&(SqBB(e+1)|SqBB(e+2)) == 0 &&
		!p.Attacked(e, them) && !p.Attacked(e+1, them) && !p.Attacked(e+2, them) {
		moves = append(moves, NewMove(e, e+2, 0, FlagCastle))
	}
	if p.Castle&qSide != 0 && p.All&(SqBB(e-1)|SqBB(e-2)|SqBB(e-3)) == 0 &&
		!p.Attacked(e, them) && !p.Attacked(e-1, them) && !p.Attacked(e-2, them) {
		moves = append(moves, NewMove(e, e-2, 0, FlagCastle))
	}
	return moves
}

// isPseudoLegal: generateInto geraria exatamente este lance nesta posição?
// Usado para testar o lance da TT antes de gerar a lista (pode vir de uma
// colisão de hash).
func (p *Position) IsPseudoLegal(m Move) bool {
	if m == NoMove || m>>20 != 0 || m&(1<<15) != 0 {
		return false
	}
	from, to := m.From(), m.To()
	us, them := p.Side, p.Side^1
	pc := p.Board[from]
	if pc < 0 || int(pc)/6 != us || p.Occ[us]&SqBB(to) != 0 {
		return false
	}
	pt, promo := int(pc)%6, m.Promo()
	flags := m &^ 0xFFFF
	enemyTo := p.Occ[them]&SqBB(to) != 0

	if pt == Pawn {
		fwd, startRank, promoRank := 8, 1, 6
		if us == Black {
			fwd, startRank, promoRank = -8, 6, 1
		}
		rank := from / 8
		if (rank == promoRank) != (promo != 0) || promo > Queen || (promo != 0 && promo < Knight) {
			return false
		}
		switch {
		case to == from+fwd:
			return flags == 0 && p.All&SqBB(to) == 0
		case to == from+2*fwd:
			return flags == FlagDouble && rank == startRank && p.All&(SqBB(from+fwd)|SqBB(to)) == 0
		case PawnAtt[us][from]&SqBB(to) != 0:
			if flags == FlagCapture|FlagEP {
				return to == p.EP && !enemyTo
			}
			return flags == FlagCapture && enemyTo
		}
		return false
	}
	if promo != 0 {
		return false
	}
	if flags == FlagCastle {
		if pt != King {
			return false
		}
		for _, c := range p.GenerateQuietsInto(make([]Move, 0, 64)) {
			if c == m {
				return true
			}
		}
		return false
	}
	var att Bitboard
	switch pt {
	case Knight:
		att = KnightAtt[from]
	case Bishop:
		att = BishopAttacks(from, p.All)
	case Rook:
		att = RookAttacks(from, p.All)
	case Queen:
		att = BishopAttacks(from, p.All) | RookAttacks(from, p.All)
	case King:
		att = KingAtt[from]
	}
	if att&SqBB(to) == 0 {
		return false
	}
	if enemyTo {
		return flags == FlagCapture
	}
	return flags == 0
}
