//go:build !(js && wasm)

package main

import (
	"bufio"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// datagen: autojogo para gerar dados de treino (tuning e NNUE).
// Cada linha: FEN | nota da busca (pov brancas) | resultado (1, 0.5, 0 pov brancas)
//
//	gochess datagen -games 4000 -nodes 5000 -workers 2 -out dados.txt

func runDatagen(args []string) {
	fs := flag.NewFlagSet("datagen", flag.ExitOnError)
	games := fs.Int("games", 1000, "número de partidas")
	nodes := fs.Int("nodes", 5000, "nós por lance")
	workers := fs.Int("workers", 2, "partidas em paralelo")
	out := fs.String("out", "dados.txt", "arquivo de saída")
	dur := fs.Duration("duration", 0, "parar após este tempo (ex.: 20m)")
	fs.Parse(args)

	f, err := os.OpenFile(*out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	var mu sync.Mutex
	var done, positions atomic.Int64
	start := time.Now()
	deadline := time.Time{}
	if *dur > 0 {
		deadline = start.Add(*dur)
	}

	var wg sync.WaitGroup
	next := atomic.Int64{}
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			r := rand.New(rand.NewSource(seed))
			s := NewSearcher(16)
			for next.Add(1) <= int64(*games) {
				if !deadline.IsZero() && time.Now().After(deadline) {
					return
				}
				lines := playGame(s, r, uint64(*nodes))
				mu.Lock()
				for _, l := range lines {
					w.WriteString(l)
				}
				mu.Unlock()
				positions.Add(int64(len(lines)))
				if n := done.Add(1); n%100 == 0 {
					fmt.Fprintf(os.Stderr, "%d partidas, %d posições, %v\n", n, positions.Load(), time.Since(start).Round(time.Second))
				}
			}
		}(time.Now().UnixNano() + int64(i)*7919)
	}
	wg.Wait()
	fmt.Fprintf(os.Stderr, "fim: %d partidas, %d posições\n", done.Load(), positions.Load())
}

func playGame(s *Searcher, r *rand.Rand, nodes uint64) []string {
	s.Clear()
	pos, _ := ParseFEN(StartFEN)
	hist := []uint64{pos.Hash}

	// Abertura: 8 ou 9 lances aleatórios para variar as posições.
	for i := 0; i < 8+r.Intn(2); i++ {
		moves := pos.LegalMoves()
		if len(moves) == 0 {
			return nil
		}
		n, _ := pos.Make(moves[r.Intn(len(moves))])
		pos = &n
		hist = append(hist, pos.Hash)
	}

	type rec struct {
		fen   string
		score int
	}
	var recs []rec
	result := -1.0
	winStreak, drawStreak := 0, 0
	for ply := 0; ply < 400; ply++ {
		legal := pos.LegalMoves()
		if len(legal) == 0 {
			if pos.InCheck() {
				result = map[bool]float64{true: 0, false: 1}[pos.Side == White]
			} else {
				result = 0.5
			}
			break
		}
		if pos.Halfmove >= 100 || pos.InsufficientMaterial() || countRep(hist, pos.Hash) >= 3 {
			result = 0.5
			break
		}
		s.ResetStop()
		res := s.Search(pos, hist, Limits{Nodes: nodes}, nil)
		score := res.Score
		white := score
		if pos.Side == Black {
			white = -score
		}
		if ply == 0 && absInt(score) > 300 {
			return nil // abertura aleatória ruim demais
		}
		// Posições "quietas": fora de xeque e com lance escolhido não tático.
		quiet := !pos.InCheck() && res.Best&FlagCapture == 0 && res.Best.Promo() == 0 && absInt(score) < Mate-2*MaxPly
		if quiet {
			recs = append(recs, rec{pos.FEN(), white})
		}
		// Adjudicação.
		if absInt(score) >= 1500 {
			winStreak++
		} else {
			winStreak = 0
		}
		if ply > 60 && absInt(score) <= 10 {
			drawStreak++
		} else {
			drawStreak = 0
		}
		if winStreak >= 6 {
			if white > 0 {
				result = 1
			} else {
				result = 0
			}
			break
		}
		if drawStreak >= 12 {
			result = 0.5
			break
		}
		n, _ := pos.Make(res.Best)
		pos = &n
		hist = append(hist, pos.Hash)
	}
	if result < 0 {
		result = 0.5
	}
	out := make([]string, 0, len(recs))
	for _, rc := range recs {
		out = append(out, fmt.Sprintf("%s | %d | %.1f\n", rc.fen, rc.score, result))
	}
	return out
}

func countRep(hist []uint64, h uint64) int {
	n := 0
	for _, x := range hist {
		if x == h {
			n++
		}
	}
	return n
}
