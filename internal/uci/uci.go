package uci

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gochess/internal/book"
	"gochess/internal/chess"
	"gochess/internal/eval"
	"gochess/internal/search"
)

// Protocolo UCI: permite usar a engine em Arena, Cute Chess, En Croissant,
// lichess-bot etc. Suporta ponder, MultiPV, Threads, go nodes/mate/movetime.

var outMu sync.Mutex

func say(s string) {
	outMu.Lock()
	fmt.Println(s)
	outMu.Unlock()
}

func parsePosition(args []string) (*chess.Position, []uint64, error) {
	fen, i := chess.StartFEN, 2
	if len(args) > 1 && args[1] == "fen" {
		j := 2
		for j < len(args) && args[j] != "moves" {
			j++
		}
		fen, i = strings.Join(args[2:j], " "), j
	}
	pos, err := chess.ParseFEN(fen)
	if err != nil {
		return nil, nil, err
	}
	hist := []uint64{pos.Hash}
	if i < len(args) && args[i] == "moves" {
		for _, mv := range args[i+1:] {
			m, ok := pos.ParseUCIMove(mv)
			if !ok {
				return nil, nil, fmt.Errorf("lance ilegal: %s", mv)
			}
			next, _ := pos.Make(m)
			pos = &next
			hist = append(hist, pos.Hash)
		}
	}
	return pos, hist, nil
}

func parseGo(args []string) search.Limits {
	var lim search.Limits
	num := func(i int) int {
		if i < len(args) {
			v, _ := strconv.Atoi(args[i])
			return v
		}
		return 0
	}
	ms := func(i int) time.Duration { return time.Duration(num(i)) * time.Millisecond }
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "depth":
			lim.Depth = num(i + 1)
		case "nodes":
			lim.Nodes = uint64(num(i + 1))
		case "mate":
			lim.Mate = num(i + 1)
		case "movetime":
			lim.MoveTime = ms(i + 1)
		case "wtime":
			lim.WTime = ms(i + 1)
		case "btime":
			lim.BTime = ms(i + 1)
		case "winc":
			lim.WInc = ms(i + 1)
		case "binc":
			lim.BInc = ms(i + 1)
		case "movestogo":
			lim.MovesToGo = num(i + 1)
		case "infinite":
			lim.Infinite = true
		case "ponder":
			lim.Ponder = true
		}
	}
	return lim
}

func Loop() {
	pos, _ := chess.ParseFEN(chess.StartFEN)
	hist := []uint64{pos.Hash}
	s := search.NewSearcher(64)
	ownBook := true
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	var wg sync.WaitGroup
	var waitMu sync.Mutex
	release := make(chan struct{}) // libera o bestmove de "go infinite"/"go ponder"
	searching := false

	stopSearch := func() {
		s.Stop()
		waitMu.Lock()
		if searching {
			close(release)
			searching = false
		}
		waitMu.Unlock()
		wg.Wait()
	}

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		args := strings.Fields(sc.Text())
		if len(args) == 0 {
			continue
		}
		switch args[0] {
		case "uci":
			say("id name GoChess 0.3")
			say("id author Jo")
			say("option name Hash type spin default 64 min 1 max 4096")
			say("option name Threads type spin default 1 min 1 max 64")
			say("option name MultiPV type spin default 1 min 1 max 50")
			say("option name Ponder type check default false")
			say("option name OwnBook type check default true")
			say("option name UseNNUE type check default false")
			say("option name EvalFile type string default <empty>")
			say("option name SyzygyPath type string default <empty>")
			say("uciok")
		case "setoption":
			// setoption name <nome> value <valor>
			name, value := "", ""
			for i := 1; i < len(args); i++ {
				if args[i] == "name" && i+1 < len(args) {
					name = strings.ToLower(args[i+1])
				}
				if args[i] == "value" && i+1 < len(args) {
					value = args[i+1]
				}
			}
			n, _ := strconv.Atoi(value)
			stopSearch()
			switch name {
			case "hash":
				s.SetHash(max(1, n))
			case "threads":
				s.Threads = max(1, n)
			case "multipv":
				s.MultiPV = max(1, n)
			case "ownbook":
				ownBook = value == "true"
			case "evalfile":
				b, err := os.ReadFile(value)
				if err != nil {
					say("info string não consegui ler a rede: " + err.Error())
					continue
				}
				net, err := eval.LoadNet(b)
				if err != nil {
					say("info string rede inválida: " + err.Error())
					continue
				}
				eval.CurrentNet, s.UseNNUE = net, true
				say("info string rede NNUE carregada de " + value)
			case "syzygypath":
				if !search.SyzygySupported() {
					say("info string esta build não tem suporte a Syzygy (compile com cgo)")
					continue
				}
				if n := search.SetSyzygyPath(value); n > 0 {
					say(fmt.Sprintf("info string Syzygy: tablebases de até %d peças carregadas", n))
				} else {
					say("info string Syzygy: nenhuma tablebase encontrada em " + value)
				}
			case "usennue":
				s.UseNNUE = value == "true"
				if s.UseNNUE && eval.CurrentNet == nil {
					say("info string nenhuma rede NNUE embutida; usando a avaliação clássica")
				}
			}
		case "isready":
			say("readyok")
		case "ucinewgame":
			stopSearch()
			pos, _ = chess.ParseFEN(chess.StartFEN)
			hist = []uint64{pos.Hash}
			s.Clear()
		case "position":
			stopSearch()
			np, nh, err := parsePosition(args)
			if err != nil {
				say("info string " + err.Error())
				continue
			}
			pos, hist = np, nh
		case "go":
			stopSearch()
			lim := parseGo(args)
			if ownBook && !lim.Infinite && !lim.Ponder && lim.Mate == 0 && s.MultiPV == 1 {
				if m, ok := book.BookMove(pos, rng); ok {
					say("info string lance do livro")
					say("bestmove " + m.String())
					continue
				}
			}
			root, h := *pos, append([]uint64(nil), hist...)
			s.ResetStop()
			waitMu.Lock()
			release = make(chan struct{})
			searching = lim.Infinite || lim.Ponder
			rel := release
			waitMu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				res := s.Search(&root, h, lim, say)
				// Em "infinite" e "ponder" o bestmove só sai após stop/ponderhit.
				if lim.Infinite || lim.Ponder {
					<-rel
				}
				out := "bestmove " + res.Best.String()
				if len(res.PV) > 1 {
					out += " ponder " + res.PV[1].String()
				}
				say(out)
			}()
		case "ponderhit":
			s.PonderHit()
			waitMu.Lock()
			if searching {
				// Continua a busca com o tempo contando; o bestmove sai quando ela terminar.
				close(release)
				searching = false
			}
			waitMu.Unlock()
		case "stop":
			stopSearch()
		case "quit":
			stopSearch()
			return
		case "d":
			say(pos.String())
			say("FEN: " + pos.FEN())
			say(fmt.Sprintf("Avaliação estática: %d", eval.Evaluate(pos)))
		case "perft":
			depth := 4
			if len(args) > 1 {
				depth, _ = strconv.Atoi(args[1])
			}
			t := time.Now()
			n := chess.Perft(pos, depth)
			say(fmt.Sprintf("perft %d: %d nós em %v", depth, n, time.Since(t).Round(time.Millisecond)))
		}
	}
	stopSearch()
}
