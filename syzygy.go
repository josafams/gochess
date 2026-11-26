package main

import "gochess/fathom"

// Tablebases Syzygy: resultado perfeito (vitória, empate ou derrota) em finais
// com poucas peças. Na busca, a tabela WDL dá a nota exata de qualquer posição
// coberta; na raiz, os lances que pioram o resultado são descartados antes de
// buscar.

// TBWin fica logo abaixo das notas de mate: uma vitória de tablebase é certa,
// mas a distância até o mate não é conhecida.
const TBWin = Mate - 2*MaxPly - 1

var tbLargest int // maior número de peças coberto (0 = sem tablebases)

func tbSupported() bool { return fathom.Available }

func setSyzygyPath(path string) int {
	tbLargest = fathom.Init(path)
	return tbLargest
}

func tbPosition(p *Position) fathom.Position {
	both := func(pt int) uint64 { return uint64(p.Pieces[White][pt] | p.Pieces[Black][pt]) }
	ep := uint(0)
	if p.EP >= 0 {
		ep = uint(p.EP)
	}
	return fathom.Position{
		White: uint64(p.Occ[White]), Black: uint64(p.Occ[Black]),
		Kings: both(King), Queens: both(Queen), Rooks: both(Rook),
		Bishops: both(Bishop), Knights: both(Knight), Pawns: both(Pawn),
		Rule50: uint(p.Halfmove), EP: ep, WhiteToMove: p.Side == White,
	}
}

// tbProbe consulta a tabela WDL no meio da busca. Só vale sem direito de roque
// e logo após um lance irreversível (captura ou peão), como exige o formato.
func (t *thread) tbProbe(p *Position, ply int) (score, flag int, ok bool) {
	if tbLargest == 0 || p.Castle != 0 || p.Halfmove != 0 || p.All.Count() > tbLargest {
		return 0, 0, false
	}
	fp := tbPosition(p)
	r := fathom.ProbeWDL(&fp)
	if r == fathom.Failed {
		return 0, 0, false
	}
	t.s.tbHits.Add(1)
	switch r {
	case fathom.Win:
		return TBWin - ply, ttLower, true
	case fathom.Loss:
		return -TBWin + ply, ttUpper, true
	case fathom.CursedWin: // vitória que a regra dos 50 lances transforma em empate
		return 1, ttExact, true
	case fathom.BlessedLoss:
		return -1, ttExact, true
	}
	return 0, ttExact, true
}

var tbPromo = [5]int{0, Queen, Rook, Bishop, Knight}

// tbRootFilter devolve só os lances da raiz que mantêm o melhor resultado
// possível segundo a tablebase (nil se a posição não estiver coberta).
func (s *Searcher) tbRootFilter(p *Position, moves []Move) []Move {
	if tbLargest == 0 || p.Castle != 0 || p.All.Count() > tbLargest {
		return nil
	}
	fp := tbPosition(p)
	list, ok := fathom.ProbeRoot(&fp)
	if !ok || len(list) == 0 {
		return nil
	}
	best := -1
	for _, rm := range list {
		best = max(best, rm.WDL)
	}
	var out []Move
	for _, rm := range list {
		if rm.WDL != best || rm.Promo > 4 {
			continue
		}
		for _, m := range moves {
			if m.From() == rm.From && m.To() == rm.To && m.Promo() == tbPromo[rm.Promo] {
				out = append(out, m)
			}
		}
	}
	s.tbHits.Add(1)
	return out
}
