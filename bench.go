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
//	gochess bench [-depth 13] [-net gochess-768x512.nnue] [-cpuprofile cpu.out]

func runBench(args []string) {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	depth := fs.Int("depth", 12, "profundidade de cada busca")
	netPath := fs.String("net", "gochess-768x512.nnue", "rede NNUE (\"\" = avaliação clássica)")
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
