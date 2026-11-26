package book

import (
	"math/rand"
	"strings"
	"sync"

	"gochess/internal/chess"
)

// Livro de aberturas embutido: linhas principais das aberturas mais comuns,
// em SAN. Dá variedade às partidas e poupa tempo no começo.

var BookLines = []string{
	// Ruy Lopez
	"e4 e5 Nf3 Nc6 Bb5 a6 Ba4 Nf6 O-O Be7 Re1 b5 Bb3 d6 c3 O-O h3",
	"e4 e5 Nf3 Nc6 Bb5 a6 Ba4 Nf6 O-O Be7 Re1 b5 Bb3 O-O c3 d5",
	"e4 e5 Nf3 Nc6 Bb5 Nf6 O-O Nxe4 d4 Nd6 Bxc6 dxc6 dxe5 Nf5",
	"e4 e5 Nf3 Nc6 Bb5 a6 Bxc6 dxc6 O-O f6 d4 exd4 Nxd4 c5",
	// Italiana / Escocesa / Petrov / Quatro Cavalos
	"e4 e5 Nf3 Nc6 Bc4 Bc5 c3 Nf6 d3 d6 O-O O-O",
	"e4 e5 Nf3 Nc6 Bc4 Nf6 d3 Be7 O-O O-O Re1 d6",
	"e4 e5 Nf3 Nc6 d4 exd4 Nxd4 Nf6 Nxc6 bxc6 e5 Qe7 Qe2 Nd5",
	"e4 e5 Nf3 Nf6 Nxe5 d6 Nf3 Nxe4 d4 d5 Bd3",
	"e4 e5 Nf3 Nc6 Nc3 Nf6 Bb5 Bb4 O-O O-O d3 d6",
	"e4 e5 Nf3 d6 d4 Nf6 Nc3 Nbd7 Bc4 Be7 O-O O-O",
	// Siciliana
	"e4 c5 Nf3 d6 d4 cxd4 Nxd4 Nf6 Nc3 a6 Be3 e5 Nb3 Be6",
	"e4 c5 Nf3 d6 d4 cxd4 Nxd4 Nf6 Nc3 g6 Be3 Bg7 f3 O-O",
	"e4 c5 Nf3 Nc6 d4 cxd4 Nxd4 Nf6 Nc3 e5 Ndb5 d6",
	"e4 c5 Nf3 e6 d4 cxd4 Nxd4 Nc6 Nc3 Qc7 Be3 a6",
	"e4 c5 Nf3 d6 Bb5+ Bd7 Bxd7+ Qxd7 O-O Nc6 c3 Nf6",
	"e4 c5 Nc3 Nc6 g3 g6 Bg2 Bg7 d3 d6",
	"e4 c5 c3 Nf6 e5 Nd5 d4 cxd4 Nf3 Nc6",
	// Francesa / Caro-Kann / Escandinava / Pirc / Alekhine / Moderna
	"e4 e6 d4 d5 Nc3 Bb4 e5 c5 a3 Bxc3+ bxc3 Ne7",
	"e4 e6 d4 d5 Nd2 Nf6 e5 Nfd7 Bd3 c5 c3 Nc6",
	"e4 e6 d4 d5 Nc3 Nf6 Bg5 Be7 e5 Nfd7 Bxe7 Qxe7",
	"e4 c6 d4 d5 e5 Bf5 Nf3 e6 Be2 c5",
	"e4 c6 d4 d5 Nc3 dxe4 Nxe4 Bf5 Ng3 Bg6 h4 h6",
	"e4 d5 exd5 Qxd5 Nc3 Qa5 d4 Nf6 Nf3 Bf5",
	"e4 d6 d4 Nf6 Nc3 g6 Nf3 Bg7 Be2 O-O O-O",
	"e4 Nf6 e5 Nd5 d4 d6 Nf3 Bg4 Be2 e6",
	"e4 g6 d4 Bg7 Nc3 d6 Be3 a6 Qd2 b5",
	// Gambito da Dama
	"d4 d5 c4 e6 Nc3 Nf6 Bg5 Be7 e3 O-O Nf3 h6",
	"d4 d5 c4 e6 Nf3 Nf6 g3 dxc4 Bg2 a6 O-O b5",
	"d4 d5 c4 c6 Nf3 Nf6 Nc3 dxc4 a4 Bf5 e3 e6",
	"d4 d5 c4 c6 Nf3 Nf6 Nc3 e6 e3 Nbd7 Bd3 dxc4 Bxc4 b5",
	"d4 d5 c4 dxc4 Nf3 Nf6 e3 e6 Bxc4 c5 O-O a6",
	// Índias
	"d4 Nf6 c4 e6 Nc3 Bb4 Qc2 O-O a3 Bxc3+ Qxc3 b6",
	"d4 Nf6 c4 e6 Nc3 Bb4 e3 O-O Bd3 d5 Nf3 c5",
	"d4 Nf6 c4 e6 Nf3 b6 g3 Bb7 Bg2 Be7 O-O O-O",
	"d4 Nf6 c4 g6 Nc3 Bg7 e4 d6 Nf3 O-O Be2 e5 O-O Nc6",
	"d4 Nf6 c4 g6 Nc3 d5 cxd5 Nxd5 e4 Nxc3 bxc3 Bg7",
	"d4 Nf6 c4 c5 d5 e6 Nc3 exd5 cxd5 d6 e4 g6",
	"d4 Nf6 c4 e6 g3 d5 Bg2 Be7 Nf3 O-O O-O dxc4 Qc2 a6",
	// Sistemas de d4 / Holandesa
	"d4 d5 Nf3 Nf6 Bf4 e6 e3 c5 c3 Nc6",
	"d4 Nf6 Nf3 e6 Bg5 c5 e3 Be7",
	"d4 f5 g3 Nf6 Bg2 g6 Nf3 Bg7 O-O O-O c4 d6",
	// Inglesa / Réti
	"c4 e5 Nc3 Nf6 Nf3 Nc6 g3 d5 cxd5 Nxd5 Bg2 Nb6",
	"c4 c5 Nf3 Nc6 Nc3 g6 g3 Bg7 Bg2 Nf6 O-O O-O",
	"c4 Nf6 Nc3 e6 Nf3 d5 d4 Be7 Bf4 O-O",
	"Nf3 d5 g3 Nf6 Bg2 c6 O-O Bg4 d3 Nbd7",
	"Nf3 Nf6 c4 g6 Nc3 Bg7 e4 d6 d4 O-O",
}

type bookEntry struct {
	move   chess.Move
	weight int
}

var (
	book     map[uint64][]bookEntry
	bookOnce sync.Once
)

func buildBook() {
	book = map[uint64][]bookEntry{}
	for _, line := range BookLines {
		p, _ := chess.ParseFEN(chess.StartFEN)
		for _, san := range strings.Fields(line) {
			m, ok := p.ParseSAN(san)
			if !ok {
				break // testes garantem que não acontece
			}
			found := false
			for i := range book[p.Hash] {
				if book[p.Hash][i].move == m {
					book[p.Hash][i].weight++
					found = true
				}
			}
			if !found {
				book[p.Hash] = append(book[p.Hash], bookEntry{m, 1})
			}
			next, _ := p.Make(m)
			p = &next
		}
	}
}

// BookMove sorteia um lance do livro, com peso pela frequência.
func BookMove(p *chess.Position, r *rand.Rand) (chess.Move, bool) {
	bookOnce.Do(buildBook)
	entries := book[p.Hash]
	if len(entries) == 0 {
		return chess.NoMove, false
	}
	total := 0
	for _, e := range entries {
		total += e.weight
	}
	x := r.Intn(total)
	for _, e := range entries {
		if x < e.weight {
			return e.move, true
		}
		x -= e.weight
	}
	return entries[0].move, true
}
