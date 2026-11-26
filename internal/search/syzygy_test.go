package search

import (
	"os"
	"testing"

	"gochess/internal/chess"
	"gochess/internal/eval"
)

// Os testes com tablebases só rodam se SYZYGY_PATH apontar para os arquivos
// (os de até 4 peças bastam), por exemplo:
//
//	SYZYGY_PATH=~/syzygy go test -run Syzygy ./...
func syzygyOrSkip(t *testing.T) {
	t.Helper()
	path := os.Getenv("SYZYGY_PATH")
	if path == "" || !SyzygySupported() {
		t.Skip("SYZYGY_PATH não definido ou build sem cgo")
	}
	if n := SetSyzygyPath(path); n < 3 {
		t.Fatalf("nenhuma tablebase carregada de %s", path)
	}
}

func TestSyzygyWDL(t *testing.T) {
	syzygyOrSkip(t)
	defer func() { tbLargest = 0 }()
	cases := []struct {
		fen   string
		score int // sinal esperado: 1 vitória, 0 empate, -1 derrota (de quem joga)
	}{
		{"4k3/8/8/8/8/8/8/3QK3 w - - 0 1", 1},
		{"4k3/8/8/8/8/8/8/3QK3 b - - 0 1", -1},
		{"4k3/8/8/8/8/8/8/3RK3 w - - 0 1", 1},
		{"4k3/8/8/r7/8/8/8/3RK3 w - - 0 1", 0}, // torre contra torre (conferido no python-chess)
		{"4k3/8/8/8/8/8/8/2NNK3 w - - 0 1", 0}, // dois cavalos não dão mate forçado
		{"7k/8/8/8/8/8/8/1BN1K3 w - - 0 1", 1}, // bispo e cavalo
	}
	s := NewSearcher(16)
	th := s.newThread(0)
	for _, c := range cases {
		p, _ := chess.ParseFEN(c.fen)
		v, _, ok := th.tbProbe(p, 0)
		if !ok {
			t.Fatalf("%s: tablebase não respondeu", c.fen)
		}
		sign := 0
		if v > 1 {
			sign = 1
		} else if v < -1 {
			sign = -1
		}
		if sign != c.score {
			t.Errorf("%s: nota %d, esperado sinal %d", c.fen, v, c.score)
		}
	}
}

// A tabela KPK própria do GoChess tem de concordar com a Syzygy em todas as posições.
func TestSyzygyAgreesWithKPK(t *testing.T) {
	syzygyOrSkip(t)
	defer func() { tbLargest = 0 }()
	s := NewSearcher(16)
	th := s.newThread(0)
	checked := 0
	for wk := 0; wk < 64; wk++ {
		for bk := 0; bk < 64; bk++ {
			for wp := 8; wp < 56; wp++ {
				for _, side := range []int{chess.White, chess.Black} {
					if wk == bk || wk == wp || bk == wp || eval.Dist(wk, bk) <= 1 {
						continue
					}
					p := &chess.Position{EP: -1, Side: side}
					for i := range p.Board {
						p.Board[i] = -1
					}
					p.Put(chess.White, chess.King, wk)
					p.Put(chess.Black, chess.King, bk)
					p.Put(chess.White, chess.Pawn, wp)
					if p.Attacked(chess.LSB(p.Pieces[side^1][chess.King]), side) { // quem não joga em xeque: ilegal
						continue
					}
					p.Hash = p.ComputeHash()
					v, _, ok := th.tbProbe(p, 0)
					if !ok {
						t.Fatalf("sem resposta em %s", p.FEN())
					}
					_, win := eval.KPKProbe(p)
					tbWin := (side == chess.White && v > 1) || (side == chess.Black && v < -1)
					if win != tbWin {
						t.Fatalf("KPK discorda da Syzygy em %s: KPK=%v Syzygy=%d", p.FEN(), win, v)
					}
					checked++
				}
			}
		}
	}
	t.Logf("%d posições KPK conferidas", checked)
}

func TestSyzygyRootFilter(t *testing.T) {
	syzygyOrSkip(t)
	defer func() { tbLargest = 0 }()
	// Dama em a1; De5 entrega a dama para o rei em e6 e vira empate.
	p, _ := chess.ParseFEN("8/8/4k3/8/8/8/8/Q3K3 w - - 0 1")
	s := NewSearcher(16)
	moves := s.tbRootFilter(p, p.LegalMoves())
	if len(moves) == 0 {
		t.Fatal("filtro da raiz não devolveu lances")
	}
	for _, m := range moves {
		if m.String() == "a1e5" {
			t.Fatal("a1e5 entrega a dama e não deveria passar pelo filtro")
		}
	}
	res := s.Search(p, nil, Limits{Depth: 12}, nil)
	if res.Score < 1000 {
		t.Errorf("com tablebase, a nota de KQvK deveria ser de vitória; veio %d (%s)", res.Score, res.Best)
	}
}
