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
			t.scoreQuiets(p, pk.quiets, pk.qScores, ply)
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
