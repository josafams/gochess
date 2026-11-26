package search

import (
	"testing"

	"gochess/internal/chess"
)

func TestThreads(t *testing.T) {
	p, _ := chess.ParseFEN("2r3k1/5ppp/8/8/8/8/3R1PPP/3R2K1 w - - 0 1")
	s := NewSearcher(16)
	s.Threads = 2
	res := s.Search(p, nil, Limits{Depth: 8}, nil)
	if res.Best.String() != "d2d8" {
		t.Errorf("com 2 threads: %s", res.Best)
	}
}

func TestMultiPV(t *testing.T) {
	p, _ := chess.ParseFEN(chess.StartFEN)
	s := NewSearcher(16)
	s.MultiPV = 3
	res := s.Search(p, nil, Limits{Depth: 5}, nil)
	if len(res.Lines) != 3 || res.Lines[0].Move == res.Lines[1].Move || res.Lines[0].Score < res.Lines[2].Score {
		t.Errorf("MultiPV inconsistente: %+v", res.Lines)
	}
}

func TestNodesAndMateLimits(t *testing.T) {
	p, _ := chess.ParseFEN(chess.StartFEN)
	s := NewSearcher(16)
	res := s.Search(p, nil, Limits{Nodes: 20000}, nil)
	if res.Nodes > 30000 {
		t.Errorf("go nodes 20000 buscou %d", res.Nodes)
	}
	q, _ := chess.ParseFEN("2r3k1/5ppp/8/8/8/8/3R1PPP/3R2K1 w - - 0 1")
	s.ResetStop()
	res = s.Search(q, nil, Limits{Mate: 2}, nil)
	if ScoreString(res.Score) != "mate 2" {
		t.Errorf("go mate 2: %s", ScoreString(res.Score))
	}
}
