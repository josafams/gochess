//go:build !(js && wasm)

package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Treino da NNUE em Go puro (CPU). A rede em float é treinada com Adam para
// prever sigmoid(nota/400) — mistura da nota da busca com o resultado — e
// depois quantizada para o formato usado em nnue.go.
//
//	gochess train -in dados.txt -epochs 20 -out gochess.nnue

// trainData guarda as posições no formato compacto (32 bytes cada).
type trainData struct {
	recs   []byte
	lambda float64
}

func (d *trainData) n() int           { return len(d.recs) / packedSize }
func (d *trainData) rec(i int) []byte { return d.recs[i*packedSize : (i+1)*packedSize] }
func (d *trainData) slice(lo, hi int) *trainData {
	return &trainData{recs: d.recs[lo*packedSize : hi*packedSize], lambda: d.lambda}
}

func (d *trainData) shuffle(r *rand.Rand) {
	n := d.n()
	var tmp [packedSize]byte
	for i := n - 1; i > 0; i-- {
		j := r.Intn(i + 1)
		a, b := d.rec(i), d.rec(j)
		copy(tmp[:], a)
		copy(a, b)
		copy(b, tmp[:])
	}
}

// loadTrainData lê uma lista de arquivos separados por vírgula: .bin
// (formato compacto) ou texto do datagen ("FEN | nota | resultado").
func loadTrainData(paths string, lambda float64) (*trainData, error) {
	list := strings.Split(paths, ",")
	total := int64(0)
	for _, p := range list {
		st, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(p, ".bin") {
			total += st.Size()
		} else {
			total += st.Size() / 50 * packedSize // estimativa generosa
		}
	}
	d := &trainData{recs: make([]byte, 0, total), lambda: lambda}
	for _, p := range list {
		before := d.n()
		if strings.HasSuffix(p, ".bin") {
			f, err := os.Open(p)
			if err != nil {
				return nil, err
			}
			st, _ := f.Stat()
			sz := int(st.Size()) / packedSize * packedSize
			off := len(d.recs)
			d.recs = d.recs[:off+sz]
			if _, err := io.ReadFull(f, d.recs[off:]); err != nil {
				f.Close()
				return nil, err
			}
			f.Close()
		} else {
			f, err := os.Open(p)
			if err != nil {
				return nil, err
			}
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				if rec, ok := readTextLine(sc.Text()); ok {
					d.recs = append(d.recs, rec[:]...)
				}
			}
			f.Close()
		}
		fmt.Printf("  %s: %d posições\n", p, d.n()-before)
	}
	return d, nil
}

type floatNet struct {
	FW []float32 // nnInputs * nnHidden
	FB []float32
	OW []float32 // 2*nnHidden
	OB float32
}

func newFloatNet(r *rand.Rand) *floatNet {
	n := &floatNet{FW: make([]float32, nnInputs*nnHidden), FB: make([]float32, nnHidden), OW: make([]float32, 2*nnHidden)}
	for i := range n.FW {
		n.FW[i] = float32(r.NormFloat64() * 0.1)
	}
	for i := range n.FB {
		n.FB[i] = 0.1
	}
	for i := range n.OW {
		n.OW[i] = float32(r.NormFloat64() * 0.1)
	}
	return n
}

func clamp01(x float32) float32 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

type grads struct {
	FW, FB, OW []float32
	OB         float32
	loss       float64
}

func newGrads() *grads {
	return &grads{FW: make([]float32, nnInputs*nnHidden), FB: make([]float32, nnHidden), OW: make([]float32, 2*nnHidden)}
}

// step: propaga uma amostra e acumula o gradiente.
func (n *floatNet) step(rec []byte, lambda float64, g *grads, accU, accT []float32) {
	H := nnHidden
	var us, them [32]int16
	cnt, _ := packedFeatures(rec, &us, &them)
	target := packedTarget(rec, lambda)
	copy(accU, n.FB)
	copy(accT, n.FB)
	for i := 0; i < cnt; i++ {
		wu := n.FW[int(us[i])*H : int(us[i])*H+H]
		wt := n.FW[int(them[i])*H : int(them[i])*H+H]
		for j := 0; j < H; j++ {
			accU[j] += wu[j]
			accT[j] += wt[j]
		}
	}
	out := n.OB
	for j := 0; j < H; j++ {
		a, b := clamp01(accU[j]), clamp01(accT[j])
		out += a*a*n.OW[j] + b*b*n.OW[H+j]
	}
	pred := float32(sigmoid(float64(out)))
	diff := pred - target
	g.loss += float64(diff * diff)
	d := 2 * diff * pred * (1 - pred) // d(loss)/d(out)
	g.OB += d
	// Reaproveita accU/accT como gradiente em relação ao acumulador.
	for j := 0; j < H; j++ {
		a, b := clamp01(accU[j]), clamp01(accT[j])
		g.OW[j] += d * a * a
		g.OW[H+j] += d * b * b
		var ga, gb float32
		if accU[j] > 0 && accU[j] < 1 {
			ga = d * n.OW[j] * 2 * a
		}
		if accT[j] > 0 && accT[j] < 1 {
			gb = d * n.OW[H+j] * 2 * b
		}
		accU[j], accT[j] = ga, gb
		g.FB[j] += ga + gb
	}
	for i := 0; i < cnt; i++ {
		gu := g.FW[int(us[i])*H : int(us[i])*H+H]
		gt := g.FW[int(them[i])*H : int(them[i])*H+H]
		for j := 0; j < H; j++ {
			gu[j] += accU[j]
			gt[j] += accT[j]
		}
	}
}

type adam struct{ m, v []float32 }

func newAdam(n int) *adam { return &adam{make([]float32, n), make([]float32, n)} }

func (a *adam) update(w, g []float32, lr float64, t int, scale float32, clip float32) {
	const b1, b2, eps = 0.9, 0.999, 1e-8
	c1 := float32(1 - math.Pow(b1, float64(t)))
	c2 := float32(1 - math.Pow(b2, float64(t)))
	for i := range w {
		gi := g[i] * scale
		a.m[i] = b1*a.m[i] + (1-b1)*gi
		a.v[i] = b2*a.v[i] + (1-b2)*gi*gi
		w[i] -= float32(lr) * (a.m[i] / c1) / (float32(math.Sqrt(float64(a.v[i]/c2))) + eps)
		if w[i] > clip {
			w[i] = clip
		} else if w[i] < -clip {
			w[i] = -clip
		}
	}
}

func (n *floatNet) quantize() *Net {
	q := &Net{}
	r := func(x float64) float64 { return math.Round(x) }
	for i := 0; i < nnInputs; i++ {
		for j := 0; j < nnHidden; j++ {
			q.FW[i][j] = int16(r(float64(n.FW[i*nnHidden+j]) * nnQA))
		}
	}
	for j := 0; j < nnHidden; j++ {
		q.FB[j] = int16(r(float64(n.FB[j]) * nnQA))
	}
	for j := 0; j < 2*nnHidden; j++ {
		// |OW| <= 128 permite o kernel AVX2 (v*w cabe em int16).
		q.OW[j] = int16(max(-128, min(128, r(float64(n.OW[j])*nnQB))))
	}
	q.OB = int32(r(float64(n.OB) * nnQA * nnQB))
	q.prepare()
	return q
}

func runTrain(args []string) {
	fs := flag.NewFlagSet("train", flag.ExitOnError)
	in := fs.String("in", "dados.txt", "arquivos de treino (.bin ou texto), separados por vírgula")
	epochs := fs.Int("epochs", 20, "épocas")
	batch := fs.Int("batch", 16384, "tamanho do lote")
	nVal := fs.Int("val", 1_000_000, "posições separadas para validação")
	lr := fs.Float64("lr", 0.001, "taxa de aprendizado inicial")
	lambda := fs.Float64("lambda", 0.5, "peso do resultado da partida")
	out := fs.String("out", "gochess.nnue", "arquivo da rede")
	fs.Parse(args)

	start := time.Now()
	data, err := loadTrainData(*in, *lambda)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	r := rand.New(rand.NewSource(42))
	data.shuffle(r)
	nv := min(*nVal, data.n()/10)
	val, train := data.slice(0, nv), data.slice(nv, data.n())
	fmt.Printf("%d amostras de treino, %d de validação, rede 768→%d (%v)\n", train.n(), val.n(), nnHidden, time.Since(start).Round(time.Millisecond))

	net := newFloatNet(r)
	optFW, optFB, optOW := newAdam(len(net.FW)), newAdam(len(net.FB)), newAdam(len(net.OW))
	optOB := newAdam(1)
	workers := runtime.NumCPU()
	gs := make([]*grads, workers)
	for i := range gs {
		gs[i] = newGrads()
	}
	t := 0
	bestVal := math.Inf(1)
	var bestNet *Net

	evalLoss := func(set *trainData) float64 {
		var wg sync.WaitGroup
		losses := make([]float64, workers)
		chunk := (set.n() + workers - 1) / workers
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				g := newGrads()
				accU, accT := make([]float32, nnHidden), make([]float32, nnHidden)
				for i := w * chunk; i < min((w+1)*chunk, set.n()); i++ {
					net.step(set.rec(i), set.lambda, g, accU, accT)
				}
				losses[w] = g.loss
			}(w)
		}
		wg.Wait()
		t := 0.0
		for _, l := range losses {
			t += l
		}
		return t / float64(set.n())
	}

	for ep := 1; ep <= *epochs; ep++ {
		rate := *lr
		if ep > *epochs*2/3 {
			rate *= 0.1 // reduz no terço final
		}
		train.shuffle(r)
		var trainLoss float64
		epStart, nextReport := time.Now(), train.n()/20
		for lo := 0; lo < train.n(); lo += *batch {
			hi := min(lo+*batch, train.n())
			if lo >= nextReport {
				nextReport += train.n() / 20
				el := time.Since(epStart).Seconds()
				fmt.Printf("  época %d: %d%% (%.0f mil pos/s, perda %.6f)\n", ep, lo*100/train.n(), float64(lo)/el/1000, trainLoss/float64(lo))
			}
			var wg sync.WaitGroup
			chunk := (hi - lo + workers - 1) / workers
			for w := 0; w < workers; w++ {
				g := gs[w]
				for i := range g.FW {
					g.FW[i] = 0
				}
				for i := range g.FB {
					g.FB[i] = 0
				}
				for i := range g.OW {
					g.OW[i] = 0
				}
				g.OB, g.loss = 0, 0
				wg.Add(1)
				go func(w int) {
					defer wg.Done()
					accU, accT := make([]float32, nnHidden), make([]float32, nnHidden)
					a, b := lo+w*chunk, min(lo+(w+1)*chunk, hi)
					for i := a; i < b; i++ {
						net.step(train.rec(i), train.lambda, gs[w], accU, accT)
					}
				}(w)
			}
			wg.Wait()
			for w := 1; w < workers; w++ {
				for i := range gs[0].FW {
					gs[0].FW[i] += gs[w].FW[i]
				}
				for i := range gs[0].FB {
					gs[0].FB[i] += gs[w].FB[i]
				}
				for i := range gs[0].OW {
					gs[0].OW[i] += gs[w].OW[i]
				}
				gs[0].OB += gs[w].OB
				gs[0].loss += gs[w].loss
			}
			trainLoss += gs[0].loss
			t++
			scale := float32(1) / float32(hi-lo)
			optFW.update(net.FW, gs[0].FW, rate, t, scale, 1.98)
			optFB.update(net.FB, gs[0].FB, rate, t, scale, 1.98)
			optOW.update(net.OW, gs[0].OW, rate, t, scale, 1.98)
			ob := []float32{net.OB}
			optOB.update(ob, []float32{gs[0].OB}, rate, t, scale, 100)
			net.OB = ob[0]
		}
		vl := evalLoss(val)
		fmt.Printf("época %d: treino %.6f, validação %.6f (%v)\n", ep, trainLoss/float64(train.n()), vl, time.Since(start).Round(time.Second))
		if vl < bestVal {
			bestVal, bestNet = vl, net.quantize()
			// Grava a cada melhora: um treino interrompido não perde tudo.
			if err := os.WriteFile(*out, bestNet.Marshal(), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
	}
	if err := os.WriteFile(*out, bestNet.Marshal(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("rede gravada em %s (validação %.6f)\n", *out, bestVal)
}
