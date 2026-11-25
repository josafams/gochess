package main

import "testing"

// isPseudoLegal tem de concordar com o gerador, e as etapas juntas têm de
// entregar exatamente os mesmos lances que generateInto.
func TestStagedGeneration(t *testing.T) {
	fens := []string{
		StartFEN,
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
		"r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1",
		"rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8",
	}
	var pool []Move // lances vistos em outras posições: candidatos "estranhos"
	checked := 0
	var walk func(p *Position, depth int)
	walk = func(p *Position, depth int) {
		all := p.GenerateMoves(false)
		in := map[Move]bool{}
		for _, m := range all {
			in[m] = true
			if !p.isPseudoLegal(m) {
				t.Fatalf("%s: lance gerado %s rejeitado", p.FEN(), m)
			}
		}
		for _, m := range pool[max(0, len(pool)-200):] {
			if p.isPseudoLegal(m) != in[m] {
				t.Fatalf("%s: isPseudoLegal(%s)=%v, gerado=%v", p.FEN(), m, !in[m], in[m])
			}
			checked++
		}
		noisy := p.generateInto(nil, true)
		quiet := p.generateQuietsInto(nil)
		if len(noisy)+len(quiet) != len(all) {
			t.Fatalf("%s: %d + %d != %d lances", p.FEN(), len(noisy), len(quiet), len(all))
		}
		for _, m := range quiet {
			if !in[m] || m&FlagCapture != 0 || m.Promo() != 0 {
				t.Fatalf("%s: quieto inválido %s", p.FEN(), m)
			}
		}
		pool = append(pool, all...)
		if depth == 0 {
			return
		}
		for _, m := range all {
			if c, ok := p.Make(m); ok {
				walk(&c, depth-1)
			}
		}
	}
	for _, fen := range fens {
		p, _ := ParseFEN(fen)
		walk(p, 2)
	}
	if checked < 10000 {
		t.Fatalf("poucos candidatos testados: %d", checked)
	}
}
