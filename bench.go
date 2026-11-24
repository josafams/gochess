//go:build !(js && wasm)

package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"time"
)

// bench: busca de profundidade fixa num conjunto fixo de posições.
// O total de nós é a "assinatura" da busca: otimizações de velocidade não
// podem mudá-lo; o NPS mede a velocidade.
//
//	gochess bench [-depth 13] [-net nnue-v1.nnue] [-cpuprofile cpu.out]
var benchFENs = []string{
	"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
	"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
	"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
	"r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1",
	"rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8",
	"r4rk1/1pp1qppp/p1np1n2/2b1p1B1/2B1P1b1/P1NP1N2/1PP1QPPP/R4RK1 w - - 0 10",
	"r1bqkbnr/pppp1ppp/2n5/4p3/4P3/5N2/PPPP1PPP/RNBQKB1R w KQkq - 2 3",
	"r1bqkb1r/pppp1ppp/2n2n2/4p3/2B1P3/5N2/PPPP1PPP/RNBQK2R w KQkq - 4 4",
	"rnbqkb1r/pp2pppp/3p1n2/8/3NP3/2N5/PPP2PPP/R1BQKB1R b KQkq - 2 5",
	"rnbqk2r/ppp1bppp/4pn2/3p4/2PP4/2N2N2/PP2PPPP/R1BQKB1R w KQkq - 4 5",
	"r1bq1rk1/pp2ppbp/2np1np1/8/3NP3/2N1BP2/PPPQ2PP/R3KB1R w KQ - 3 9",
	"r2q1rk1/pp1bppbp/2np1np1/8/2BNP3/2N1BP2/PPPQ2PP/R3K2R w KQ - 5 10",
	"2rq1rk1/pp1bppbp/3p1np1/4n3/3NP3/1BN1BP2/PPPQ2PP/2KR3R w - - 4 13",
	"r1b2rk1/2q1bppp/p2ppn2/1p6/3BPP2/2NB4/PPPQ2PP/2KR3R w - - 0 14",
	"r2qr1k1/1p1nbppp/p2pbn2/4p3/4P3/1NN1BP2/PPPQ2PP/2KR1B1R w - - 4 12",
	"r1bqr1k1/pp3pbp/2pp1np1/3Pn3/2P1P3/2N2N2/PP2BPPP/R1BQR1K1 w - - 1 11",
	"2r2rk1/1bqnbppp/pp1ppn2/8/2PNP3/1PN1BP2/P2QB1PP/2RR2K1 w - - 2 15",
	"r4rk1/pp2qppp/2nbpn2/3p4/2PP4/1PN1PN2/P2Q1PPP/R3KB1R w KQ - 1 12",
	"3r1rk1/pp2qppp/2n1pn2/3p4/2PP4/PQ2PN2/5PPP/2R1KB1R w K - 3 15",
	"2rr2k1/pp2qppp/2n1pn2/8/2pP4/P1Q1PN2/4BPPP/2RR2K1 w - - 0 18",
	"6k1/pp3ppp/2n1p3/8/3P4/P3PN2/5PPP/6K1 w - - 0 25",
	"8/pp3kpp/2n1p3/8/3P4/P3P3/4KPPP/3N4 w - - 2 28",
	"8/5kpp/p3p3/1p6/3P4/P3P3/4KPPP/8 w - - 0 32",
	"8/8/4k3/8/2pK4/8/1P6/8 w - - 0 1",
	"8/8/1p1k4/1P6/2K5/8/8/8 w - - 0 1",
	"6k1/5ppp/8/8/8/8/5PPP/3R2K1 w - - 0 1",
	"4r1k1/5ppp/8/8/8/8/3R1PPP/6K1 w - - 0 1",
	"8/5pk1/6p1/7p/7P/6P1/5PK1/3B4 w - - 0 1",
	"8/8/3k4/3p4/3P4/3K4/8/8 w - - 0 1",
	"8/3k4/8/8/8/8/3K4/3Q4 w - - 0 1",
	"r1bq1rk1/ppp2ppp/2n2n2/3pp3/1bPP4/2N1PN2/PP1B1PPP/R2QKB1R w KQ - 0 7",
	"rnbqkbnr/pp1ppppp/8/2p5/4P3/8/PPPP1PPP/RNBQKBNR w KQkq c6 0 2",
	"rnbqkbnr/ppp1pppp/8/3p4/3P4/8/PPP1PPPP/RNBQKBNR w KQkq d6 0 2",
	"r1bqkbnr/pppp1ppp/2n5/1B2p3/4P3/5N2/PPPP1PPP/RNBQK2R b KQkq - 3 3",
	"rnbqkb1r/ppp2ppp/4pn2/3p4/2PP4/2N5/PP2PPPP/R1BQKBNR w KQkq - 0 4",
	"r2qkb1r/pp2pppp/2n2n2/3p1b2/3P1B2/2N1PN2/PP3PPP/R2QKB1R w KQkq - 1 7",
	"rn1qk2r/pbppbppp/1p2pn2/8/2PP4/P1N2N2/1P2PPPP/R1BQKB1R w KQkq - 1 6",
	"r1b1k2r/ppppqppp/2n2n2/2b1p3/2B1P3/2NP1N2/PPP2PPP/R1BQK2R w KQkq - 1 6",
	"3k4/8/8/8/8/8/8/2K1Q3 w - - 0 1",
	"5rk1/1p3ppp/p1p5/2b5/4P3/2N5/PPP3PP/5R1K b - - 0 22",
}

func runBench(args []string) {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	depth := fs.Int("depth", 12, "profundidade de cada busca")
	netPath := fs.String("net", "nnue-v1.nnue", "rede NNUE (\"\" = avaliação clássica)")
	cpuprofile := fs.String("cpuprofile", "", "grava o perfil de CPU (go tool pprof)")
	hash := fs.Int("hash", 16, "tabela de transposição (MB)")
	memprofile := fs.String("memprofile", "", "grava o perfil de alocações (go tool pprof -sample_index=alloc_space)")
	fs.Parse(args)

	s := NewSearcher(*hash)
	if *netPath != "" {
		b, err := os.ReadFile(*netPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		net, err := LoadNet(b)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		currentNet, s.UseNNUE = net, true
	}
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	var ms0 runtime.MemStats
	runtime.ReadMemStats(&ms0)
	var nodes uint64
	var elapsed time.Duration
	for i, fen := range benchFENs {
		p, err := ParseFEN(fen)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FEN %d inválido: %v\n", i+1, err)
			os.Exit(1)
		}
		s.Clear()
		s.ResetStop()
		res := s.Search(p, nil, Limits{Depth: *depth}, nil)
		nodes += res.Nodes
		elapsed += res.Time
		fmt.Printf("%2d  %-8s %10d nós  %s\n", i+1, res.Best, res.Nodes, res.Time.Round(time.Millisecond))
	}
	if *memprofile != "" {
		if f, err := os.Create(*memprofile); err == nil {
			pprof.Lookup("allocs").WriteTo(f, 0)
			f.Close()
		}
	}
	var ms1 runtime.MemStats
	runtime.ReadMemStats(&ms1)
	nps := uint64(0)
	if elapsed > 0 {
		nps = uint64(float64(nodes) / elapsed.Seconds())
	}
	fmt.Printf("\n%d nós  %d nps  %s  (alocações: %.1f MB, %d GCs)\n",
		nodes, nps, elapsed.Round(time.Millisecond),
		float64(ms1.TotalAlloc-ms0.TotalAlloc)/1e6, ms1.NumGC-ms0.NumGC)
}
