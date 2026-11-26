package main

// Avaliação clássica parametrizada.
//
// Cada termo tem dois pesos: meio-jogo (MG) e final (EG). A nota final é
// interpolada pela fase do jogo. Todos os pesos ficam num único vetor
// (Params), o que permite ajustá-los automaticamente (tune.go) a partir de
// partidas reais: a avaliação é linear nos pesos, então basta registrar
// quantas vezes cada termo apareceu (o "trace").

type Pair struct{ MG, EG int }

var Params []Pair

// Deslocamentos de cada grupo de parâmetros dentro de Params.
var (
	oMaterial, oPST                     int
	oMobN, oMobB, oMobR, oMobQ          int
	oDoubled, oIsolated, oBackward      int
	oPassed, oPhalanx, oSupported       int
	oPassedKing, oPassedFree            int
	oRookOpen, oRookSemi, oRook7th      int
	oBishopPair, oKnightOutpost         int
	oKingAtt, oShield, oKingOpenFile    int
	oThreatPawn, oThreatMinor, oHanging int
	oTempo                              int
)

var paramNames []string

func alloc(name string, n int) int {
	off := len(Params)
	for i := 0; i < n; i++ {
		Params = append(Params, Pair{})
		paramNames = append(paramNames, name)
	}
	return off
}

func init() {
	oMaterial = alloc("material", 5)
	oPST = alloc("pst", 6*64)
	oMobN = alloc("mob_knight", 9)
	oMobB = alloc("mob_bishop", 14)
	oMobR = alloc("mob_rook", 15)
	oMobQ = alloc("mob_queen", 28)
	oDoubled = alloc("doubled", 1)
	oIsolated = alloc("isolated", 1)
	oBackward = alloc("backward", 1)
	oPassed = alloc("passed", 8)
	oPhalanx = alloc("phalanx", 8)
	oSupported = alloc("supported", 8)
	oPassedKing = alloc("passed_king_dist", 1)
	oPassedFree = alloc("passed_free_path", 8)
	oRookOpen = alloc("rook_open", 1)
	oRookSemi = alloc("rook_semiopen", 1)
	oRook7th = alloc("rook_7th", 1)
	oBishopPair = alloc("bishop_pair", 1)
	oKnightOutpost = alloc("knight_outpost", 1)
	oKingAtt = alloc("king_attack", 4)
	oShield = alloc("pawn_shield", 4)
	oKingOpenFile = alloc("king_open_file", 1)
	oThreatPawn = alloc("threat_by_pawn", 1)
	oThreatMinor = alloc("threat_by_minor", 1)
	oHanging = alloc("hanging", 1)
	oTempo = alloc("tempo", 1)

	// Valores iniciais: os mesmos da versão anterior; o resto começa em
	// zero e é preenchido pelo tuning (ou pelos valores de tuned_params.go).
	mat := [5]int{100, 320, 330, 500, 900}
	for pt := 0; pt < 5; pt++ {
		Params[oMaterial+pt] = Pair{mat[pt], mat[pt]}
		for sq := 0; sq < 64; sq++ {
			v := initPST[pt][sq]
			Params[oPST+pt*64+sq] = Pair{v, v}
		}
	}
	for sq := 0; sq < 64; sq++ {
		Params[oPST+King*64+sq] = Pair{initKingMid[sq], initKingEnd[sq]}
	}
	Params[oBishopPair] = Pair{30, 30}
	loadTunedParams()
}

// Máscaras auxiliares.
var (
	fileMask         [8]Bitboard
	adjFiles         [8]Bitboard
	forwardFile      [2][64]Bitboard // casas à frente na mesma coluna
	passedMask       [2][64]Bitboard // casas à frente nesta coluna e nas vizinhas
	attackSpanBehind [2][64]Bitboard // casas atrás/ao lado nas colunas vizinhas (peão atrasado)
)

func init() {
	for f := 0; f < 8; f++ {
		for r := 0; r < 8; r++ {
			fileMask[f] |= sqBB(r*8 + f)
		}
	}
	for f := 0; f < 8; f++ {
		if f > 0 {
			adjFiles[f] |= fileMask[f-1]
		}
		if f < 7 {
			adjFiles[f] |= fileMask[f+1]
		}
	}
	for sq := 0; sq < 64; sq++ {
		f, r := sq%8, sq/8
		for rr := r + 1; rr < 8; rr++ {
			forwardFile[White][sq] |= sqBB(rr*8 + f)
		}
		for rr := r - 1; rr >= 0; rr-- {
			forwardFile[Black][sq] |= sqBB(rr*8 + f)
		}
		passedMask[White][sq] = forwardFile[White][sq]
		passedMask[Black][sq] = forwardFile[Black][sq]
		for _, af := range []int{f - 1, f + 1} {
			if af < 0 || af > 7 {
				continue
			}
			for rr := r + 1; rr < 8; rr++ {
				passedMask[White][sq] |= sqBB(rr*8 + af)
			}
			for rr := r - 1; rr >= 0; rr-- {
				passedMask[Black][sq] |= sqBB(rr*8 + af)
			}
			for rr := r; rr >= 0; rr-- {
				attackSpanBehind[White][sq] |= sqBB(rr*8 + af)
			}
			for rr := r; rr < 8; rr++ {
				attackSpanBehind[Black][sq] |= sqBB(rr*8 + af)
			}
		}
	}
}

const (
	notFileA Bitboard = 0xfefefefefefefefe
	notFileH Bitboard = 0x7f7f7f7f7f7f7f7f
	rank7W   Bitboard = 0x00ff000000000000
	rank7B   Bitboard = 0x000000000000ff00
)

func pawnAttacksBB(c int, pawns Bitboard) Bitboard {
	if c == White {
		return (pawns&notFileA)<<7 | (pawns&notFileH)<<9
	}
	return (pawns&notFileA)>>9 | (pawns&notFileH)>>7
}

func relRank(c, sq int) int {
	if c == White {
		return sq / 8
	}
	return 7 - sq/8
}

func pstIndex(color, sq int) int {
	if color == White {
		return sq ^ 56
	}
	return sq
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func dist(a, b int) int {
	df, dr := absInt(a%8-b%8), absInt(a/8-b/8)
	if df > dr {
		return df
	}
	return dr
}

// Trace registra quantas vezes cada parâmetro contribuiu (brancas menos pretas).
type Trace struct {
	coef  map[int]int
	phase int
	scale int // fator do final, em 1/128
}

type evalState struct {
	mg, eg [2]int
	tr     *Trace
}

func (e *evalState) add(c, idx, n int) {
	if n == 0 {
		return
	}
	e.mg[c] += Params[idx].MG * n
	e.eg[c] += Params[idx].EG * n
	if e.tr != nil {
		if c == White {
			e.tr.coef[idx] += n
		} else {
			e.tr.coef[idx] -= n
		}
	}
}

var phaseInc = [6]int{0, 1, 1, 2, 4, 0}

// Evaluate devolve a nota em centipeões do ponto de vista de quem joga.
func Evaluate(p *Position) int {
	if v, ok := evalEndgame(p); ok {
		return v
	}
	return evaluateTrace(p, nil)
}

func evaluateTrace(p *Position, tr *Trace) int {
	e := evalState{tr: tr}
	phase := 0
	pawnAtt := [2]Bitboard{pawnAttacksBB(White, p.Pieces[White][Pawn]), pawnAttacksBB(Black, p.Pieces[Black][Pawn])}
	var attacks [2]Bitboard  // tudo o que cada lado ataca (para ameaças)
	var minorAtt [2]Bitboard // ataques de cavalos e bispos
	attacks[White], attacks[Black] = pawnAtt[White], pawnAtt[Black]
	kingSq := [2]int{lsb(p.Pieces[White][King]), lsb(p.Pieces[Black][King])}

	for c := White; c <= Black; c++ {
		them := c ^ 1
		own := &p.Pieces[c]
		myPawns, theirPawns := own[Pawn], p.Pieces[them][Pawn]
		mobArea := ^(p.Occ[c] | pawnAtt[them])
		kingZone := kingAtt[kingSq[them]] | sqBB(kingSq[them])

		for pt := Pawn; pt <= King; pt++ {
			bb := own[pt]
			phase += phaseInc[pt] * bb.Count()
			if pt != King {
				e.add(c, oMaterial+pt, bb.Count())
			}
			for bb != 0 {
				sq := popLSB(&bb)
				e.add(c, oPST+pt*64+pstIndex(c, sq), 1)

				var att Bitboard
				switch pt {
				case Knight:
					att = knightAtt[sq]
					e.add(c, oMobN+(att&mobArea).Count(), 1)
					// Posto avançado: protegido por peão e sem peões inimigos que possam expulsá-lo.
					if rr := relRank(c, sq); rr >= 3 && rr <= 5 && pawnAtt[c]&sqBB(sq) != 0 &&
						passedMask[c][sq]&^fileMask[sq%8]&theirPawns == 0 {
						e.add(c, oKnightOutpost, 1)
					}
				case Bishop:
					att = bishopAttacks(sq, p.All)
					e.add(c, oMobB+(att&mobArea).Count(), 1)
				case Rook:
					att = rookAttacks(sq, p.All)
					e.add(c, oMobR+(att&mobArea).Count(), 1)
					f := fileMask[sq%8]
					if f&myPawns == 0 {
						if f&theirPawns == 0 {
							e.add(c, oRookOpen, 1)
						} else {
							e.add(c, oRookSemi, 1)
						}
					}
					if relRank(c, sq) == 6 && relRank(c, kingSq[them]) == 7 {
						e.add(c, oRook7th, 1)
					}
				case Queen:
					att = bishopAttacks(sq, p.All) | rookAttacks(sq, p.All)
					e.add(c, oMobQ+(att&mobArea).Count(), 1)
				case King:
					att = kingAtt[sq]
				case Pawn:
					evalPawn(&e, p, c, sq, kingSq)
				}
				if pt >= Knight && pt <= Queen {
					e.add(c, oKingAtt+pt-1, (att & kingZone).Count())
				}
				if pt == Knight || pt == Bishop {
					minorAtt[c] |= att
				}
				attacks[c] |= att
			}
		}
		if own[Bishop].Count() >= 2 {
			e.add(c, oBishopPair, 1)
		}

		// Escudo de peões e colunas abertas perto do rei.
		ks := kingSq[c]
		if relRank(c, ks) <= 1 {
			n := 0
			for f := ks%8 - 1; f <= ks%8+1; f++ {
				if f < 0 || f > 7 {
					continue
				}
				front := fileMask[f] & passedMask[c][ks] // à frente do rei
				for r := 1; r <= 2; r++ {
					rr := ks/8 + r
					if c == Black {
						rr = ks/8 - r
					}
					if rr >= 0 && rr < 8 && myPawns&front&sqBB(rr*8+f) != 0 {
						n++
						break
					}
				}
				if fileMask[f]&myPawns == 0 {
					e.add(c, oKingOpenFile, 1)
				}
			}
			e.add(c, oShield+n, 1)
		}
	}

	// Ameaças (precisam dos ataques dos dois lados).
	for c := White; c <= Black; c++ {
		them := c ^ 1
		theirs := &p.Pieces[them]
		nonPawn := theirs[Knight] | theirs[Bishop] | theirs[Rook] | theirs[Queen]
		e.add(c, oThreatPawn, (pawnAtt[c] & nonPawn).Count())
		e.add(c, oThreatMinor, (minorAtt[c] & (theirs[Rook] | theirs[Queen])).Count())
		// Peças atacadas e não defendidas.
		e.add(c, oHanging, (attacks[c] & p.Occ[them] &^ attacks[them] &^ theirs[King]).Count())
	}

	e.add(p.Side, oTempo, 1)

	if phase > 24 {
		phase = 24
	}
	scale := endgameScale(p)
	if tr != nil {
		tr.phase, tr.scale = phase, scale
	}
	mg := e.mg[White] - e.mg[Black]
	eg := (e.eg[White] - e.eg[Black]) * scale / 128
	v := (mg*phase + eg*(24-phase)) / 24
	if p.Side == Black {
		return -v
	}
	return v
}

func evalPawn(e *evalState, p *Position, c, sq int, kingSq [2]int) {
	them := c ^ 1
	myPawns, theirPawns := p.Pieces[c][Pawn], p.Pieces[them][Pawn]
	f, rr := sq%8, relRank(c, sq)

	if forwardFile[c][sq]&myPawns != 0 {
		e.add(c, oDoubled, 1)
	}
	if adjFiles[f]&myPawns == 0 {
		e.add(c, oIsolated, 1)
	} else if attackSpanBehind[c][sq]&myPawns == 0 {
		// Nenhum peão vizinho atrás para apoiar e a casa da frente é controlada por peão inimigo.
		stop := sq + 8
		if c == Black {
			stop = sq - 8
		}
		if stop >= 0 && stop < 64 && pawnAttacksBB(them, theirPawns)&sqBB(stop) != 0 {
			e.add(c, oBackward, 1)
		}
	}
	if pawnAtt[them][sq]&myPawns != 0 { // pawnAtt[them][sq] = casas de onde um peão nosso defende sq
		e.add(c, oSupported+rr, 1)
	}
	if (sq%8 > 0 && myPawns&sqBB(sq-1) != 0) || (sq%8 < 7 && myPawns&sqBB(sq+1) != 0) {
		e.add(c, oPhalanx+rr, 1)
	}
	if passedMask[c][sq]&theirPawns == 0 && forwardFile[c][sq]&myPawns == 0 {
		e.add(c, oPassed+rr, 1)
		promo := f + 56
		if c == Black {
			promo = f
		}
		e.add(c, oPassedKing, dist(kingSq[them], promo)-dist(kingSq[c], promo))
		if forwardFile[c][sq]&p.All == 0 {
			e.add(c, oPassedFree+rr, 1)
		}
	}
}
