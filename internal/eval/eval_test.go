package eval

import (
	"math/rand"
	"strings"
	"testing"

	"gochess/internal/chess"
)

// mirrorFEN troca as cores e espelha o tabuleiro verticalmente.
func mirrorFEN(fen string) string {
	f := strings.Fields(fen)
	ranks := strings.Split(f[0], "/")
	for i, j := 0, len(ranks)-1; i < j; i, j = i+1, j-1 {
		ranks[i], ranks[j] = ranks[j], ranks[i]
	}
	swap := func(s string) string {
		var b strings.Builder
		for _, c := range s {
			switch {
			case c >= 'a' && c <= 'z':
				b.WriteRune(c - 32)
			case c >= 'A' && c <= 'Z':
				b.WriteRune(c + 32)
			default:
				b.WriteRune(c)
			}
		}
		return b.String()
	}
	side := "w"
	if f[1] == "w" {
		side = "b"
	}
	castle := f[2]
	if castle != "-" {
		castle = swap(castle)
		// ordem canônica KQkq
		order := ""
		for _, c := range "KQkq" {
			if strings.ContainsRune(castle, c) {
				order += string(c)
			}
		}
		castle = order
	}
	ep := f[3]
	if ep != "-" {
		ep = string(ep[0]) + string(rune('1'+('8'-ep[1])))
	}
	return swap(strings.Join(ranks, "/")) + " " + side + " " + castle + " " + ep + " 0 1"
}

var evalFENs = []string{
	chess.StartFEN,
	"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
	"r1bq1rk1/pp2bppp/2n1pn2/3p4/2PP4/2N1PN2/PP3PPP/R2QKB1R w KQ - 0 8",
	"8/5pk1/6p1/3P4/1p6/1P4P1/5PK1/8 b - - 0 40",
	"2r3k1/5ppp/8/8/8/8/3R1PPP/3R2K1 w - - 0 1",
}

func TestEvalSymmetry(t *testing.T) {
	// Valores arbitrários nos parâmetros zerados, para exercitar todos os termos.
	saved := append([]Pair(nil), Params...)
	defer copy(Params, saved)
	r := rand.New(rand.NewSource(1))
	for i := range Params {
		if Params[i] == (Pair{}) {
			Params[i] = Pair{MG: r.Intn(41) - 20, EG: r.Intn(41) - 20}
		}
	}
	for _, fen := range evalFENs {
		p, _ := chess.ParseFEN(fen)
		m, err := chess.ParseFEN(mirrorFEN(fen))
		if err != nil {
			t.Fatal(err)
		}
		if a, b := Evaluate(p), Evaluate(m); a != b {
			t.Errorf("assimetria em %s: %d vs %d", fen, a, b)
		}
	}
}

func TestKPK(t *testing.T) {
	cases := []struct {
		fen string
		win bool
	}{
		{"4k3/8/4K3/4P3/8/8/8/8 w - - 0 1", true}, // rei na 6ª à frente do peão
		{"4k3/8/4K3/4P3/8/8/8/8 b - - 0 1", true},
		{"k7/8/K7/P7/8/8/8/8 b - - 0 1", false}, // peão da torre: empate
		{"k7/8/K7/P7/8/8/8/8 w - - 0 1", false},
		{"8/8/8/8/8/4k3/4P3/4K3 w - - 0 1", false}, // rei preto na frente, sem oposição útil
		{"8/8/k7/8/8/8/7P/4K3 w - - 0 1", true},    // peão corre: rei preto fora do quadrado
	}
	for _, c := range cases[:6] {
		p, _ := chess.ParseFEN(c.fen)
		_, win := KPKProbe(p)
		if win != c.win {
			t.Errorf("KPK %s: vitória=%v, esperado %v", c.fen, win, c.win)
		}
	}
	// Mesmo resultado com cores trocadas.
	for _, c := range cases[:6] {
		p, _ := chess.ParseFEN(mirrorFEN(c.fen))
		if _, win := KPKProbe(p); win != c.win {
			t.Errorf("KPK espelhado %s: vitória=%v", c.fen, win)
		}
	}
}

func randomNet(seed int64) *Net {
	r := rand.New(rand.NewSource(seed))
	n := &Net{}
	for i := range n.FW {
		for j := range n.FW[i] {
			n.FW[i][j] = int16(r.Intn(61) - 30)
		}
	}
	for j := range n.FB {
		n.FB[j] = int16(r.Intn(200))
	}
	for j := range n.OW {
		n.OW[j] = int16(r.Intn(129) - 64)
	}
	n.OB = 100
	return n
}

// O acumulador incremental tem de bater com o recalculado em toda a árvore.
func TestNNUEIncremental(t *testing.T) {
	n := randomNet(7)
	st := NewNNUEStack()
	var walk func(p *chess.Position, ply, depth int)
	walk = func(p *chess.Position, ply, depth int) {
		if got, want := st.Evaluate(p, ply), EvaluateNNUE(n, p); got != want {
			t.Fatalf("NNUE incremental %d != %d em %s", got, want, p.FEN())
		}
		if depth == 0 {
			return
		}
		for _, m := range p.GenerateMoves(false) {
			c, ok := p.Make(m)
			if !ok {
				continue
			}
			st.Push(ply, p, m)
			walk(&c, ply+1, depth-1)
		}
	}
	for _, fen := range []string{chess.StartFEN, "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1"} {
		p, _ := chess.ParseFEN(fen)
		st.Enable(n, p)
		walk(p, 0, 3)
	}
	// Serialização ida e volta.
	m, err := LoadNet(n.Marshal())
	if err != nil || m.OB != n.OB || m.FW[100][5] != n.FW[100][5] {
		t.Fatalf("Marshal/LoadNet falhou: %v", err)
	}
}
