package search

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"gochess/internal/chess"
	"gochess/internal/eval"
)

const (
	Inf  = 100000
	Mate = 50000
)

type Limits struct {
	Depth     int
	Nodes     uint64
	Mate      int // procurar mate em N lances
	MoveTime  time.Duration
	WTime     time.Duration
	BTime     time.Duration
	WInc      time.Duration
	BInc      time.Duration
	MovesToGo int
	Infinite  bool
	Ponder    bool
}

// Line é uma variante (para MultiPV).
type Line struct {
	Move  chess.Move
	Score int
	PV    []chess.Move
}

// Result é o resumo da última iteração completa.
type Result struct {
	Best  chess.Move
	Score int // do ponto de vista de quem joga
	Depth int
	Nodes uint64
	Time  time.Duration
	PV    []chess.Move
	Lines []Line
}

// Searcher guarda o que é compartilhado entre as threads.
type Searcher struct {
	tt      *TT
	Threads int
	MultiPV int
	UseNNUE bool
	workers []*thread

	stop     atomic.Bool
	timed    atomic.Bool
	deadline atomic.Int64 // unix nano: limite duro
	soft     atomic.Int64 // unix nano: não começar nova iteração
	budget   time.Duration
	// Gestão de tempo com relógio: o tempo "ideal" (softBase) é ajustado a
	// cada iteração pela estabilidade do melhor lance e pela queda da nota.
	clockMode   bool
	softBase    time.Duration
	maxTime     time.Duration
	searchStart atomic.Int64 // unix nano
	nodeLimit   uint64
	tbHits      atomic.Uint64 // consultas às tablebases Syzygy
	totalNodes  atomic.Uint64
}

// thread guarda o estado de cada thread de busca (Lazy SMP).
type thread struct {
	s        *Searcher
	id       int
	nodes    uint64
	killers  [chess.MaxPly + 2][2]chess.Move
	hist     [2][64][64]int16             // int16: a gravidade mantém em ±16384; tabelas menores cabem no cache
	capHist  [12][64][6]int16             // [peça][destino][vítima]
	cont     [768][768]int16              // [peça*64+destino anterior][peça*64+destino]
	played   [chess.MaxPly + 2]int        // peça*64+destino do lance feito em cada ply (-1 = null)
	excluded [chess.MaxPly + 2]chess.Move // lance excluído na busca de singularidade deste ply
	path     []uint64
	nn       *eval.NNUEStack
	mbuf     [chess.MaxPly + 2][256]chess.Move // lista de lances de cada ply
	stack    [chess.MaxPly + 2]chess.Position  // posição de cada ply (evita alocar no heap)
	sbuf     [chess.MaxPly + 2][256]int        // notas de ordenação de cada ply
	qbuf     [chess.MaxPly + 2][256]chess.Move // lances quietos (geração em etapas)
	qsbuf    [chess.MaxPly + 2][256]int
	evals    [chess.MaxPly + 2]int            // eval estática de cada ply (evalNone em xeque)
	seeM     [chess.MaxPly + 2][32]chess.Move // SEE já calculados no nó de cada ply
	seeV     [chess.MaxPly + 2][32]int
	seeN     [chess.MaxPly + 2]int
}

func NewSearcher(hashMB int) *Searcher {
	s := &Searcher{tt: NewTT(hashMB), Threads: 1, MultiPV: 1}
	return s
}

func (s *Searcher) SetHash(mb int) { s.tt = NewTT(mb) }

func (s *Searcher) Clear() {
	s.tt.Clear()
	s.workers = nil
}

func (s *Searcher) Stop() { s.stop.Store(true) }

// ResetStop deve ser chamado antes de cada busca nova.
func (s *Searcher) ResetStop() { s.stop.Store(false) }

// PonderHit: o adversário jogou o lance previsto; passa a contar o tempo.
func (s *Searcher) PonderHit() {
	now := time.Now()
	if s.budget > 0 {
		s.searchStart.Store(now.UnixNano())
		s.deadline.Store(now.Add(s.hardBudget()).UnixNano())
		s.soft.Store(now.Add(s.budget / 2).UnixNano())
		s.timed.Store(true)
	}
}

var lmrTable [64][64]int

func init() {
	for d := 1; d < 64; d++ {
		for i := 1; i < 64; i++ {
			lmrTable[d][i] = int(0.8 + math.Log(float64(d))*math.Log(float64(i))/2.2)
		}
	}
}

func (t *thread) push(h uint64) { t.path = append(t.path, h) }
func (t *thread) pop()          { t.path = t.path[:len(t.path)-1] }

func (t *thread) isRepetition(halfmove int) bool {
	n := len(t.path)
	cur := t.path[n-1]
	for i := n - 3; i >= 0 && i >= n-1-halfmove; i -= 2 {
		if t.path[i] == cur {
			return true
		}
	}
	return false
}

func (t *thread) setupNN(p *chess.Position) {
	t.nn.Reset(p)
	if t.s.UseNNUE && eval.CurrentNet != nil {
		t.nn.Enable(eval.CurrentNet, p)
	}
}

func (t *thread) evaluate(p *chess.Position, ply int) int {
	if t.s.UseNNUE && eval.CurrentNet != nil {
		if v, ok := eval.EvalEndgame(p); ok {
			return v
		}
		return t.nn.Evaluate(p, ply)
	}
	return eval.Evaluate(p)
}

// see: SEE do lance, guardado por nó (a ordenação, as podas e o LMR pedem o
// mesmo valor várias vezes).
func (t *thread) see(p *chess.Position, m chess.Move, ply int) int {
	n := t.seeN[ply]
	ms := &t.seeM[ply]
	for i := 0; i < n; i++ {
		if ms[i] == m {
			return t.seeV[ply][i]
		}
	}
	v := p.SEE(m)
	if n < len(ms) {
		ms[n], t.seeV[ply][n] = m, v
		t.seeN[ply] = n + 1
	}
	return v
}

// Ordenação: lance da TT > promoção a dama > boas capturas (MVV-LVA)
// > killers > lances quietos pelo histórico > capturas perdedoras.
func (t *thread) scoreMove(p *chess.Position, m chess.Move, ply int, first chess.Move) int {
	if m == first {
		return 1_000_000
	}
	if m.Promo() == chess.Queen {
		return 400_000
	}
	if m&chess.FlagCapture != 0 {
		victim := chess.Pawn
		if m&chess.FlagEP == 0 {
			victim = int(p.Board[m.To()]) % 6
		}
		pc := int(p.Board[m.From()])
		mvv := victim*1000 + int(t.capHist[pc][m.To()][victim])/8
		if t.see(p, m, ply) >= 0 {
			return 300_000 + mvv
		}
		return -300_000 + mvv
	}
	if m.Promo() != 0 {
		return -400_000 // sub-promoções por último
	}
	if m == t.killers[ply][0] {
		return 200_000
	}
	if m == t.killers[ply][1] {
		return 190_000
	}
	return t.quietScore(p, m, ply)
}

// quietScore: histórico do lance + histórico de continuação (o que costuma
// ser bom depois do nosso lance anterior e do lance anterior do adversário).
func (t *thread) quietScore(p *chess.Position, m chess.Move, ply int) int {
	key := int(p.Board[m.From()])*64 + m.To()
	v := int(t.hist[p.Side][m.From()][m.To()])
	if ply >= 1 && t.played[ply-1] >= 0 {
		v += int(t.cont[t.played[ply-1]][key])
	}
	if ply >= 2 && t.played[ply-2] >= 0 {
		v += int(t.cont[t.played[ply-2]][key])
	}
	return v
}

// scoreQuiets pontua uma lista só de lances quietos (sem promoções): killers e,
// depois, histórico + continuação. Igual a scoreMove para esses lances, mas lê as
// linhas da tabela de continuação uma vez por nó, não uma vez por lance.
func (t *thread) scoreQuiets(p *chess.Position, moves []chess.Move, scores []int, ply int) {
	var r1, r2 *[768]int16
	if ply >= 1 && t.played[ply-1] >= 0 {
		r1 = &t.cont[t.played[ply-1]]
	}
	if ply >= 2 && t.played[ply-2] >= 0 {
		r2 = &t.cont[t.played[ply-2]]
	}
	h := &t.hist[p.Side]
	k0, k1 := t.killers[ply][0], t.killers[ply][1]
	for i, m := range moves {
		switch m {
		case k0:
			scores[i] = 200_000
			continue
		case k1:
			scores[i] = 190_000
			continue
		}
		from, to := m.From(), m.To()
		key := int(p.Board[from])*64 + to
		v := int(h[from][to])
		if r1 != nil {
			v += int(r1[key])
		}
		if r2 != nil {
			v += int(r2[key])
		}
		scores[i] = v
	}
}

// gravity: soma o bônus mantendo o valor em [-limit, limit].
func gravity(v, bonus, limit int) int {
	return v + bonus - v*eval.Abs(bonus)/limit
}

func (t *thread) updateQuiet(p *chess.Position, m chess.Move, ply, bonus int) {
	t.updateHistory(p.Side, m, bonus)
	key := int(p.Board[m.From()])*64 + m.To()
	for _, back := range [2]int{1, 2} {
		if ply >= back && t.played[ply-back] >= 0 {
			c := &t.cont[t.played[ply-back]][key]
			*c = int16(gravity(int(*c), bonus, 16384))
		}
	}
}

func (t *thread) updateCapture(p *chess.Position, m chess.Move, bonus int) {
	victim := chess.Pawn
	if m&chess.FlagEP == 0 {
		victim = int(p.Board[m.To()]) % 6
	}
	h := &t.capHist[p.Board[m.From()]][m.To()][victim]
	*h = int16(gravity(int(*h), bonus, 16384))
}

// score calcula a nota de ordenação de cada lance (buffer do ply); os lances
// são escolhidos um a um com pickNext, sem ordenar a lista toda: na maioria
// dos nós o primeiro ou o segundo lance já corta.
func (t *thread) score(p *chess.Position, moves []chess.Move, ply int, first chess.Move) []int {
	scores := t.sbuf[ply][:len(moves)]
	for i, m := range moves {
		scores[i] = t.scoreMove(p, m, ply, first)
	}
	return scores
}

// pickNext traz para a posição i o lance de maior nota entre os restantes,
// preservando a ordem relativa dos outros (igual a uma ordenação estável).
func pickNext(moves []chess.Move, scores []int, i int) chess.Move {
	best := i
	for j := i + 1; j < len(moves); j++ {
		if scores[j] > scores[best] {
			best = j
		}
	}
	if best != i {
		m, sc := moves[best], scores[best]
		copy(moves[i+1:best+1], moves[i:best])
		copy(scores[i+1:best+1], scores[i:best])
		moves[i], scores[i] = m, sc
	}
	return moves[i]
}

// dropQuiets remove os lances quietos de moves[from:], mantendo a ordem.
func dropQuiets(moves []chess.Move, scores []int, from int) ([]chess.Move, []int) {
	n := from
	for j := from; j < len(moves); j++ {
		if m := moves[j]; m&chess.FlagCapture != 0 || m.Promo() != 0 {
			moves[n], scores[n] = m, scores[j]
			n++
		}
	}
	return moves[:n], scores[:n]
}

// order ordena a lista inteira (usado na raiz, que reaproveita a ordem).
func (t *thread) order(p *chess.Position, moves []chess.Move, ply int, first chess.Move) {
	t.seeN[ply] = 0 // a raiz não passa por negamax: zera o cache aqui
	var buf [256]int
	scores := buf[:len(moves)]
	for i, m := range moves {
		scores[i] = t.scoreMove(p, m, ply, first)
	}
	for i := 1; i < len(moves); i++ {
		for j := i; j > 0 && scores[j] > scores[j-1]; j-- {
			scores[j], scores[j-1] = scores[j-1], scores[j]
			moves[j], moves[j-1] = moves[j-1], moves[j]
		}
	}
}

func (t *thread) checkStop() bool {
	t.nodes++
	if t.nodes&255 == 0 {
		s := t.s
		total := s.totalNodes.Add(256)
		if s.nodeLimit > 0 && total >= s.nodeLimit {
			s.stop.Store(true)
		}
		if t.id == 0 && s.timed.Load() && time.Now().UnixNano() > s.deadline.Load() {
			s.stop.Store(true)
		}
	}
	return t.s.stop.Load()
}

// updateHistory com "gravidade": mantém os valores entre -16384 e 16384.
func (t *thread) updateHistory(side int, m chess.Move, bonus int) {
	h := &t.hist[side][m.From()][m.To()]
	*h = int16(gravity(int(*h), bonus, 16384))
}

// quiesce: só capturas (ou todas as evasões, se em xeque) até a posição acalmar.
func (t *thread) quiesce(p *chess.Position, ply, alpha, beta int) int {
	if t.checkStop() {
		return 0
	}
	t.seeN[ply] = 0
	if ply >= chess.MaxPly-1 {
		return t.evaluate(p, ply)
	}
	tt := t.s.tt
	tte, ttHit := tt.probe(p.Hash)
	if ttHit {
		sc := scoreFromTT(tte.score, ply)
		switch {
		case tte.flag == ttExact,
			tte.flag == ttLower && sc >= beta,
			tte.flag == ttUpper && sc <= alpha:
			return sc
		}
	}
	inCheck := p.InCheck()
	var pinned chess.Bitboard // peças cravadas, calculadas no primeiro lance
	havePins := false
	best := -Inf
	stand, raw := 0, evalNone
	if !inCheck {
		if ttHit && tte.eval != evalNone {
			raw = tte.eval
		} else {
			raw = t.evaluate(p, ply)
		}
		stand = raw
		if ttHit && tte.score > -Mate+2*chess.MaxPly && tte.score < Mate-2*chess.MaxPly {
			sc := tte.score
			if tte.flag == ttExact || (tte.flag == ttLower && sc > stand) || (tte.flag == ttUpper && sc < stand) {
				stand = sc
			}
		}
		if stand >= beta {
			if !ttHit {
				tt.store(p.Hash, chess.NoMove, scoreToTT(stand, ply), 0, ttLower, raw)
			}
			return stand
		}
		if stand > alpha {
			alpha = stand
		}
		best = stand
	}

	origAlpha := alpha
	bestMove := chess.NoMove
	moves := p.GenerateInto(t.mbuf[ply][:0], !inCheck)
	scores := t.score(p, moves, ply, tte.move)
	legal := 0
	for i := range moves {
		m := pickNext(moves, scores, i)
		if !inCheck {
			// Delta pruning: nem ganhando a peça chega perto de alfa.
			if m.Promo() == 0 && m&chess.FlagEP == 0 && stand+chess.SEEValue[int(p.Board[m.To()])%6]+200 <= alpha {
				continue
			}
			if t.see(p, m, ply) < 0 {
				continue
			}
		}
		child := &t.stack[ply+1]
		var ok bool
		if !havePins {
			pinned, havePins = p.Pinned(), true
		}
		*child, ok = p.MakeFast(m, pinned, inCheck)
		if !ok {
			continue
		}
		t.s.tt.prefetch(child.Hash)
		legal++
		t.nn.Push(ply, p, m)
		score := -t.quiesce(child, ply+1, -beta, -alpha)
		if t.s.stop.Load() {
			return 0
		}
		if score > best {
			best = score
			if score > alpha {
				alpha, bestMove = score, m
				if score >= beta {
					tt.store(p.Hash, m, scoreToTT(score, ply), 0, ttLower, raw)
					return score
				}
			}
		}
	}
	if inCheck && legal == 0 {
		return -Mate + ply
	}
	flag := ttUpper
	if best > origAlpha {
		flag = ttExact
	}
	tt.store(p.Hash, bestMove, scoreToTT(best, ply), 0, flag, raw)
	return best
}

func (t *thread) negamax(p *chess.Position, depth, ply, alpha, beta int, allowNull bool) int {
	if t.checkStop() {
		return 0
	}
	t.seeN[ply] = 0
	pvNode := beta-alpha > 1

	if p.Halfmove >= 100 || t.isRepetition(p.Halfmove) || p.InsufficientMaterial() {
		return 0
	}
	if ply >= chess.MaxPly-1 {
		return t.evaluate(p, ply)
	}
	// Mate distance pruning: não adianta buscar mate mais longo que um já achado.
	alpha = max(alpha, -Mate+ply)
	beta = min(beta, Mate-ply-1)
	if alpha >= beta {
		return alpha
	}

	inCheck := p.InCheck()
	var pinned chess.Bitboard // peças cravadas, calculadas no primeiro lance
	havePins := false
	if inCheck {
		depth++
	}
	if depth <= 0 {
		return t.quiesce(p, ply, alpha, beta)
	}

	s := t.s
	excluded := t.excluded[ply]
	ttMove := chess.NoMove
	tte, ttHit := s.tt.probe(p.Hash)
	if e, ok := tte, ttHit; ok {
		ttMove = e.move
		if !pvNode && excluded == chess.NoMove && e.depth >= depth {
			score := scoreFromTT(e.score, ply)
			switch {
			case e.flag == ttExact,
				e.flag == ttLower && score >= beta,
				e.flag == ttUpper && score <= alpha:
				return score
			}
		}
	}
	// Tablebases: em finais cobertos, o resultado é exato (exceto na busca de
	// singularidade, que exclui um lance e não corresponde à posição inteira).
	if ply > 0 && excluded == chess.NoMove {
		if v, flag, ok := t.tbProbe(p, ply); ok {
			if flag == ttExact || (flag == ttLower && v >= beta) || (flag == ttUpper && v <= alpha) {
				s.tt.store(p.Hash, chess.NoMove, scoreToTT(v, ply), min(depth+6, 250), flag, evalNone)
				return v
			}
		}
	}
	// IIR: sem lance da tabela, reduz um pouco (ordenação será pior de qualquer jeito).
	if ttMove == chess.NoMove && depth >= 4 && excluded == chess.NoMove {
		depth--
	}

	// Eval estática: reaproveita a da tabela; a nota da tabela, quando o
	// limite aponta na direção certa, é uma estimativa ainda melhor.
	ev, raw := -Inf, evalNone
	if !inCheck {
		if ttHit && tte.eval != evalNone {
			raw = tte.eval
		} else {
			raw = t.evaluate(p, ply)
		}
		ev = raw
		if ttHit && tte.score > -Mate+2*chess.MaxPly && tte.score < Mate-2*chess.MaxPly {
			sc := tte.score
			if tte.flag == ttExact || (tte.flag == ttLower && sc > ev) || (tte.flag == ttUpper && sc < ev) {
				ev = sc
			}
		}
	}
	t.evals[ply] = raw
	// improving: a eval estática subiu em relação ao nosso lance anterior.
	improving := false
	if !inCheck && ply >= 2 && t.evals[ply-2] != evalNone {
		improving = raw > t.evals[ply-2]
	}
	imp := 0
	if improving {
		imp = 1
	}

	if !pvNode && !inCheck && excluded == chess.NoMove {
		// Reverse futility.
		if depth <= 6 && ev-80*(depth-imp) >= beta && beta > -Mate+2*chess.MaxPly && beta < Mate-2*chess.MaxPly {
			return ev
		}
		// Null move.
		if allowNull && depth >= 3 && ev >= beta && p.HasNonPawn(p.Side) {
			r := 3 + depth/5 + min((ev-beta)/200, 3)
			null := &t.stack[ply+1]
			*null = p.MakeNull()
			s.tt.prefetch(null.Hash)
			t.played[ply] = -1
			t.push(null.Hash)
			t.nn.PushNull(ply)
			score := -t.negamax(null, depth-1-r, ply+1, -beta, -beta+1, false)
			t.pop()
			if s.stop.Load() {
				return 0
			}
			if score >= beta {
				if score > Mate-2*chess.MaxPly {
					score = beta
				}
				return score
			}
		}
		// ProbCut: uma captura boa que passa de beta com folga em busca rasa.
		if depth >= 5 && beta < Mate-2*chess.MaxPly {
			rbeta := beta + 180
			caps := p.GenerateInto(t.mbuf[ply][:0], true)
			scores := t.score(p, caps, ply, ttMove)
			for i := range caps {
				m := pickNext(caps, scores, i)
				if t.see(p, m, ply) < rbeta-ev {
					continue
				}
				child := &t.stack[ply+1]
				var ok bool
				if !havePins {
					pinned, havePins = p.Pinned(), true
				}
				*child, ok = p.MakeFast(m, pinned, inCheck)
				if !ok {
					continue
				}
				t.s.tt.prefetch(child.Hash)
				t.played[ply] = int(p.Board[m.From()])*64 + m.To()
				t.push(child.Hash)
				t.nn.Push(ply, p, m)
				score := -t.quiesce(child, ply+1, -rbeta, -rbeta+1)
				if score >= rbeta {
					score = -t.negamax(child, depth-4, ply+1, -rbeta, -rbeta+1, true)
				}
				t.pop()
				if s.stop.Load() {
					return 0
				}
				if score >= rbeta {
					s.tt.store(p.Hash, m, scoreToTT(score, ply), depth-3, ttLower, raw)
					return score
				}
			}
		}
	}

	pk := picker{ttMove: ttMove}

	origAlpha := alpha
	best, bestMove := -Inf, chess.NoMove
	legal, searched := 0, 0
	var quiets [64]chess.Move
	nQuiets := 0
	var captures [32]chess.Move
	nCaptures := 0

	for {
		m := t.nextMove(&pk, p, ply)
		if m == chess.NoMove {
			break
		}
		if m == excluded {
			continue
		}
		quiet := m&chess.FlagCapture == 0 && m.Promo() == 0
		isKiller := m == t.killers[ply][0] || m == t.killers[ply][1]

		// Singular extension: se o lance da TT é muito melhor que todos os
		// outros (uma busca rasa sem ele não chega perto da nota dele), ele é
		// "singular" e merece ser buscado um ply mais fundo.
		ext := 0
		if m == ttMove && excluded == chess.NoMove && ply > 0 && depth >= 8 && ttHit &&
			tte.flag != ttUpper && tte.depth >= depth-3 &&
			tte.score > -Mate+2*chess.MaxPly && tte.score < Mate-2*chess.MaxPly {
			ttScore := scoreFromTT(tte.score, ply)
			sBeta := ttScore - 2*depth
			t.excluded[ply] = m
			sScore := t.negamax(p, (depth-1)/2, ply, sBeta-1, sBeta, false)
			t.excluded[ply] = chess.NoMove
			if s.stop.Load() {
				return 0
			}
			switch {
			case sScore < sBeta:
				ext = 1
			case sBeta >= beta:
				// Multi-cut: mesmo sem o lance da TT outro lance passa de beta.
				return sBeta
			case ttScore >= beta:
				ext = -1 // há alternativas boas: busca o lance da TT mais raso
			}
		}
		newDepth := depth - 1 + ext

		// Podas em lances tardios (só depois de ter um lance que não perde de mate).
		if ply > 0 && !inCheck && best > -Mate+2*chess.MaxPly {
			if quiet && !isKiller {
				// Late move pruning. A condição só fica mais verdadeira daqui
				// em diante, e os killers vêm antes de qualquer quieto: todos os
				// quietos restantes seriam podados, então saem da lista já.
				if depth <= 6 && searched >= (3+depth*depth)/(2-imp) {
					pk.skipQuiets()
					continue
				}
			} else if !quiet && depth <= 5 && t.see(p, m, ply) < -90*depth {
				continue
			}
		}

		child := &t.stack[ply+1]
		var ok bool
		if !havePins {
			pinned, havePins = p.Pinned(), true
		}
		*child, ok = p.MakeFast(m, pinned, inCheck)
		if !ok {
			continue
		}
		t.s.tt.prefetch(child.Hash)
		legal++
		givesCheck := child.InCheck()

		// Futility: lance quieto perto das folhas que não tem como subir até alfa.
		if ply > 0 && quiet && !inCheck && !givesCheck && !isKiller && depth <= 5 &&
			best > -Mate+2*chess.MaxPly && ev+100+90*depth <= alpha {
			continue
		}
		searched++

		t.played[ply] = int(p.Board[m.From()])*64 + m.To()
		t.push(child.Hash)
		t.nn.Push(ply, p, m)
		var score int
		if searched == 1 {
			score = -t.negamax(child, newDepth, ply+1, -beta, -alpha, true)
		} else {
			r := 0
			if depth >= 3 && searched > 2 && !inCheck && !givesCheck && (quiet || t.see(p, m, ply) < 0) {
				r = lmrTable[min(depth, 63)][min(searched, 63)]
				if pvNode {
					r--
				}
				if isKiller {
					r--
				}
				if !improving {
					r++
				}
				if quiet {
					r -= t.quietScore(p, m, ply) / 8000
				}
				r = max(0, min(r, newDepth-1))
			}
			score = -t.negamax(child, newDepth-r, ply+1, -alpha-1, -alpha, true)
			if score > alpha && r > 0 {
				score = -t.negamax(child, newDepth, ply+1, -alpha-1, -alpha, true)
			}
			if score > alpha && score < beta {
				score = -t.negamax(child, newDepth, ply+1, -beta, -alpha, true)
			}
		}
		t.pop()
		if s.stop.Load() {
			return 0
		}

		if score > best {
			best, bestMove = score, m
			if score > alpha {
				alpha = score
				if score >= beta {
					bonus := min(depth*depth*16, 1600)
					if quiet {
						if t.killers[ply][0] != m {
							t.killers[ply][1] = t.killers[ply][0]
							t.killers[ply][0] = m
						}
						t.updateQuiet(p, m, ply, bonus)
						for _, q := range quiets[:nQuiets] {
							t.updateQuiet(p, q, ply, -bonus)
						}
					} else if m&chess.FlagCapture != 0 {
						t.updateCapture(p, m, bonus)
					}
					for _, c := range captures[:nCaptures] {
						t.updateCapture(p, c, -bonus)
					}
					break
				}
			}
		}
		if quiet && nQuiets < len(quiets) {
			quiets[nQuiets] = m
			nQuiets++
		} else if !quiet && nCaptures < len(captures) && m&chess.FlagCapture != 0 {
			captures[nCaptures] = m
			nCaptures++
		}
	}

	if legal == 0 {
		if excluded != chess.NoMove {
			return alpha // o único lance legal era o excluído: não é mate
		}
		if inCheck {
			return -Mate + ply
		}
		return 0
	}
	if best == -Inf { // todos os lances podados
		return alpha
	}
	if excluded != chess.NoMove {
		return best // busca de singularidade: não grava na TT (mesma posição)
	}

	flag := ttExact
	if best <= origAlpha {
		flag = ttUpper
	} else if best >= beta {
		flag = ttLower
	}
	s.tt.store(p.Hash, bestMove, scoreToTT(best, ply), depth, flag, raw)
	return best
}

func (t *thread) searchRoot(p *chess.Position, moves []chess.Move, depth, alpha, beta int) (int, chess.Move) {
	best, bestMove := -Inf, chess.NoMove
	for i, m := range moves {
		child := &t.stack[1]
		*child, _ = p.Make(m)
		t.played[0] = int(p.Board[m.From()])*64 + m.To()
		t.push(child.Hash)
		t.nn.Push(0, p, m)
		var score int
		if i == 0 {
			score = -t.negamax(child, depth-1, 1, -beta, -alpha, true)
		} else {
			score = -t.negamax(child, depth-1, 1, -alpha-1, -alpha, true)
			if score > alpha && score < beta {
				score = -t.negamax(child, depth-1, 1, -beta, -alpha, true)
			}
		}
		t.pop()
		if t.s.stop.Load() {
			break
		}
		if score > best {
			best = score
			if score > alpha {
				alpha, bestMove = score, m
				if score >= beta {
					break
				}
			}
		}
	}
	return best, bestMove
}

// hardBudget: com relógio, o limite duro é maior que o ideal; com movetime, é o próprio.
func (s *Searcher) hardBudget() time.Duration {
	if s.clockMode && s.maxTime > 0 {
		return s.maxTime
	}
	return s.budget
}

// timeScale: quanto do tempo ideal usar, conforme a estabilidade do melhor
// lance (iterações seguidas sem mudar) e a queda da nota desde a anterior.
func timeScale(stability, drop int) float64 {
	scale := [...]float64{1.6, 1.25, 1.0, 0.85, 0.75, 0.65}[min(stability, 5)]
	if drop > 0 {
		scale *= 1 + float64(min(drop, 150))/200
	}
	return scale
}

func (s *Searcher) setupTime(p *chess.Position, lim Limits) {
	now := time.Now()
	s.timed.Store(false)
	s.budget = 0
	s.clockMode = false
	s.maxTime = 0
	switch {
	case lim.MoveTime > 0:
		s.budget = lim.MoveTime
	case lim.WTime > 0 || lim.BTime > 0:
		left, inc := lim.WTime, lim.WInc
		if p.Side == chess.Black {
			left, inc = lim.BTime, lim.BInc
		}
		mtg := 30
		if lim.MovesToGo > 0 {
			mtg = min(lim.MovesToGo+1, 30)
		}
		budget := left/time.Duration(mtg) + inc*3/4
		if budget > left/2 {
			budget = left / 2
		}
		if budget > left-50*time.Millisecond {
			budget = max(left-50*time.Millisecond, 5*time.Millisecond)
		}
		s.budget = budget
		s.clockMode = true
		s.softBase = budget / 2
		// Limite duro: pode passar do ideal quando a posição pede, mas nunca
		// mais que 1/4 do que resta (nem chegar perto de zerar o relógio).
		s.maxTime = min(budget*3, max(left/4, budget), max(left-50*time.Millisecond, 5*time.Millisecond))
	}
	if s.budget > 0 && !lim.Infinite && !lim.Ponder {
		s.searchStart.Store(now.UnixNano())
		s.deadline.Store(now.Add(s.hardBudget()).UnixNano())
		if lim.MoveTime > 0 {
			s.soft.Store(now.Add(s.budget).UnixNano())
		} else {
			s.soft.Store(now.Add(s.budget / 2).UnixNano())
		}
		s.timed.Store(true)
	}
}

func (t *thread) extractPV(p *chess.Position, first chess.Move, maxLen int) []chess.Move {
	pv := []chess.Move{first}
	pos, _ := p.Make(first)
	seen := map[uint64]bool{p.Hash: true}
	for len(pv) < maxLen && !seen[pos.Hash] {
		seen[pos.Hash] = true
		e, ok := t.s.tt.probe(pos.Hash)
		if !ok || e.move == chess.NoMove {
			break
		}
		valid := false
		for _, m := range pos.GenerateMoves(false) {
			if m == e.move {
				valid = true
				break
			}
		}
		if !valid {
			break
		}
		next, ok := pos.Make(e.move)
		if !ok {
			break
		}
		pv = append(pv, e.move)
		pos = next
	}
	return pv
}

func ScoreString(score int) string {
	if score > Mate-2*chess.MaxPly {
		return fmt.Sprintf("mate %d", (Mate-score+1)/2)
	}
	if score < -Mate+2*chess.MaxPly {
		return fmt.Sprintf("mate %d", -(Mate+score)/2)
	}
	return fmt.Sprintf("cp %d", score)
}

func (s *Searcher) newThread(id int) *thread {
	return &thread{s: s, id: id, nn: eval.NewNNUEStack()}
}

// Search: aprofundamento iterativo com aspiration windows, MultiPV e Lazy SMP
// (threads auxiliares buscam a mesma posição e se ajudam pela tabela).
// path: hashes do jogo até p (inclusive). searchMoves: restringe a raiz (opcional).
func (s *Searcher) Search(p *chess.Position, path []uint64, lim Limits, info func(string)) Result {
	if s.tt == nil {
		s.tt = NewTT(16)
	}
	threads := max(1, s.Threads)
	for len(s.workers) < threads {
		s.workers = append(s.workers, s.newThread(len(s.workers)))
	}
	s.totalNodes.Store(0)
	s.tt.NewSearch()
	s.tbHits.Store(0)
	s.nodeLimit = lim.Nodes
	s.setupTime(p, lim)
	start := time.Now()

	basePath := append([]uint64(nil), path...)
	if len(basePath) == 0 || basePath[len(basePath)-1] != p.Hash {
		basePath = append(basePath, p.Hash)
	}

	rootMoves := p.LegalMoves()
	res := Result{}
	if len(rootMoves) == 0 {
		return res
	}
	if f := s.tbRootFilter(p, rootMoves); len(f) > 0 {
		rootMoves = f // só lances que mantêm o resultado da tablebase
	}
	res.Best = rootMoves[0]
	if e, ok := s.tt.probe(p.Hash); ok {
		for _, m := range rootMoves {
			if m == e.move {
				res.Best = m
			}
		}
	}
	if len(rootMoves) == 1 && s.timed.Load() && !lim.Infinite {
		res.PV = []chess.Move{res.Best}
		return res
	}

	maxDepth := lim.Depth
	if maxDepth <= 0 || maxDepth >= chess.MaxPly-2 {
		maxDepth = chess.MaxPly - 3
	}

	// Threads auxiliares.
	var wg sync.WaitGroup
	first := res.Best // cópia: a thread principal reescreve res.Best durante a busca
	for i := 1; i < threads; i++ {
		w := s.workers[i]
		w.nodes = 0
		w.path = append(w.path[:0], basePath...)
		w.killers = [chess.MaxPly + 2][2]chess.Move{}
		w.setupNN(p)
		wg.Add(1)
		go func(w *thread) {
			defer wg.Done()
			moves := append([]chess.Move(nil), rootMoves...)
			best := first
			for d := 1 + w.id%2; d <= maxDepth && !s.stop.Load(); d++ {
				w.order(p, moves, 0, best)
				_, m := w.searchRoot(p, moves, d, -Inf, Inf)
				if m != chess.NoMove {
					best = m
				}
			}
		}(w)
	}

	main := s.workers[0]
	main.nodes = 0
	main.path = append(main.path[:0], basePath...)
	main.killers = [chess.MaxPly + 2][2]chess.Move{}
	main.setupNN(p)

	multiPV := max(1, min(s.MultiPV, len(rootMoves)))
	prevScore := 0
	stability := 0    // iterações seguidas com o mesmo melhor lance
	partialScore := 0 // nota do melhor lance achado numa iteração interrompida
	prevLines := []Line{}

	for depth := 1; depth <= maxDepth; depth++ {
		var lines []Line
		chosen := map[chess.Move]bool{}
		stopped := false
		for pvIdx := 0; pvIdx < multiPV; pvIdx++ {
			moves := make([]chess.Move, 0, len(rootMoves))
			for _, m := range rootMoves {
				if !chosen[m] {
					moves = append(moves, m)
				}
			}
			first := res.Best
			if pvIdx < len(prevLines) && !chosen[prevLines[pvIdx].Move] {
				first = prevLines[pvIdx].Move
			} else if pvIdx > 0 {
				first = chess.NoMove
			}
			main.order(p, moves, 0, first)

			window := 30
			alpha, beta := -Inf, Inf
			if depth >= 4 && pvIdx == 0 {
				alpha, beta = prevScore-window, prevScore+window
			}
			var score int
			var move chess.Move
			for {
				score, move = main.searchRoot(p, moves, depth, alpha, beta)
				if move != chess.NoMove {
					main.order(p, moves, 0, move)
					if pvIdx == 0 {
						res.Best, partialScore = move, score
					}
				}
				if s.stop.Load() {
					break
				}
				if score <= alpha {
					alpha = max(-Inf, alpha-window)
				} else if score >= beta {
					beta = min(Inf, beta+window)
				} else {
					break
				}
				window *= 2
				if window > 800 {
					alpha, beta = -Inf, Inf
				}
			}
			if s.stop.Load() {
				stopped = true
				break
			}
			chosen[move] = true
			lines = append(lines, Line{Move: move, Score: score, PV: main.extractPV(p, move, depth)})
		}
		if stopped {
			break
		}

		if len(prevLines) > 0 && lines[0].Move == prevLines[0].Move {
			stability++
		} else {
			stability = 0
		}
		drop := 0
		if depth >= 4 {
			drop = prevScore - lines[0].Score
		}
		prevLines = lines
		prevScore = lines[0].Score
		res.Best, res.Score, res.Depth = lines[0].Move, lines[0].Score, depth
		res.PV, res.Lines = lines[0].PV, lines
		res.Nodes, res.Time = s.totalNodes.Load()+main.nodes%256, time.Since(start)

		if info != nil {
			ms := res.Time.Milliseconds()
			nps := uint64(0)
			if ms > 0 {
				nps = res.Nodes * 1000 / uint64(ms)
			}
			for i, l := range lines {
				pvs := ""
				for _, m := range l.PV {
					pvs += " " + m.String()
				}
				info(fmt.Sprintf("info depth %d multipv %d score %s nodes %d nps %d hashfull %d tbhits %d time %d pv%s",
					depth, i+1, ScoreString(l.Score), res.Nodes, nps, s.tt.Hashfull(), s.tbHits.Load(), ms, pvs))
			}
		}

		if lim.Mate > 0 && res.Score > Mate-2*chess.MaxPly && (Mate-res.Score+1)/2 <= lim.Mate {
			break
		}
		if s.timed.Load() {
			now := time.Now().UnixNano()
			if s.clockMode {
				limit := s.searchStart.Load() + int64(float64(s.softBase)*timeScale(stability, drop))
				if now > limit {
					break
				}
			} else if now > s.soft.Load() {
				break
			}
		}
		if !lim.Infinite && !lim.Ponder && lim.Mate == 0 && res.Score > Mate-2*chess.MaxPly && depth > 2*(Mate-res.Score)+4 {
			break
		}
	}
	s.stop.Store(true)
	wg.Wait()
	res.Nodes, res.Time = s.totalNodes.Load()+main.nodes%256, time.Since(start)
	// A iteração interrompida achou um lance melhor que o da última completa:
	// joga esse lance, e a última linha "info" tem que mostrá-lo (a GUI e o
	// fastchess esperam que o bestmove seja o começo da última PV).
	if len(res.PV) == 0 || res.PV[0] != res.Best {
		if res.Depth > 0 {
			res.Score = partialScore
		}
		res.PV = main.extractPV(p, res.Best, max(1, res.Depth))
		if len(res.Lines) > 0 {
			res.Lines[0] = Line{Move: res.Best, Score: res.Score, PV: res.PV}
		}
		if info != nil && res.Depth > 0 {
			ms := res.Time.Milliseconds()
			nps := uint64(0)
			if ms > 0 {
				nps = res.Nodes * 1000 / uint64(ms)
			}
			pvs := ""
			for _, m := range res.PV {
				pvs += " " + m.String()
			}
			info(fmt.Sprintf("info depth %d multipv 1 score %s nodes %d nps %d hashfull %d tbhits %d time %d pv%s",
				res.Depth, ScoreString(res.Score), res.Nodes, nps, s.tt.Hashfull(), s.tbHits.Load(), ms, pvs))
		}
	}
	return res
}
