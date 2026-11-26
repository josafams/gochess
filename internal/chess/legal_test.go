package chess

import "testing"

// TestMakeFast: em posições variadas, MakeFast aceita exatamente os mesmos
// lances que Make e produz a mesma posição.
func TestMakeFast(t *testing.T) {
	var walk func(p *Position, depth int)
	n := 0
	walk = func(p *Position, depth int) {
		inCheck := p.InCheck()
		pinned := p.Pinned()
		for _, m := range p.GenerateMoves(false) {
			a, okA := p.Make(m)
			b, okB := p.MakeFast(m, pinned, inCheck)
			if okA != okB || okA && a != b {
				t.Fatalf("lance %v: Make=%v MakeFast=%v", m, okA, okB)
			}
			n++
			if okA && depth > 1 {
				walk(&a, depth-1)
			}
		}
	}
	for _, fen := range BenchFENs {
		p, err := ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		walk(p, 3)
	}
	t.Logf("%d lances conferidos", n)
}
