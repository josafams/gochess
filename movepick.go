package main

// Geração de lances em etapas.
//
// Em vez de gerar e pontuar todos os lances logo de cara, o seletor entrega:
//  1. o lance da TT, sem gerar nada;
//  2. as capturas e promoções boas (SEE >= 0, promoção a dama), por nota;
//  3. os lances quietos (killers primeiro, depois pelo histórico);
//  4. as capturas ruins e as sub-promoções.
//
// A maioria dos cortes beta acontece nas etapas 1 e 2, então em muitos nós os
// lances quietos nunca chegam a ser gerados nem pontuados.

const (
	stTT = iota
	stNoisyGen
	stGoodNoisy
	stQuietGen
	stQuiet
	stBadNoisy
	stDone
)

type picker struct {
	stage   int
	ttMove  Move
	noisy   []Move
	nScores []int
	ni      int
	quiets  []Move
	qScores []int
	qi      int
}

// skipQuiets: o LMP disparou; os quietos restantes seriam todos podados.
func (pk *picker) skipQuiets() {
	if pk.stage == stQuiet || pk.stage == stQuietGen {
		pk.stage = stBadNoisy
	}
}

func (t *thread) nextMove(pk *picker, p *Position, ply int) Move {
	for {
		switch pk.stage {
		case stTT:
			pk.stage = stNoisyGen
			if pk.ttMove != NoMove && p.isPseudoLegal(pk.ttMove) {
				return pk.ttMove
			}
		case stNoisyGen:
			pk.noisy = p.generateInto(t.mbuf[ply][:0], true)
			pk.nScores = t.sbuf[ply][:len(pk.noisy)]
			for i, m := range pk.noisy {
				pk.nScores[i] = t.scoreMove(p, m, ply, NoMove)
			}
			pk.stage = stGoodNoisy
		case stGoodNoisy:
			for pk.ni < len(pk.noisy) {
				m := pickNext(pk.noisy, pk.nScores, pk.ni)
				if pk.nScores[pk.ni] < 0 {
					break // o resto são capturas ruins: ficam para o fim
				}
				pk.ni++
				if m != pk.ttMove {
					return m
				}
			}
			pk.stage = stQuietGen
		case stQuietGen:
			pk.quiets = p.generateQuietsInto(t.qbuf[ply][:0])
			pk.qScores = t.qsbuf[ply][:len(pk.quiets)]
			for i, m := range pk.quiets {
				pk.qScores[i] = t.scoreMove(p, m, ply, NoMove)
			}
			pk.stage = stQuiet
		case stQuiet:
			for pk.qi < len(pk.quiets) {
				m := pickNext(pk.quiets, pk.qScores, pk.qi)
				pk.qi++
				if m != pk.ttMove {
					return m
				}
			}
			pk.stage = stBadNoisy
		case stBadNoisy:
			for pk.ni < len(pk.noisy) {
				m := pickNext(pk.noisy, pk.nScores, pk.ni)
				pk.ni++
				if m != pk.ttMove {
					return m
				}
			}
			pk.stage = stDone
		default:
			return NoMove
		}
	}
}

// generateQuietsInto gera só os lances que não são capturas nem promoções,
// na mesma ordem relativa de generateInto.
func (p *Position) generateQuietsInto(moves []Move) []Move {
	us, them := p.Side, p.Side^1
	empty := ^p.All
	fwd, startRank, promoRank := 8, 1, 6
	if us == Black {
		fwd, startRank, promoRank = -8, 6, 1
	}
	pawns := p.Pieces[us][Pawn]
	for pawns != 0 {
		from := popLSB(&pawns)
		rank := from / 8
		if rank == promoRank {
			continue
		}
		to := from + fwd
		if sqBB(to)&empty != 0 {
			moves = append(moves, NewMove(from, to, 0, 0))
			if rank == startRank && sqBB(to+fwd)&empty != 0 {
				moves = append(moves, NewMove(from, to+fwd, 0, FlagDouble))
			}
		}
	}
	for pt := Knight; pt <= King; pt++ {
		bb := p.Pieces[us][pt]
		for bb != 0 {
			from := popLSB(&bb)
			var att Bitboard
			switch pt {
			case Knight:
				att = knightAtt[from]
			case Bishop:
				att = bishopAttacks(from, p.All)
			case Rook:
				att = rookAttacks(from, p.All)
			case Queen:
				att = bishopAttacks(from, p.All) | rookAttacks(from, p.All)
			case King:
				att = kingAtt[from]
			}
			att &= empty
			for att != 0 {
				moves = append(moves, NewMove(from, popLSB(&att), 0, 0))
			}
		}
	}
	base := 0
	kSide, qSide := uint8(CastleWK), uint8(CastleWQ)
	if us == Black {
		base, kSide, qSide = 56, CastleBK, CastleBQ
	}
	e := base + 4
	if p.Castle&kSide != 0 && p.All&(sqBB(e+1)|sqBB(e+2)) == 0 &&
		!p.Attacked(e, them) && !p.Attacked(e+1, them) && !p.Attacked(e+2, them) {
		moves = append(moves, NewMove(e, e+2, 0, FlagCastle))
	}
	if p.Castle&qSide != 0 && p.All&(sqBB(e-1)|sqBB(e-2)|sqBB(e-3)) == 0 &&
		!p.Attacked(e, them) && !p.Attacked(e-1, them) && !p.Attacked(e-2, them) {
		moves = append(moves, NewMove(e, e-2, 0, FlagCastle))
	}
	return moves
}

// isPseudoLegal: generateInto geraria exatamente este lance nesta posição?
// Usado para testar o lance da TT antes de gerar a lista (pode vir de uma
// colisão de hash).
func (p *Position) isPseudoLegal(m Move) bool {
	if m == NoMove || m>>20 != 0 || m&(1<<15) != 0 {
		return false
	}
	from, to := m.From(), m.To()
	us, them := p.Side, p.Side^1
	pc := p.Board[from]
	if pc < 0 || int(pc)/6 != us || p.Occ[us]&sqBB(to) != 0 {
		return false
	}
	pt, promo := int(pc)%6, m.Promo()
	flags := m &^ 0xFFFF
	enemyTo := p.Occ[them]&sqBB(to) != 0

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
			return flags == 0 && p.All&sqBB(to) == 0
		case to == from+2*fwd:
			return flags == FlagDouble && rank == startRank && p.All&(sqBB(from+fwd)|sqBB(to)) == 0
		case pawnAtt[us][from]&sqBB(to) != 0:
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
		for _, c := range p.generateQuietsInto(make([]Move, 0, 64)) {
			if c == m {
				return true
			}
		}
		return false
	}
	var att Bitboard
	switch pt {
	case Knight:
		att = knightAtt[from]
	case Bishop:
		att = bishopAttacks(from, p.All)
	case Rook:
		att = rookAttacks(from, p.All)
	case Queen:
		att = bishopAttacks(from, p.All) | rookAttacks(from, p.All)
	case King:
		att = kingAtt[from]
	}
	if att&sqBB(to) == 0 {
		return false
	}
	if enemyTo {
		return flags == FlagCapture
	}
	return flags == 0
}
