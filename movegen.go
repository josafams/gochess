package main

// Move compacta um lance em 32 bits:
// bits 0-5 origem | 6-11 destino | 12-14 promoção (0 = nenhuma) | 16+ flags
type Move uint32

const NoMove Move = 0

const (
	FlagCapture Move = 1 << (16 + iota)
	FlagEP
	FlagCastle
	FlagDouble
)

func NewMove(from, to, promo int, flags Move) Move {
	return Move(from) | Move(to)<<6 | Move(promo)<<12 | flags
}

func (m Move) From() int  { return int(m & 63) }
func (m Move) To() int    { return int(m>>6) & 63 }
func (m Move) Promo() int { return int(m>>12) & 7 }

// String usa a notação UCI: e2e4, e7e8q.
func (m Move) String() string {
	if m == NoMove {
		return "0000"
	}
	s := sqName(m.From()) + sqName(m.To())
	if pr := m.Promo(); pr != 0 {
		s += string("pnbrqk"[pr])
	}
	return s
}

// GenerateMoves gera lances pseudo-legais (podem deixar o próprio rei em xeque;
// isso é verificado em Make). Com capturesOnly, gera só capturas e promoções,
// usado na quiescence search.
func (p *Position) GenerateMoves(capturesOnly bool) []Move {
	return p.generateInto(make([]Move, 0, 48), capturesOnly)
}

// generateInto acrescenta os lances a moves (a busca passa um buffer por ply,
// para não alocar memória a cada nó).
func (p *Position) generateInto(moves []Move, capturesOnly bool) []Move {
	us, them := p.Side, p.Side^1
	enemy := p.Occ[them]
	empty := ^p.All

	addPromos := func(from, to int, flags Move) {
		for pr := Queen; pr >= Knight; pr-- {
			moves = append(moves, NewMove(from, to, pr, flags))
		}
	}

	// Peões
	fwd, startRank, promoRank := 8, 1, 6
	if us == Black {
		fwd, startRank, promoRank = -8, 6, 1
	}
	pawns := p.Pieces[us][Pawn]
	for pawns != 0 {
		from := popLSB(&pawns)
		rank := from / 8
		promo := rank == promoRank

		to := from + fwd
		if sqBB(to)&empty != 0 {
			if promo {
				addPromos(from, to, 0)
			} else if !capturesOnly {
				moves = append(moves, NewMove(from, to, 0, 0))
				if rank == startRank && sqBB(to+fwd)&empty != 0 {
					moves = append(moves, NewMove(from, to+fwd, 0, FlagDouble))
				}
			}
		}

		caps := pawnAtt[us][from] & enemy
		for caps != 0 {
			to := popLSB(&caps)
			if promo {
				addPromos(from, to, FlagCapture)
			} else {
				moves = append(moves, NewMove(from, to, 0, FlagCapture))
			}
		}

		if p.EP >= 0 && pawnAtt[us][from]&sqBB(p.EP) != 0 {
			moves = append(moves, NewMove(from, p.EP, 0, FlagCapture|FlagEP))
		}
	}

	// Demais peças
	targets := ^p.Occ[us]
	if capturesOnly {
		targets = enemy
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
			att &= targets
			for att != 0 {
				to := popLSB(&att)
				var flags Move
				if sqBB(to)&enemy != 0 {
					flags = FlagCapture
				}
				moves = append(moves, NewMove(from, to, 0, flags))
			}
		}
	}

	// Roque: casas entre rei e torre vazias e o rei não pode passar por casa atacada.
	if !capturesOnly {
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
	}
	return moves
}

// castleMask: mexer (ou capturar) em a1, e1, h1, a8, e8, h8 remove direitos de roque.
var castleMask [64]uint8

func init() {
	for i := range castleMask {
		castleMask[i] = 15
	}
	castleMask[0] &^= CastleWQ
	castleMask[7] &^= CastleWK
	castleMask[4] &^= CastleWK | CastleWQ
	castleMask[56] &^= CastleBQ
	castleMask[63] &^= CastleBK
	castleMask[60] &^= CastleBK | CastleBQ
}

// Make devolve a nova posição e se o lance é legal (o rei de quem jogou não
// ficou em xeque).
func (p *Position) Make(m Move) (Position, bool) {
	n := *p
	us, them := n.Side, n.Side^1
	from, to := m.From(), m.To()
	pt := int(n.Board[from]) % 6

	// Tira do hash o estado que vai mudar; recoloca no fim.
	n.Hash ^= castleKeys[n.Castle]
	if n.EP >= 0 {
		n.Hash ^= epKeys[n.EP%8]
	}

	n.Halfmove++
	if pt == Pawn || m&FlagCapture != 0 {
		n.Halfmove = 0
	}

	if m&FlagEP != 0 {
		if us == White {
			n.remove(to - 8)
		} else {
			n.remove(to + 8)
		}
	} else if n.Board[to] >= 0 {
		n.remove(to)
	}

	n.remove(from)
	if pr := m.Promo(); pr != 0 {
		pt = pr
	}
	n.put(us, pt, to)

	if m&FlagCastle != 0 {
		switch to {
		case 6:
			n.remove(7)
			n.put(us, Rook, 5)
		case 2:
			n.remove(0)
			n.put(us, Rook, 3)
		case 62:
			n.remove(63)
			n.put(us, Rook, 61)
		case 58:
			n.remove(56)
			n.put(us, Rook, 59)
		}
	}

	n.EP = -1
	if m&FlagDouble != 0 {
		n.EP = (from + to) / 2
	}
	n.Castle &= castleMask[from] & castleMask[to]
	n.Hash ^= castleKeys[n.Castle]
	if n.EP >= 0 {
		n.Hash ^= epKeys[n.EP%8]
	}
	if us == Black {
		n.Fullmove++
	}
	n.Side = them
	n.Hash ^= sideKey

	return n, !n.Attacked(lsb(n.Pieces[us][King]), them)
}

// MakeNull passa a vez sem mexer peça (usado no null move pruning).
func (p *Position) MakeNull() Position {
	n := *p
	if n.EP >= 0 {
		n.Hash ^= epKeys[n.EP%8]
		n.EP = -1
	}
	n.Side ^= 1
	n.Hash ^= sideKey
	n.Halfmove = 0 // impede detectar "repetição" atravessando o lance nulo
	return n
}

func (p *Position) LegalMoves() []Move {
	var legal []Move
	for _, m := range p.GenerateMoves(false) {
		if _, ok := p.Make(m); ok {
			legal = append(legal, m)
		}
	}
	return legal
}

// Perft conta as folhas da árvore de lances até a profundidade dada.
// É o teste padrão para validar um gerador de lances.
func Perft(p *Position, depth int) uint64 {
	if depth == 0 {
		return 1
	}
	var nodes uint64
	for _, m := range p.GenerateMoves(false) {
		child, ok := p.Make(m)
		if ok {
			nodes += Perft(&child, depth-1)
		}
	}
	return nodes
}

// SAN devolve o lance em notação algébrica (Nf3, exd5, O-O, e8=Q+).
func (p *Position) SAN(m Move) string {
	from, to := m.From(), m.To()
	var s string
	if m&FlagCastle != 0 {
		s = "O-O"
		if to%8 == 2 {
			s = "O-O-O"
		}
	} else {
		pt := int(p.Board[from]) % 6
		if pt != Pawn {
			s = string("PNBRQK"[pt])
			// Desambiguação: outra peça igual pode ir para a mesma casa?
			sameFile, sameRank, other := false, false, false
			for _, o := range p.LegalMoves() {
				if o.To() == to && o.From() != from && int(p.Board[o.From()])%6 == pt {
					other = true
					sameFile = sameFile || o.From()%8 == from%8
					sameRank = sameRank || o.From()/8 == from/8
				}
			}
			if other {
				switch {
				case !sameFile:
					s += sqName(from)[:1]
				case !sameRank:
					s += sqName(from)[1:]
				default:
					s += sqName(from)
				}
			}
		} else if m&FlagCapture != 0 {
			s = sqName(from)[:1]
		}
		if m&FlagCapture != 0 {
			s += "x"
		}
		s += sqName(to)
		if pr := m.Promo(); pr != 0 {
			s += "=" + string("PNBRQK"[pr])
		}
	}
	if child, ok := p.Make(m); ok && child.InCheck() {
		if len(child.LegalMoves()) == 0 {
			s += "#"
		} else {
			s += "+"
		}
	}
	return s
}

// ParseUCIMove encontra o lance legal correspondente a "e2e4", "e7e8q" etc.
func (p *Position) ParseUCIMove(s string) (Move, bool) {
	for _, m := range p.LegalMoves() {
		if m.String() == s {
			return m, true
		}
	}
	return NoMove, false
}
