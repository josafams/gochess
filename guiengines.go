//go:build !(js && wasm)

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Adversários da interface web além da engine embutida: motores UCI
// externos (Stockfish, ou o próprio GoChess com uma rede NNUE), cada um
// num processo que fica aberto entre os lances.

type guiEngine struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	path      string   // "" = engine embutida
	opts      [][2]string
	stockfish bool // nível vira UCI_Elo

	mu  sync.Mutex
	uci *uciEngine
}

var guiEngines []*guiEngine

// detectGUIEngines monta a lista com o que existir no disco.
func detectGUIEngines(netPath string) {
	guiEngines = []*guiEngine{{ID: "gochess", Name: "GoChess"}}
	self, _ := os.Executable()
	if netPath != "" && self != "" {
		if abs, err := filepath.Abs(netPath); err == nil {
			if _, err := os.Stat(abs); err == nil {
				guiEngines = append(guiEngines, &guiEngine{ID: "gochess-nnue", Name: "GoChess + NNUE", path: self,
					opts: [][2]string{{"OwnBook", "false"}, {"Hash", "64"}, {"EvalFile", abs}}})
			}
		}
	}
	home, _ := os.UserHomeDir()
	for _, sf := range []struct{ id, name, file string }{
		{"sf17", "Stockfish 17.1", "chess-tools/sf"},
		{"sf19", "Stockfish 19", "chess-tools/sf19bin"},
	} {
		p := filepath.Join(home, sf.file)
		if _, err := os.Stat(p); err == nil {
			guiEngines = append(guiEngines, &guiEngine{ID: sf.id, Name: sf.name, path: p, stockfish: true,
				opts: [][2]string{{"Hash", "128"}, {"Threads", "4"}}})
		}
	}
}

func findGUIEngine(id string) *guiEngine {
	for _, e := range guiEngines {
		if e.ID == id {
			return e
		}
	}
	return nil
}

// Nível 1–6 do Stockfish: 1320 é o mínimo que ele aceita; 6 = força total.
var sfLevelElo = [7]int{0, 1320, 1600, 1900, 2200, 2600, 0}

// externalThink responde a um "think" com um motor externo.
func externalThink(ge *guiEngine, req APIRequest) APIResponse {
	state := req
	state.Action = "state"
	resp := HandleAPI(state)
	if resp.Error != "" || resp.Status != "playing" {
		return resp
	}
	pos, _, _, err := replay(req.FEN, req.Moves)
	if err != nil {
		return APIResponse{Error: err.Error()}
	}

	ge.mu.Lock()
	defer ge.mu.Unlock()
	if ge.uci == nil {
		e, err := startEngine(ge.path, ge.opts)
		if err != nil {
			return APIResponse{Error: "não foi possível abrir " + ge.Name + ": " + err.Error()}
		}
		ge.uci = e
	}
	e := ge.uci

	level := max(1, min(req.Level, 6))
	if ge.stockfish {
		if elo := sfLevelElo[level]; elo > 0 {
			e.send("setoption name UCI_LimitStrength value true")
			e.send("setoption name UCI_Elo value " + strconv.Itoa(elo))
		} else {
			e.send("setoption name UCI_LimitStrength value false")
		}
	}
	if len(req.Moves) == 0 {
		e.send("ucinewgame")
	}
	if err := e.ready(); err != nil {
		e.kill()
		ge.uci = nil
		return APIResponse{Error: ge.Name + " não respondeu"}
	}

	position := "position startpos"
	if req.FEN != "" {
		position = "position fen " + req.FEN
	}
	if len(req.Moves) > 0 {
		position += " moves " + strings.Join(req.Moves, " ")
	}
	e.send(position)

	lim := levelLimits(level)
	var goCmd string
	switch {
	case req.WTime > 0 || req.BTime > 0:
		goCmd = fmt.Sprintf("go wtime %d btime %d winc %d binc %d", req.WTime, req.BTime, req.Inc, req.Inc)
	case ge.stockfish && level < 6:
		goCmd = "go movetime 500"
	case lim.Depth > 0:
		goCmd = "go depth " + strconv.Itoa(lim.Depth)
	default:
		goCmd = "go movetime " + strconv.FormatInt(lim.MoveTime.Milliseconds(), 10)
	}
	start := time.Now()
	e.send(goCmd)

	var last string
	for {
		l, err := e.next(5 * time.Minute)
		if err != nil {
			e.kill()
			ge.uci = nil
			return APIResponse{Error: ge.Name + " parou de responder"}
		}
		if strings.HasPrefix(l, "info ") && strings.Contains(l, " score ") && strings.Contains(l, " pv ") &&
			!strings.Contains(l, "multipv 2") && !strings.Contains(l, "bound") {
			last = l
		}
		if strings.HasPrefix(l, "bestmove ") {
			f := strings.Fields(l)
			m, ok := pos.ParseUCIMove(f[1])
			if !ok {
				return APIResponse{Error: ge.Name + " jogou um lance inválido: " + f[1]}
			}
			resp.Best, resp.BestSAN = m.String(), pos.SAN(m)
			resp.TimeMs = time.Since(start).Milliseconds()
			fillFromInfo(&resp, pos, last)
			return resp
		}
	}
}

// fillFromInfo copia nota (pov brancas), profundidade, nós e variante.
func fillFromInfo(resp *APIResponse, pos *Position, line string) {
	f := strings.Fields(line)
	for i := 0; i+1 < len(f); i++ {
		switch f[i] {
		case "depth":
			resp.Depth, _ = strconv.Atoi(f[i+1])
		case "nodes":
			resp.Nodes, _ = strconv.ParseUint(f[i+1], 10, 64)
		case "score":
			if i+2 >= len(f) {
				continue
			}
			v, _ := strconv.Atoi(f[i+2])
			if pos.Side == Black {
				v = -v
			}
			if f[i+1] == "cp" {
				resp.EvalCp = v
			} else if f[i+1] == "mate" {
				resp.EvalCp, resp.MateIn = 0, v
			}
		case "pv":
			var pv []Move
			p := *pos
			for _, s := range f[i+1:] {
				m, ok := p.ParseUCIMove(s)
				if !ok {
					break
				}
				pv = append(pv, m)
				p, _ = p.Make(m)
			}
			resp.PV = sanLine(pos, pv)
			return
		}
	}
}
