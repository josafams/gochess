package chess

import "testing"

func TestSEE(t *testing.T) {
	cases := []struct {
		fen, move string
		want      int
	}{
		{"1k1r4/1pp4p/p7/4p3/8/P5P1/1PP4P/2K1R3 w - - 0 1", "e1e5", 100},           // peão indefeso
		{"1k1r3q/1ppn3p/p4b2/4p3/8/P2N2P1/1PP1R1BP/2K1Q3 w - - 0 1", "d3e5", -220}, // N x P, perde o cavalo
		{"4k3/8/8/3p4/4P3/8/8/4K3 w - - 0 1", "e4d5", 100},
		{"4k3/8/2p5/3p4/4P3/8/8/4K3 w - - 0 1", "e4d5", 0},
		{"4k3/8/8/3q4/8/8/3R4/3RK3 w - - 0 1", "d2d5", 900}, // torre captura dama indefesa
	}
	for _, c := range cases {
		p, _ := ParseFEN(c.fen)
		m, ok := p.ParseUCIMove(c.move)
		if !ok {
			t.Fatalf("lance %s ilegal", c.move)
		}
		if got := p.SEE(m); got != c.want {
			t.Errorf("SEE %s em %s = %d, esperado %d", c.move, c.fen, got, c.want)
		}
	}
}
