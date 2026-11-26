package api

import (
	"strings"
	"testing"

	"gochess/internal/book"
	"gochess/internal/chess"
	"gochess/internal/search"
)

// Valores de perft conhecidos (chessprogramming.org/Perft_Results).
func TestPerft(t *testing.T) {
	cases := []struct {
		fen   string
		depth int
		nodes uint64
	}{
		{chess.StartFEN, 1, 20},
		{chess.StartFEN, 2, 400},
		{chess.StartFEN, 3, 8902},
		{chess.StartFEN, 4, 197281},
		{chess.StartFEN, 5, 4865609},
		// "Kiwipete": roques, en passant, promoções, cravadas
		{"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1", 1, 48},
		{"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1", 2, 2039},
		{"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1", 3, 97862},
		{"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1", 4, 4085603},
		// en passant que deixaria o rei em xeque
		{"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1", 5, 674624},
		// promoções e roque
		{"r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1", 4, 422333},
		{"rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8", 4, 2103487},
	}
	for _, c := range cases {
		p, err := chess.ParseFEN(c.fen)
		if err != nil {
			t.Fatal(err)
		}
		if got := chess.Perft(p, c.depth); got != c.nodes {
			t.Errorf("perft(%d) %s = %d, esperado %d", c.depth, c.fen, got, c.nodes)
		}
	}
}

func bestMove(t *testing.T, fen string, depth int) string {
	t.Helper()
	p, err := chess.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}
	s := search.NewSearcher(16)
	return s.Search(p, nil, search.Limits{Depth: depth}, nil).Best.String()
}

func TestMateInOne(t *testing.T) {
	if m := bestMove(t, "6k1/5ppp/8/8/8/8/5PPP/3R2K1 w - - 0 1", 3); m != "d1d8" {
		t.Errorf("mate do corredor: esperado d1d8, veio %s", m)
	}
}

func TestWinsHangingQueen(t *testing.T) {
	// Dama preta solta em d5 atacada pelo cavalo de c3.
	if m := bestMove(t, "rnb1kbnr/pppp1ppp/8/3q4/8/2N5/PPPP1PPP/R1BQKBNR w KQkq - 0 1", 4); m != "c3d5" {
		t.Errorf("esperado c3d5, veio %s", m)
	}
}

// O hash incremental tem de bater com o recalculado do zero em toda a árvore.
func TestIncrementalHash(t *testing.T) {
	var walk func(p *chess.Position, depth int)
	walk = func(p *chess.Position, depth int) {
		if p.Hash != p.ComputeHash() {
			t.Fatalf("hash divergiu em %s", p.FEN())
		}
		if depth == 0 {
			return
		}
		for _, m := range p.GenerateMoves(false) {
			if c, ok := p.Make(m); ok {
				walk(&c, depth-1)
			}
		}
	}
	for _, fen := range []string{
		chess.StartFEN,
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
	} {
		p, _ := chess.ParseFEN(fen)
		walk(p, 3)
	}
}

func TestFENRoundTrip(t *testing.T) {
	for _, fen := range []string{
		chess.StartFEN,
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"rnbqkbnr/pp1ppppp/8/2p5/4P3/8/PPPP1PPP/RNBQKBNR w KQkq c6 0 2",
	} {
		p, _ := chess.ParseFEN(fen)
		if got := p.FEN(); got != fen {
			t.Errorf("FEN: esperado %q, veio %q", fen, got)
		}
	}
}

func TestSAN(t *testing.T) {
	cases := []struct{ fen, uci, san string }{
		{chess.StartFEN, "g1f3", "Nf3"},
		{"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1", "e1g1", "O-O"},
		{"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1", "e1c1", "O-O-O"},
		{"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1", "d5e6", "dxe6"},
		{"6k1/5ppp/8/8/8/8/5PPP/3R2K1 w - - 0 1", "d1d8", "Rd8#"},
		{"4k3/8/8/8/8/8/4K3/R6R w - - 0 1", "a1d1", "Rad1"},
		{"7k/P7/8/8/8/8/8/4K3 w - - 0 1", "a7a8q", "a8=Q+"},
		{"4k3/8/8/2N3N1/8/2N5/8/4K3 w - - 0 1", "c3e4", "N3e4"},
	}
	for _, c := range cases {
		p, _ := chess.ParseFEN(c.fen)
		m, ok := p.ParseUCIMove(c.uci)
		if !ok {
			t.Fatalf("lance %s ilegal em %s", c.uci, c.fen)
		}
		if got := p.SAN(m); got != c.san {
			t.Errorf("%s: esperado %s, veio %s", c.uci, c.san, got)
		}
	}
}

func TestMateInTwo(t *testing.T) {
	// 1.Rd8+ Rxd8 2.Rxd8# (mate no corredor com torres dobradas)
	p, _ := chess.ParseFEN("2r3k1/5ppp/8/8/8/8/3R1PPP/3R2K1 w - - 0 1")
	s := search.NewSearcher(16)
	res := s.Search(p, nil, search.Limits{Depth: 6}, nil)
	if res.Best.String() != "d2d8" || search.ScoreString(res.Score) != "mate 2" {
		t.Errorf("esperado d2d8 com mate 2; veio %s %s", res.Best, search.ScoreString(res.Score))
	}
}

// Buscas seguidas pela API não podem herdar a flag de parada da anterior.
func TestAPIConsecutiveSearches(t *testing.T) {
	for i, moves := range [][]string{{"e2e4"}, {"e2e4", "e7e5", "g1f3"}} {
		r := HandleAPI(APIRequest{Action: "think", Moves: moves, Level: 4})
		if r.Error != "" || r.Depth < 4 {
			t.Fatalf("busca %d: profundidade %d, erro %q", i, r.Depth, r.Error)
		}
	}
}

func TestBookLinesAreLegal(t *testing.T) {
	for _, line := range book.BookLines {
		p, _ := chess.ParseFEN(chess.StartFEN)
		for _, san := range strings.Fields(line) {
			m, ok := p.ParseSAN(san)
			if !ok {
				t.Fatalf("lance %q inválido na linha %q", san, line)
			}
			n, _ := p.Make(m)
			p = &n
		}
	}
}

func TestPGNImport(t *testing.T) {
	pgn := "[Event \"x\"]\n[White \"a\"]\n\n1. e4 {comentário} e5 2. Nf3 (2. f4 exf4) Nc6 3. Bb5 a6 4. Ba4 Nf6 5. O-O Be7 *"
	r := HandleAPI(APIRequest{Action: "pgn", PGN: pgn})
	if r.Error != "" || len(r.Moves) != 10 || r.Moves[8] != "e1g1" {
		t.Fatalf("importação de PGN: %+v", r.Moves)
	}
}

func TestAPIAnalyze(t *testing.T) {
	r := HandleAPI(APIRequest{Action: "analyze", Moves: []string{"e2e4"}, MultiPV: 3, TimeMs: 300})
	if len(r.Lines) != 3 || len(r.Lines[0].PV) == 0 {
		t.Fatalf("análise MultiPV: %+v", r.Lines)
	}
}
