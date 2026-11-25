package main

import (
	"math/rand"
	"testing"
)

// Os kernels AVX2 têm que dar exatamente o mesmo que o código em Go puro.
func TestNNUESIMD(t *testing.T) {
	if !hasAVX2 {
		t.Skip("CPU sem AVX2")
	}
	r := rand.New(rand.NewSource(1))
	n := &Net{}
	for i := range n.FW {
		for j := range n.FW[i] {
			n.FW[i][j] = int16(r.Intn(1001) - 500)
		}
	}
	for j := range n.FB {
		n.FB[j] = int16(r.Intn(601) - 300)
	}
	for j := range n.OW {
		n.OW[j] = int16(r.Intn(257) - 128)
	}
	n.OB = 1234
	n.prepare()
	if !n.simd {
		t.Fatal("rede deveria usar SIMD")
	}
	ref := *n
	ref.simd = false

	fens := []string{
		StartFEN,
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
	}
	for _, fen := range fens {
		p, _ := ParseFEN(fen)
		var a accumulator
		a.refresh(n, p)
		if got, want := n.output(&a, p.Side), ref.output(&a, p.Side); got != want {
			t.Errorf("%s: output %d, esperado %d", fen, got, want)
		}
		// Todos os lances (quietos, capturas, roques, en passant, promoções):
		// acumulador incremental == recalculado do zero.
		for _, m := range p.GenerateMoves(false) {
			child, ok := p.Make(m)
			if !ok {
				continue
			}
			st := &nnueStack{}
			st.enable(n, p)
			st.push(0, p, m)
			got := st.evaluate(&child, 1)
			var full accumulator
			full.refresh(n, &child)
			if st.entries[1].acc != full {
				t.Fatalf("%s %s: acumulador incremental difere", fen, m)
			}
			if want := ref.output(&full, child.Side); got != want {
				t.Fatalf("%s %s: nota %d, esperado %d", fen, m, got, want)
			}
		}
	}
}

// screluDot2AVX2 tem de dar exatamente a soma escalar das duas perspectivas.
func TestScreluDot2AVX2(t *testing.T) {
	if !hasAVX2 {
		t.Skip("sem AVX2")
	}
	r := rand.New(rand.NewSource(3))
	for iter := 0; iter < 200; iter++ {
		us, them := make([]int16, nnHidden), make([]int16, nnHidden)
		w := make([]int16, 2*nnHidden)
		for i := range us {
			us[i] = int16(r.Intn(700) - 200)
			them[i] = int16(r.Intn(700) - 200)
		}
		for i := range w {
			w[i] = int16(r.Intn(257) - 128)
		}
		var want int64
		for j := 0; j < nnHidden; j++ {
			want += int64(screlu(us[j]))*int64(w[j]) + int64(screlu(them[j]))*int64(w[nnHidden+j])
		}
		if got := screluDot2AVX2(&us[0], &them[0], &w[0], nnHidden); got != want {
			t.Fatalf("iteração %d: %d != %d", iter, got, want)
		}
	}
}
