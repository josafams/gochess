package api

import (
	"math/rand"
	"strings"
	"sync"
	"time"

	"gochess/internal/book"
	"gochess/internal/chess"
	"gochess/internal/search"
)

// API em JSON usada pela interface web. O cliente não guarda estado no
// servidor: manda sempre a lista de lances desde o início e recebe a
// posição resultante. O mesmo código roda no servidor local (gui.go) e
// dentro do navegador via WebAssembly (wasm.go).

type APIRequest struct {
	FEN     string   `json:"fen,omitempty"`
	Moves   []string `json:"moves"`
	Action  string   `json:"action"` // "state", "think", "analyze", "new", "pgn"
	Level   int      `json:"level"`  // 1 a 6
	MultiPV int      `json:"multipv,omitempty"`
	TimeMs  int      `json:"timeMs,omitempty"` // análise: tempo de busca
	WTime   int      `json:"wtime,omitempty"`  // relógio (ms), se houver
	BTime   int      `json:"btime,omitempty"`
	Inc     int      `json:"inc,omitempty"`
	PGN     string   `json:"pgn,omitempty"` // action "pgn": importa lances em SAN
	Book    bool     `json:"book,omitempty"`
	Engine  string   `json:"engine,omitempty"` // adversário (só no servidor local): gochess, sf17, sf19...
}

type APILine struct {
	Move   string   `json:"move"`
	SAN    string   `json:"san"`
	EvalCp int      `json:"evalCp"`
	MateIn int      `json:"mateIn,omitempty"`
	PV     []string `json:"pv"`
}

type APIResponse struct {
	Error  string   `json:"error,omitempty"`
	FEN    string   `json:"fen"`
	Turn   string   `json:"turn"`  // "w" ou "b"
	Board  string   `json:"board"` // 64 caracteres, a1..h8, "." = vazio
	Legal  []string `json:"legal"`
	SAN    []string `json:"san"` // lances jogados em notação algébrica
	Check  bool     `json:"check"`
	Status string   `json:"status"` // playing, checkmate, stalemate, fifty, repetition, material
	Winner string   `json:"winner,omitempty"`

	// Preenchidos por "think"
	Best     string    `json:"best,omitempty"`
	BestSAN  string    `json:"bestSan,omitempty"`
	EvalCp   int       `json:"evalCp"`           // centipeões, ponto de vista das brancas
	MateIn   int       `json:"mateIn,omitempty"` // >0 brancas dão mate, <0 pretas
	Depth    int       `json:"depth,omitempty"`
	Nodes    uint64    `json:"nodes,omitempty"`
	TimeMs   int64     `json:"timeMs,omitempty"`
	PV       []string  `json:"pv,omitempty"` // em SAN
	Lines    []APILine `json:"lines,omitempty"`
	FromBook bool      `json:"fromBook,omitempty"`
	Moves    []string  `json:"moves,omitempty"` // action "pgn": lances em UCI
	StartFEN string    `json:"startFen,omitempty"`
}

var (
	apiMu       sync.Mutex
	apiSearcher *search.Searcher
	HashMB      = 64
)

// Níveis: profundidade fixa nos fáceis, tempo por lance nos fortes.
func LevelLimits(level int) search.Limits {
	switch level {
	case 1:
		return search.Limits{Depth: 1}
	case 2:
		return search.Limits{Depth: 3}
	case 3:
		return search.Limits{Depth: 6}
	case 5:
		return search.Limits{MoveTime: 1500 * time.Millisecond}
	case 6:
		return search.Limits{MoveTime: 4 * time.Second}
	default:
		return search.Limits{MoveTime: 400 * time.Millisecond}
	}
}

func Replay(fen string, moves []string) (*chess.Position, []uint64, []string, error) {
	if fen == "" {
		fen = chess.StartFEN
	}
	pos, err := chess.ParseFEN(fen)
	if err != nil {
		return nil, nil, nil, err
	}
	hist := []uint64{pos.Hash}
	sans := make([]string, 0, len(moves))
	for _, mv := range moves {
		m, ok := pos.ParseUCIMove(mv)
		if !ok {
			return nil, nil, nil, &apiError{"lance ilegal: " + mv}
		}
		sans = append(sans, pos.SAN(m))
		next, _ := pos.Make(m)
		pos = &next
		hist = append(hist, pos.Hash)
	}
	return pos, hist, sans, nil
}

type apiError struct{ msg string }

func (e *apiError) Error() string { return e.msg }

func colorName(c int) string {
	if c == chess.White {
		return "w"
	}
	return "b"
}

func ms(v int) time.Duration { return time.Duration(v) * time.Millisecond }

// whiteScore converte a nota (de quem joga) para o ponto de vista das brancas.
func whiteScore(pos *chess.Position, score int) (cp, mate int) {
	if pos.Side == chess.Black {
		score = -score
	}
	switch {
	case score > search.Mate-2*chess.MaxPly:
		return 0, (search.Mate - score + 1) / 2
	case score < -search.Mate+2*chess.MaxPly:
		return 0, -(search.Mate + score + 1) / 2
	}
	return score, 0
}

func SANLine(pos *chess.Position, pv []chess.Move) []string {
	var out []string
	p := *pos
	for _, m := range pv {
		out = append(out, p.SAN(m))
		next, ok := p.Make(m)
		if !ok {
			break
		}
		p = next
	}
	return out
}

// importPGN lê os lances de um PGN (ignora cabeçalhos, comentários, variantes e números).
func importPGN(text string) (string, []string, error) {
	fen := chess.StartFEN
	var body strings.Builder
	depth := 0
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			if strings.HasPrefix(t, "[FEN ") {
				fen = strings.Trim(strings.TrimPrefix(t, "[FEN "), "\"]")
			}
			continue
		}
		for _, c := range t {
			switch {
			case c == '{' || c == '(':
				depth++
			case c == '}' || c == ')':
				depth--
			case depth == 0:
				body.WriteRune(c)
			}
		}
		body.WriteByte(' ')
	}
	pos, err := chess.ParseFEN(fen)
	if err != nil {
		return "", nil, err
	}
	var moves []string
	for _, tok := range strings.Fields(body.String()) {
		if i := strings.LastIndex(tok, "."); i >= 0 {
			tok = tok[i+1:]
		}
		if tok == "" || tok == "*" || tok == "1-0" || tok == "0-1" || tok == "1/2-1/2" || strings.HasPrefix(tok, "$") {
			continue
		}
		m, ok := pos.ParseSAN(tok)
		if !ok {
			return "", nil, &apiError{"lance inválido no PGN: " + tok}
		}
		moves = append(moves, m.String())
		next, _ := pos.Make(m)
		pos = &next
	}
	return fen, moves, nil
}

var apiRand = rand.New(rand.NewSource(time.Now().UnixNano()))

func HandleAPI(req APIRequest) APIResponse {
	apiMu.Lock()
	defer apiMu.Unlock()
	if apiSearcher == nil {
		apiSearcher = search.NewSearcher(HashMB)
	}
	if req.Action == "pgn" {
		fen, moves, err := importPGN(req.PGN)
		if err != nil {
			return APIResponse{Error: err.Error()}
		}
		req.FEN, req.Moves = fen, moves
	}
	if req.Action == "new" {
		apiSearcher.Clear()
		req.Moves = nil
	}

	pos, hist, sans, err := Replay(req.FEN, req.Moves)
	if err != nil {
		return APIResponse{Error: err.Error()}
	}

	resp := APIResponse{
		Moves:    req.Moves,
		StartFEN: req.FEN,
		FEN:      pos.FEN(),
		Turn:     colorName(pos.Side),
		SAN:      sans,
		Check:    pos.InCheck(),
		Status:   "playing",
		Legal:    []string{},
	}
	board := make([]byte, 64)
	for sq := 0; sq < 64; sq++ {
		if pc := pos.Board[sq]; pc >= 0 {
			board[sq] = "PNBRQKpnbrqk"[pc]
		} else {
			board[sq] = '.'
		}
	}
	resp.Board = string(board)
	legal := pos.LegalMoves()
	for _, m := range legal {
		resp.Legal = append(resp.Legal, m.String())
	}

	reps := 0
	for _, h := range hist {
		if h == pos.Hash {
			reps++
		}
	}
	switch {
	case len(legal) == 0 && resp.Check:
		resp.Status, resp.Winner = "checkmate", colorName(pos.Side^1)
	case len(legal) == 0:
		resp.Status = "stalemate"
	case pos.Halfmove >= 100:
		resp.Status = "fifty"
	case reps >= 3:
		resp.Status = "repetition"
	case pos.InsufficientMaterial():
		resp.Status = "material"
	}

	if (req.Action == "think" || req.Action == "analyze") && resp.Status == "playing" {
		lim := LevelLimits(req.Level)
		multipv := 1
		if req.Action == "analyze" {
			lim = search.Limits{MoveTime: time.Duration(max(100, req.TimeMs)) * time.Millisecond}
			multipv = max(1, min(req.MultiPV, 5))
		} else if req.WTime > 0 || req.BTime > 0 {
			// Com relógio: usa o tempo restante, mas respeita o teto do nível.
			clock := search.Limits{WTime: ms(req.WTime), BTime: ms(req.BTime), WInc: ms(req.Inc), BInc: ms(req.Inc)}
			if lim.MoveTime > 0 {
				left := clock.WTime
				if pos.Side == chess.Black {
					left = clock.BTime
				}
				if budget := left/30 + clock.WInc*3/4; budget < lim.MoveTime {
					lim = clock
				}
			}
		}
		if req.Action == "think" && req.Book {
			if m, ok := book.BookMove(pos, apiRand); ok {
				resp.Best, resp.BestSAN, resp.FromBook = m.String(), pos.SAN(m), true
				return resp
			}
		}
		apiSearcher.MultiPV = multipv
		apiSearcher.ResetStop()
		res := apiSearcher.Search(pos, hist, lim, nil)
		resp.Best = res.Best.String()
		resp.BestSAN = pos.SAN(res.Best)
		resp.Depth, resp.Nodes, resp.TimeMs = res.Depth, res.Nodes, res.Time.Milliseconds()
		resp.EvalCp, resp.MateIn = whiteScore(pos, res.Score)
		resp.PV = SANLine(pos, res.PV)
		for _, l := range res.Lines {
			cp, mate := whiteScore(pos, l.Score)
			resp.Lines = append(resp.Lines, APILine{Move: l.Move.String(), SAN: pos.SAN(l.Move), EvalCp: cp, MateIn: mate, PV: SANLine(pos, l.PV)})
		}
	}
	return resp
}
