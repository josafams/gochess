//go:build !(js && wasm)

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// lichess: converte a base de avaliações do Lichess (CC0,
// https://database.lichess.org/#evals) para o formato do datagen:
//
//	FEN | nota (pov brancas) | resultado esperado
//
// A base não tem resultado de partida, então o terceiro campo é
// sigmoid(nota/400): com qualquer lambda o alvo do treino vira a própria nota.
//
//	zstdcat lichess_db_eval.jsonl.zst | gochess lichess -out lichess.txt

type lichessLine struct {
	FEN   string `json:"fen"`
	Evals []struct {
		Depth int `json:"depth"`
		PVs   []struct {
			CP   *int   `json:"cp"`
			Mate *int   `json:"mate"`
			Line string `json:"line"`
		} `json:"pvs"`
	} `json:"evals"`
}

// convertLichess devolve o registro de saída (texto, ou 32 bytes se binary),
// ou "" se a posição for descartada.
func convertLichess(raw []byte, minDepth, maxCP int, binary bool) string {
	var l lichessLine
	if json.Unmarshal(raw, &l) != nil || len(l.Evals) == 0 {
		return ""
	}
	best := 0
	for i, e := range l.Evals {
		if e.Depth > l.Evals[best].Depth {
			best = i
		}
	}
	e := l.Evals[best]
	if e.Depth < minDepth || len(e.PVs) == 0 || e.PVs[0].CP == nil {
		return "" // sem nota em centipeões (mate) ou rasa demais
	}
	cp := *e.PVs[0].CP
	if absInt(cp) > maxCP {
		return ""
	}
	fen := l.FEN + " 0 1"
	p, err := ParseFEN(fen)
	if err != nil || p.InCheck() || p.All.Count() > 32 {
		return "" // > 32 peças: posição montada à mão, não cabe no formato compacto
	}
	// Posição "quieta": o melhor lance não é captura nem promoção.
	first, _, _ := strings.Cut(e.PVs[0].Line, " ")
	// A base usa a notação de roque do Chess960 (rei "captura" a torre).
	switch first {
	case "e1h1":
		first = "e1g1"
	case "e1a1":
		first = "e1c1"
	case "e8h8":
		first = "e8g8"
	case "e8a8":
		first = "e8c8"
	}
	m, ok := p.ParseUCIMove(first)
	if !ok || m&FlagCapture != 0 || m.Promo() != 0 {
		return ""
	}
	if _, special := evalEndgame(p); special {
		return ""
	}
	if binary {
		rec := packPosition(p, cp, -1)
		return string(rec[:])
	}
	return fmt.Sprintf("%s | %d | %.3f\n", fen, cp, sigmoid(float64(cp)/400))
}

func runLichess(args []string) {
	fs := flag.NewFlagSet("lichess", flag.ExitOnError)
	out := fs.String("out", "lichess.bin", "arquivo de saída (.bin = formato compacto; senão texto)")
	minDepth := fs.Int("mindepth", 20, "profundidade mínima da avaliação")
	maxCP := fs.Int("maxcp", 2000, "descarta notas maiores que isto (em módulo)")
	limit := fs.Int64("limit", 0, "parar após N posições gravadas (0 = todas)")
	workers := fs.Int("workers", 8, "threads de conversão")
	fs.Parse(args)

	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)
	defer w.Flush()

	isBin := strings.HasSuffix(*out, ".bin")
	in := make(chan [][]byte, 64)
	outc := make(chan []string, 64)
	var read, kept atomic.Int64
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range in {
				res := make([]string, 0, len(batch))
				for _, raw := range batch {
					if s := convertLichess(raw, *minDepth, *maxCP, isBin); s != "" {
						res = append(res, s)
					}
				}
				outc <- res
			}
		}()
	}
	done := make(chan struct{})
	stop := atomic.Bool{}
	go func() {
		defer close(done)
		for res := range outc {
			for _, s := range res {
				if *limit > 0 && kept.Load() >= *limit {
					stop.Store(true)
					break
				}
				w.WriteString(s)
				kept.Add(1)
			}
		}
	}()

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	batch := make([][]byte, 0, 4096)
	for sc.Scan() && !stop.Load() {
		batch = append(batch, append([]byte(nil), sc.Bytes()...))
		if len(batch) == cap(batch) {
			in <- batch
			batch = make([][]byte, 0, 4096)
		}
		if n := read.Add(1); n%5_000_000 == 0 {
			fmt.Fprintf(os.Stderr, "%d lidas, %d gravadas, %v\n", n, kept.Load(), time.Since(start).Round(time.Second))
		}
	}
	if len(batch) > 0 {
		in <- batch
	}
	close(in)
	wg.Wait()
	close(outc)
	<-done
	fmt.Fprintf(os.Stderr, "fim: %d lidas, %d gravadas (%v)\n", read.Load(), kept.Load(), time.Since(start).Round(time.Second))
}
