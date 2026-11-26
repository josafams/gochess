package eval

import (
	"gochess/internal/chess"
)

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

var ParamNames []string

func alloc(name string, n int) int {
	off := len(Params)
	for i := 0; i < n; i++ {
		Params = append(Params, Pair{})
		ParamNames = append(ParamNames, name)
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
		Params[oPST+chess.King*64+sq] = Pair{initKingMid[sq], initKingEnd[sq]}
	}
	Params[oBishopPair] = Pair{30, 30}
	loadTunedParams()
}

// Máscaras auxiliares.
var (
	fileMask         [8]chess.Bitboard
	adjFiles         [8]chess.Bitboard
	forwardFile      [2][64]chess.Bitboard // casas à frente na mesma coluna
	passedMask       [2][64]chess.Bitboard // casas à frente nesta coluna e nas vizinhas
	attackSpanBehind [2][64]chess.Bitboard // casas atrás/ao lado nas colunas vizinhas (peão atrasado)
)

func init() {
	for f := 0; f < 8; f++ {
		for r := 0; r < 8; r++ {
			fileMask[f] |= chess.SqBB(r*8 + f)
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
			forwardFile[chess.White][sq] |= chess.SqBB(rr*8 + f)
		}
		for rr := r - 1; rr >= 0; rr-- {
			forwardFile[chess.Black][sq] |= chess.SqBB(rr*8 + f)
		}
		passedMask[chess.White][sq] = forwardFile[chess.White][sq]
		passedMask[chess.Black][sq] = forwardFile[chess.Black][sq]
		for _, af := range []int{f - 1, f + 1} {
			if af < 0 || af > 7 {
				continue
			}
			for rr := r + 1; rr < 8; rr++ {
				passedMask[chess.White][sq] |= chess.SqBB(rr*8 + af)
			}
			for rr := r - 1; rr >= 0; rr-- {
				passedMask[chess.Black][sq] |= chess.SqBB(rr*8 + af)
			}
			for rr := r; rr >= 0; rr-- {
				attackSpanBehind[chess.White][sq] |= chess.SqBB(rr*8 + af)
			}
			for rr := r; rr < 8; rr++ {
				attackSpanBehind[chess.Black][sq] |= chess.SqBB(rr*8 + af)
			}
		}
	}
}

const (
	notFileA chess.Bitboard = 0xfefefefefefefefe
	notFileH chess.Bitboard = 0x7f7f7f7f7f7f7f7f
	rank7W   chess.Bitboard = 0x00ff000000000000
	rank7B   chess.Bitboard = 0x000000000000ff00
)

func pawnAttacksBB(c int, pawns chess.Bitboard) chess.Bitboard {
	if c == chess.White {
		return (pawns&notFileA)<<7 | (pawns&notFileH)<<9
	}
	return (pawns&notFileA)>>9 | (pawns&notFileH)>>7
}

func relRank(c, sq int) int {
	if c == chess.White {
		return sq / 8
	}
	return 7 - sq/8
}

func pstIndex(color, sq int) int {
	if color == chess.White {
		return sq ^ 56
	}
	return sq
}

func Abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func Dist(a, b int) int {
	df, dr := Abs(a%8-b%8), Abs(a/8-b/8)
	if df > dr {
		return df
	}
	return dr
}

// Trace registra quantas vezes cada parâmetro contribuiu (brancas menos pretas).
type Trace struct {
	Coef  map[int]int
	Phase int
	Scale int // fator do final, em 1/128
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
		if c == chess.White {
			e.tr.Coef[idx] += n
		} else {
			e.tr.Coef[idx] -= n
		}
	}
}

var phaseInc = [6]int{0, 1, 1, 2, 4, 0}

// Evaluate devolve a nota em centipeões do ponto de vista de quem joga.
func Evaluate(p *chess.Position) int {
	if v, ok := EvalEndgame(p); ok {
		return v
	}
	return EvaluateTrace(p, nil)
}

func EvaluateTrace(p *chess.Position, tr *Trace) int {
	e := evalState{tr: tr}
	phase := 0
	pawnAtt := [2]chess.Bitboard{pawnAttacksBB(chess.White, p.Pieces[chess.White][chess.Pawn]), pawnAttacksBB(chess.Black, p.Pieces[chess.Black][chess.Pawn])}
	var attacks [2]chess.Bitboard  // tudo o que cada lado ataca (para ameaças)
	var minorAtt [2]chess.Bitboard // ataques de cavalos e bispos
	attacks[chess.White], attacks[chess.Black] = pawnAtt[chess.White], pawnAtt[chess.Black]
	kingSq := [2]int{chess.LSB(p.Pieces[chess.White][chess.King]), chess.LSB(p.Pieces[chess.Black][chess.King])}

	for c := chess.White; c <= chess.Black; c++ {
		them := c ^ 1
		own := &p.Pieces[c]
		myPawns, theirPawns := own[chess.Pawn], p.Pieces[them][chess.Pawn]
		mobArea := ^(p.Occ[c] | pawnAtt[them])
		kingZone := chess.KingAtt[kingSq[them]] | chess.SqBB(kingSq[them])

		for pt := chess.Pawn; pt <= chess.King; pt++ {
			bb := own[pt]
			phase += phaseInc[pt] * bb.Count()
			if pt != chess.King {
				e.add(c, oMaterial+pt, bb.Count())
			}
			for bb != 0 {
				sq := chess.PopLSB(&bb)
				e.add(c, oPST+pt*64+pstIndex(c, sq), 1)

				var att chess.Bitboard
				switch pt {
				case chess.Knight:
					att = chess.KnightAtt[sq]
					e.add(c, oMobN+(att&mobArea).Count(), 1)
					// Posto avançado: protegido por peão e sem peões inimigos que possam expulsá-lo.
					if rr := relRank(c, sq); rr >= 3 && rr <= 5 && pawnAtt[c]&chess.SqBB(sq) != 0 &&
						passedMask[c][sq]&^fileMask[sq%8]&theirPawns == 0 {
						e.add(c, oKnightOutpost, 1)
					}
				case chess.Bishop:
					att = chess.BishopAttacks(sq, p.All)
					e.add(c, oMobB+(att&mobArea).Count(), 1)
				case chess.Rook:
					att = chess.RookAttacks(sq, p.All)
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
				case chess.Queen:
					att = chess.BishopAttacks(sq, p.All) | chess.RookAttacks(sq, p.All)
					e.add(c, oMobQ+(att&mobArea).Count(), 1)
				case chess.King:
					att = chess.KingAtt[sq]
				case chess.Pawn:
					evalPawn(&e, p, c, sq, kingSq)
				}
				if pt >= chess.Knight && pt <= chess.Queen {
					e.add(c, oKingAtt+pt-1, (att & kingZone).Count())
				}
				if pt == chess.Knight || pt == chess.Bishop {
					minorAtt[c] |= att
				}
				attacks[c] |= att
			}
		}
		if own[chess.Bishop].Count() >= 2 {
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
					if c == chess.Black {
						rr = ks/8 - r
					}
					if rr >= 0 && rr < 8 && myPawns&front&chess.SqBB(rr*8+f) != 0 {
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
	for c := chess.White; c <= chess.Black; c++ {
		them := c ^ 1
		theirs := &p.Pieces[them]
		nonPawn := theirs[chess.Knight] | theirs[chess.Bishop] | theirs[chess.Rook] | theirs[chess.Queen]
		e.add(c, oThreatPawn, (pawnAtt[c] & nonPawn).Count())
		e.add(c, oThreatMinor, (minorAtt[c] & (theirs[chess.Rook] | theirs[chess.Queen])).Count())
		// Peças atacadas e não defendidas.
		e.add(c, oHanging, (attacks[c] & p.Occ[them] &^ attacks[them] &^ theirs[chess.King]).Count())
	}

	e.add(p.Side, oTempo, 1)

	if phase > 24 {
		phase = 24
	}
	scale := endgameScale(p)
	if tr != nil {
		tr.Phase, tr.Scale = phase, scale
	}
	mg := e.mg[chess.White] - e.mg[chess.Black]
	eg := (e.eg[chess.White] - e.eg[chess.Black]) * scale / 128
	v := (mg*phase + eg*(24-phase)) / 24
	if p.Side == chess.Black {
		return -v
	}
	return v
}

func evalPawn(e *evalState, p *chess.Position, c, sq int, kingSq [2]int) {
	them := c ^ 1
	myPawns, theirPawns := p.Pieces[c][chess.Pawn], p.Pieces[them][chess.Pawn]
	f, rr := sq%8, relRank(c, sq)

	if forwardFile[c][sq]&myPawns != 0 {
		e.add(c, oDoubled, 1)
	}
	if adjFiles[f]&myPawns == 0 {
		e.add(c, oIsolated, 1)
	} else if attackSpanBehind[c][sq]&myPawns == 0 {
		// Nenhum peão vizinho atrás para apoiar e a casa da frente é controlada por peão inimigo.
		stop := sq + 8
		if c == chess.Black {
			stop = sq - 8
		}
		if stop >= 0 && stop < 64 && pawnAttacksBB(them, theirPawns)&chess.SqBB(stop) != 0 {
			e.add(c, oBackward, 1)
		}
	}
	if chess.PawnAtt[them][sq]&myPawns != 0 { // pawnAtt[them][sq] = casas de onde um peão nosso defende sq
		e.add(c, oSupported+rr, 1)
	}
	if (sq%8 > 0 && myPawns&chess.SqBB(sq-1) != 0) || (sq%8 < 7 && myPawns&chess.SqBB(sq+1) != 0) {
		e.add(c, oPhalanx+rr, 1)
	}
	if passedMask[c][sq]&theirPawns == 0 && forwardFile[c][sq]&myPawns == 0 {
		e.add(c, oPassed+rr, 1)
		promo := f + 56
		if c == chess.Black {
			promo = f
		}
		e.add(c, oPassedKing, Dist(kingSq[them], promo)-Dist(kingSq[c], promo))
		if forwardFile[c][sq]&p.All == 0 {
			e.add(c, oPassedFree+rr, 1)
		}
	}
}
