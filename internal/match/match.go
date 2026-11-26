//go:build !(js && wasm)

package match

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gochess/internal/chess"
	"gochess/web"
)

// Registro das engines abertas, para o "recomeçar" encerrar todas.
var (
	enginesMu sync.Mutex
	engines   = map[*Engine]bool{}
)

// restartMatch encerra as engines e reexecuta o próprio processo com os
// mesmos argumentos: placar, partidas e aberturas começam do zero.
func restartMatch() {
	enginesMu.Lock()
	var wg sync.WaitGroup
	for e := range engines {
		e.cmd.Process.Kill()
		wg.Add(1)
		go func(c *exec.Cmd) { c.Wait(); wg.Done() }(e.cmd) // evita processos-zumbi
	}
	enginesMu.Unlock()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	exe, err := os.Executable()
	if err != nil {
		log.Println("recomeçar:", err)
		return
	}
	if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
		log.Println("recomeçar:", err)
	}
}

// match: árbitro de partidas entre duas engines UCI, com uma página que mostra
// os tabuleiros ao vivo e as partidas terminadas.
//
//	gochess match -e2 ~/chess-tools/sf -e2opts UCI_LimitStrength=true,UCI_Elo=2000 \
//	    -games 20 -tc 10+0.1 -concurrency 2 -book UHO.epd
//
// Para testes estatísticos longos (SPRT) use o fastchess; este modo é para
// assistir e para gauntlets.

// ================= Engine UCI externa =================

type Engine struct {
	path  string
	opts  [][2]string
	name  string
	cmd   *exec.Cmd
	in    io.WriteCloser
	lines chan string
	dead  bool
}

func StartEngine(path string, opts [][2]string) (*Engine, error) {
	e := &Engine{path: path, opts: opts}
	cmd := exec.Command(path)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	e.cmd, e.in = cmd, in
	enginesMu.Lock()
	engines[e] = true
	enginesMu.Unlock()
	e.lines = make(chan string, 4096)
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			e.lines <- sc.Text()
		}
		close(e.lines)
	}()
	e.Send("uci")
	for {
		l, err := e.Next(10 * time.Second)
		if err != nil {
			e.Kill()
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if strings.HasPrefix(l, "id name ") {
			e.name = strings.TrimPrefix(l, "id name ")
		}
		if l == "uciok" {
			break
		}
	}
	for _, o := range opts {
		e.Send("setoption name " + o[0] + " value " + o[1])
	}
	if err := e.Ready(); err != nil {
		e.Kill()
		return nil, err
	}
	return e, nil
}

func (e *Engine) Send(s string) { fmt.Fprintln(e.in, s) }

func (e *Engine) Next(d time.Duration) (string, error) {
	if d < time.Millisecond {
		d = time.Millisecond
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case l, ok := <-e.lines:
		if !ok {
			return "", errors.New("a engine terminou")
		}
		return l, nil
	case <-t.C:
		return "", errors.New("a engine não respondeu a tempo")
	}
}

func (e *Engine) Ready() error {
	e.Send("isready")
	for {
		l, err := e.Next(10 * time.Second)
		if err != nil {
			return err
		}
		if l == "readyok" {
			return nil
		}
	}
}

func (e *Engine) Kill() {
	enginesMu.Lock()
	delete(engines, e)
	enginesMu.Unlock()
	e.Send("quit")
	e.in.Close()
	done := make(chan struct{})
	go func() { e.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		e.cmd.Process.Kill()
		<-done
	}
}

// ================= Estado do match (lido pela página) =================

type liveGame struct {
	Num   int         `json:"num"`
	White string      `json:"white"`
	Black string      `json:"black"`
	FEN   string      `json:"fen"`
	Last  string      `json:"last"`
	SAN   []string    `json:"san"`
	WTime int64       `json:"wtime"`
	BTime int64       `json:"btime"`
	Turn  string      `json:"turn"`
	Eval  string      `json:"eval"`
	Depth int         `json:"depth"`
	Think [2]thinking `json:"think"` // [brancas, pretas]: última busca de cada uma
}

type gameRecord struct {
	Num    int      `json:"num"`
	White  string   `json:"white"`
	Black  string   `json:"black"`
	Result string   `json:"result"` // "1-0", "0-1", "1/2-1/2"
	Reason string   `json:"reason"`
	E1     float64  `json:"e1"` // pontos da engine 1
	FENs   []string `json:"fens,omitempty"`
	UCI    []string `json:"uci,omitempty"`
	SAN    []string `json:"san,omitempty"`
	PGN    string   `json:"pgn,omitempty"`
	Plies  int      `json:"plies"`
}

type matchState struct {
	mu      sync.Mutex
	Name1   string
	Name2   string
	TC      string
	Total   int
	W, D, L int
	Live    []*liveGame
	Done    []*gameRecord
	Started time.Time
	Over    bool
}

// eloDiff: diferença de Elo e margem de 95% a partir de W/D/L.
func eloDiff(w, d, l int) (elo, lo, hi float64) {
	n := float64(w + d + l)
	if n == 0 {
		return 0, 0, 0
	}
	s := (float64(w) + float64(d)/2) / n
	v := (float64(w)*(1-s)*(1-s) + float64(d)*(0.5-s)*(0.5-s) + float64(l)*s*s) / n
	se := math.Sqrt(v / n)
	f := func(x float64) float64 {
		x = math.Min(math.Max(x, 0.001), 0.999)
		return -400 * math.Log10(1/x-1)
	}
	return f(s), f(s - 1.96*se), f(s + 1.96*se)
}

func (m *matchState) stateJSON() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	elo, lo, hi := eloDiff(m.W, m.D, m.L)
	done := make([]gameRecord, 0, len(m.Done))
	for i := len(m.Done) - 1; i >= 0 && len(done) < 500; i-- {
		g := *m.Done[i]
		g.FENs, g.UCI, g.SAN, g.PGN = nil, nil, nil, ""
		done = append(done, g)
	}
	b, _ := json.Marshal(map[string]any{
		"name1": m.Name1, "name2": m.Name2, "tc": m.TC,
		"total": m.Total, "played": len(m.Done), "w": m.W, "d": m.D, "l": m.L,
		"elo": elo, "eloLo": lo, "eloHi": hi,
		"live": m.Live, "done": done, "over": m.Over,
		"elapsed": time.Since(m.Started).Seconds(),
	})
	return b
}

// ================= Partida =================

// thinking: o que a engine informou na última linha "info" da sua busca.
type thinking struct {
	Eval     string `json:"eval"`
	Depth    int    `json:"depth"`
	SelDepth int    `json:"seldepth"`
	Nodes    int64  `json:"nodes"`
	NPS      int64  `json:"nps"`
	PV       string `json:"pv"` // em SAN, numerada
}

// parseThinking lê depth/seldepth/nodes/nps/score/pv de uma linha "info".
func parseThinking(line string, pos *chess.Position, prev thinking) thinking {
	t := prev
	ev, d := whitePOVEval(line, pos.Side)
	if ev != "" {
		t.Eval, t.Depth = ev, d
	}
	f := strings.Fields(line)
	for i := 0; i+1 < len(f); i++ {
		switch f[i] {
		case "seldepth":
			t.SelDepth, _ = strconv.Atoi(f[i+1])
		case "nodes":
			t.Nodes, _ = strconv.ParseInt(f[i+1], 10, 64)
		case "nps":
			t.NPS, _ = strconv.ParseInt(f[i+1], 10, 64)
		case "pv":
			t.PV = pvSAN(pos, f[i+1:])
			return t
		}
	}
	return t
}

// pvSAN converte a variante principal (UCI) em SAN numerada, até 12 lances.
func pvSAN(pos *chess.Position, moves []string) string {
	p := *pos
	var b strings.Builder
	n, black := p.Fullmove, p.Side == chess.Black
	for i, mv := range moves {
		if i >= 12 {
			b.WriteString(" …")
			break
		}
		m, ok := p.ParseUCIMove(mv)
		if !ok {
			break
		}
		if !black {
			fmt.Fprintf(&b, "%d. ", n)
		} else if i == 0 {
			fmt.Fprintf(&b, "%d… ", n)
		}
		b.WriteString(p.SAN(m) + " ")
		if black {
			n++
		}
		black = !black
		p, _ = p.Make(m)
	}
	return strings.TrimSpace(b.String())
}

func whitePOVEval(line string, side int) (string, int) {
	f := strings.Fields(line)
	ev, depth := "", 0
	for i := 0; i+1 < len(f); i++ {
		switch f[i] {
		case "depth":
			depth, _ = strconv.Atoi(f[i+1])
		case "score":
			if i+2 < len(f) {
				v, _ := strconv.Atoi(f[i+2])
				if side == chess.Black {
					v = -v
				}
				if f[i+1] == "cp" {
					ev = fmt.Sprintf("%+.2f", float64(v)/100)
				} else if f[i+1] == "mate" {
					if v >= 0 {
						ev = fmt.Sprintf("M%d", v)
					} else {
						ev = fmt.Sprintf("-M%d", -v)
					}
				}
			}
		}
	}
	return ev, depth
}

type matchConfig struct {
	base, inc int64 // ms
	margin    int64
	movetime  int64 // ms por lance (0 = usar o relógio)
}

// playOne joga uma partida; e1White diz se a engine 1 joga de brancas.
func playOne(st *matchState, slot, num int, fen string, e1, e2 *Engine, e1White bool, cfg matchConfig) *gameRecord {
	pos, _ := chess.ParseFEN(fen)
	hist := []uint64{pos.Hash}
	eng := [2]*Engine{e1, e2}
	if !e1White {
		eng = [2]*Engine{e2, e1}
	}
	names := [2]string{st.Name1, st.Name2}
	if !e1White {
		names = [2]string{st.Name2, st.Name1}
	}
	rec := &gameRecord{Num: num, White: names[0], Black: names[1], FENs: []string{fen}}
	live := &liveGame{Num: num, White: names[0], Black: names[1], FEN: fen, WTime: cfg.base, BTime: cfg.base}
	st.mu.Lock()
	st.Live[slot] = live
	st.mu.Unlock()

	winner := -1 // -1 empate, senão a cor vencedora
	finish := func(w int, reason string) { winner, rec.Reason = w, reason }
	for _, e := range eng {
		e.Send("ucinewgame")
		if err := e.Ready(); err != nil {
			e.dead = true
		}
	}
	for c := 0; c < 2; c++ {
		if eng[c].dead {
			finish(1-c, "engine não respondeu")
		}
	}
	times := [2]int64{cfg.base, cfg.base}
	for rec.Reason == "" {
		legal := pos.LegalMoves()
		switch {
		case len(legal) == 0 && pos.InCheck():
			finish(1-pos.Side, "xeque-mate")
			continue
		case len(legal) == 0:
			finish(-1, "afogamento")
			continue
		case pos.Halfmove >= 100:
			finish(-1, "regra dos 50 lances")
			continue
		case pos.InsufficientMaterial():
			finish(-1, "material insuficiente")
			continue
		case chess.CountRep(hist, pos.Hash) >= 3:
			finish(-1, "repetição tripla")
			continue
		case len(rec.UCI) >= 600:
			finish(-1, "limite de lances")
			continue
		}
		side := pos.Side
		e := eng[side]
		cmd := "position fen " + fen
		if len(rec.UCI) > 0 {
			cmd += " moves " + strings.Join(rec.UCI, " ")
		}
		e.Send(cmd)
		t0 := time.Now()
		limit := time.Duration(times[side])*time.Millisecond + 5*time.Second
		if cfg.movetime > 0 {
			e.Send(fmt.Sprintf("go movetime %d", cfg.movetime))
			limit = time.Duration(cfg.movetime)*time.Millisecond + 10*time.Second
		} else {
			e.Send(fmt.Sprintf("go wtime %d btime %d winc %d binc %d", max(times[0], 1), max(times[1], 1), cfg.inc, cfg.inc))
		}
		best := ""
		for best == "" {
			l, err := e.Next(limit - time.Since(t0))
			if err != nil {
				e.dead = true
				break
			}
			if strings.HasPrefix(l, "info ") && strings.Contains(l, " score ") {
				ev, d := whitePOVEval(l, side)
				th := parseThinking(l, pos, live.Think[side])
				st.mu.Lock()
				live.Eval, live.Depth = ev, d
				live.Think[side] = th
				st.mu.Unlock()
			} else if strings.HasPrefix(l, "bestmove") {
				if f := strings.Fields(l); len(f) > 1 {
					best = f[1]
				} else {
					best = "(none)"
				}
			}
		}
		if e.dead {
			finish(1-side, "engine travou ou caiu")
			continue
		}
		if cfg.movetime > 0 {
			times[side] = cfg.movetime
		} else {
			times[side] -= time.Since(t0).Milliseconds()
		}
		if times[side] < -cfg.margin {
			finish(1-side, "tempo esgotado")
			continue
		}
		times[side] = max(times[side], 0) + cfg.inc
		m, ok := pos.ParseUCIMove(best)
		if !ok {
			finish(1-side, "lance ilegal: "+best)
			continue
		}
		rec.SAN = append(rec.SAN, pos.SAN(m))
		rec.UCI = append(rec.UCI, best)
		n, _ := pos.Make(m)
		pos = &n
		hist = append(hist, pos.Hash)
		rec.FENs = append(rec.FENs, pos.FEN())
		st.mu.Lock()
		live.FEN, live.Last = rec.FENs[len(rec.FENs)-1], best
		live.SAN = rec.SAN
		live.WTime, live.BTime = times[0], times[1]
		live.Turn = map[int]string{chess.White: "w", chess.Black: "b"}[pos.Side]
		st.mu.Unlock()
	}
	switch winner {
	case chess.White:
		rec.Result = "1-0"
	case chess.Black:
		rec.Result = "0-1"
	default:
		rec.Result = "1/2-1/2"
	}
	e1Color := chess.White
	if !e1White {
		e1Color = chess.Black
	}
	switch winner {
	case -1:
		rec.E1 = 0.5
	case e1Color:
		rec.E1 = 1
	}
	rec.Plies = len(rec.UCI)
	rec.PGN = buildPGN(rec, fen, st.TC)
	return rec
}

func buildPGN(r *gameRecord, fen, tc string) string {
	var b strings.Builder
	tag := func(k, v string) { fmt.Fprintf(&b, "[%s \"%s\"]\n", k, v) }
	tag("Event", "GoChess match")
	tag("Site", "local")
	tag("Date", time.Now().Format("2006.01.02"))
	tag("Round", strconv.Itoa(r.Num))
	tag("White", r.White)
	tag("Black", r.Black)
	tag("Result", r.Result)
	tag("FEN", fen)
	tag("SetUp", "1")
	tag("TimeControl", tc)
	tag("Termination", r.Reason)
	b.WriteString("\n")
	p, _ := chess.ParseFEN(fen)
	moveNo, black := p.Fullmove, p.Side == chess.Black
	col := 0
	write := func(s string) {
		if col+len(s) > 79 {
			b.WriteString("\n")
			col = 0
		} else if col > 0 {
			b.WriteString(" ")
			col++
		}
		b.WriteString(s)
		col += len(s)
	}
	for i, s := range r.SAN {
		if !black {
			write(fmt.Sprintf("%d. %s", moveNo, s))
		} else {
			if i == 0 {
				write(fmt.Sprintf("%d... %s", moveNo, s))
			} else {
				write(s)
			}
			moveNo++
		}
		black = !black
	}
	write(r.Result)
	b.WriteString("\n\n")
	return b.String()
}

// ================= Aberturas =================

func loadOpenings(path string, n int, r *rand.Rand) ([]string, error) {
	if path == "" {
		out := make([]string, n)
		for i := range out {
			out[i] = chess.StartFEN
		}
		return out, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Amostragem por reservatório: não carrega o livro inteiro na memória.
	res := make([]string, 0, n)
	seen := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) < 4 {
			continue
		}
		fen := strings.Join(fl[:4], " ") + " 0 1"
		seen++
		if len(res) < n {
			res = append(res, fen)
		} else if j := r.Intn(seen); j < n {
			res[j] = fen
		}
	}
	if len(res) == 0 {
		return nil, fmt.Errorf("nenhuma abertura em %s", path)
	}
	for i := range res {
		if _, err := chess.ParseFEN(res[i]); err != nil {
			res[i] = chess.StartFEN
		}
	}
	r.Shuffle(len(res), func(i, j int) { res[i], res[j] = res[j], res[i] })
	for len(res) < n {
		res = append(res, res[len(res)%len(res)])
	}
	return res, nil
}

func parseOpts(s string) [][2]string {
	var out [][2]string
	for _, kv := range strings.Split(s, ",") {
		if k, v, ok := strings.Cut(strings.TrimSpace(kv), "="); ok {
			out = append(out, [2]string{k, v})
		}
	}
	return out
}

func parseTC(s string) (base, inc int64, err error) {
	b, i, _ := strings.Cut(s, "+")
	bf, err := strconv.ParseFloat(b, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("tc inválido: %s", s)
	}
	inf := 0.0
	if i != "" {
		if inf, err = strconv.ParseFloat(i, 64); err != nil {
			return 0, 0, fmt.Errorf("tc inválido: %s", s)
		}
	}
	return int64(bf * 1000), int64(inf * 1000), nil
}

// ================= Comando =================

func Run(args []string) {
	fs := flag.NewFlagSet("match", flag.ExitOnError)
	e1Path := fs.String("e1", "./gochess", "engine 1 (a testada)")
	e1Opts := fs.String("e1opts", "OwnBook=false,Hash=64", "opções UCI da engine 1 (k=v,k=v)")
	e2Path := fs.String("e2", os.ExpandEnv("$HOME/chess-tools/sf"), "engine 2 (adversária)")
	e2Opts := fs.String("e2opts", "UCI_LimitStrength=true,UCI_Elo=2000,Hash=64", "opções UCI da engine 2")
	name1 := fs.String("name1", "", "nome exibido da engine 1 (padrão: id name)")
	name2 := fs.String("name2", "", "nome exibido da engine 2 (padrão: id name + UCI_Elo)")
	games := fs.Int("games", 20, "número de partidas (pares com cores trocadas)")
	tc := fs.String("tc", "10+0.1", "tempo base + incremento, em segundos")
	conc := fs.Int("concurrency", 2, "partidas simultâneas")
	book := fs.String("book", os.ExpandEnv("$HOME/chess-tools/UHO_Lichess_4852_v1.epd"), "aberturas EPD (vazio = posição inicial)")
	pgnOut := fs.String("pgn", "testes/out/match.pgn", "PGN de saída (acrescenta)")
	addr := fs.String("addr", "localhost:8090", "endereço da página ao vivo")
	keep := fs.Bool("keep", true, "manter a página no ar depois do fim")
	margin := fs.Int("margin", 100, "tolerância de tempo em ms")
	movetime := fs.Float64("movetime", 0, "segundos fixos por lance (substitui -tc)")
	fs.Parse(args)

	// Depois de um "recomeçar", recolhe engines antigas que viraram zumbi.
	for {
		var ws syscall.WaitStatus
		if pid, _ := syscall.Wait4(-1, &ws, syscall.WNOHANG, nil); pid <= 0 {
			break
		}
	}
	base, inc, err := parseTC(*tc)
	if err != nil {
		log.Fatal(err)
	}
	cfg := matchConfig{base: base, inc: inc, margin: int64(*margin), movetime: int64(*movetime * 1000)}
	if cfg.movetime > 0 {
		cfg.base = cfg.movetime
		*tc = fmt.Sprintf("%gs/lance", *movetime)
	}
	o1, o2 := parseOpts(*e1Opts), parseOpts(*e2Opts)

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	openings, err := loadOpenings(*book, (*games+1)/2, r)
	if err != nil {
		log.Fatal(err)
	}

	// Descobre os nomes.
	probe1, err := StartEngine(*e1Path, o1)
	if err != nil {
		log.Fatal(err)
	}
	probe2, err := StartEngine(*e2Path, o2)
	if err != nil {
		log.Fatal(err)
	}
	st := &matchState{Name1: probe1.name, Name2: probe2.name, TC: *tc, Total: *games,
		Live: make([]*liveGame, *conc), Started: time.Now()}
	probe1.Kill()
	probe2.Kill()
	if *name1 != "" {
		st.Name1 = *name1
	}
	if *name2 != "" {
		st.Name2 = *name2
	} else {
		for _, o := range o2 {
			if o[0] == "UCI_Elo" {
				st.Name2 += " (Elo " + o[1] + ")"
			}
		}
	}

	if err := os.MkdirAll(dirOf(*pgnOut), 0o755); err != nil {
		log.Fatal(err)
	}
	pgnFile, err := os.OpenFile(*pgnOut, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatal(err)
	}
	defer pgnFile.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(web.MatchPage)
	})
	mux.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(st.stateJSON())
	})
	mux.HandleFunc("/restart", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "use POST", http.StatusMethodNotAllowed)
			return
		}
		w.Write([]byte("ok"))
		fmt.Println("Recomeçando do zero (pedido pela página)")
		go func() {
			time.Sleep(200 * time.Millisecond) // deixa a resposta sair
			restartMatch()
		}()
	})
	mux.HandleFunc("/game", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("n"))
		st.mu.Lock()
		var g *gameRecord
		for _, x := range st.Done {
			if x.Num == n {
				g = x
			}
		}
		b, _ := json.Marshal(g)
		st.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	})
	go func() {
		if err := http.ListenAndServe(*addr, mux); err != nil {
			log.Fatal(err)
		}
	}()
	fmt.Printf("%s × %s, %d partidas, %s, %d simultâneas\n", st.Name1, st.Name2, *games, *tc, *conc)
	fmt.Printf("Assista em http://%s\n", *addr)

	jobs := make(chan int)
	go func() {
		for i := 1; i <= *games; i++ {
			jobs <- i
		}
		close(jobs)
	}()
	var wg sync.WaitGroup
	for w := 0; w < *conc; w++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			var e1, e2 *Engine
			defer func() {
				if e1 != nil {
					e1.Kill()
				}
				if e2 != nil {
					e2.Kill()
				}
			}()
			for num := range jobs {
				var err error
				if e1 == nil || e1.dead {
					if e1 != nil {
						e1.Kill()
					}
					if e1, err = StartEngine(*e1Path, o1); err != nil {
						log.Fatal(err)
					}
				}
				if e2 == nil || e2.dead {
					if e2 != nil {
						e2.Kill()
					}
					if e2, err = StartEngine(*e2Path, o2); err != nil {
						log.Fatal(err)
					}
				}
				rec := playOne(st, slot, num, openings[(num-1)/2], e1, e2, num%2 == 1, cfg)
				st.mu.Lock()
				st.Done = append(st.Done, rec)
				switch rec.E1 {
				case 1:
					st.W++
				case 0.5:
					st.D++
				default:
					st.L++
				}
				pgnFile.WriteString(rec.PGN)
				elo, lo, hi := eloDiff(st.W, st.D, st.L)
				fmt.Printf("#%d %s - %s: %s (%s) | %s: +%d =%d -%d | Elo %+.0f [%+.0f, %+.0f]\n",
					num, rec.White, rec.Black, rec.Result, rec.Reason, st.Name1, st.W, st.D, st.L, elo, lo, hi)
				st.mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	st.mu.Lock()
	st.Over = true
	st.Live = make([]*liveGame, len(st.Live))
	elo, lo, hi := eloDiff(st.W, st.D, st.L)
	fmt.Printf("FIM: %s +%d =%d -%d contra %s | Elo %+.0f [%+.0f, %+.0f]\n", st.Name1, st.W, st.D, st.L, st.Name2, elo, lo, hi)
	st.mu.Unlock()
	if *keep {
		fmt.Printf("A página continua em http://%s (Ctrl-C para sair)\n", *addr)
		select {}
	}
}

func dirOf(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return "."
}
