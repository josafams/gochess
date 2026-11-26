//go:build !(js && wasm)

package main

import (
	"bufio"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Texel tuning: ajusta todos os pesos da avaliação para que
// sigmoid(avaliação) preveja o resultado das partidas (misturado com a nota
// da busca, que tem menos ruído). Como a avaliação é linear nos pesos, cada
// posição vira uma lista esparsa de coeficientes (o "trace") e o gradiente
// sai de graça.
//
//	gochess tune -in dados.txt -epochs 600 -out tuned_params.go

type tuneEntry struct {
	idx    []int32
	coef   []float32
	phase  float32 // fração de meio-jogo (0..1)
	scale  float32 // fator do final (0..1)
	target float32
}

func loadTuneData(path string, lambda float64) ([]tuneEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []tuneEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.Split(sc.Text(), "|")
		if len(parts) != 3 {
			continue
		}
		p, err := ParseFEN(strings.TrimSpace(parts[0]))
		if err != nil {
			continue
		}
		if _, special := evalEndgame(p); special {
			continue
		}
		score, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		result, _ := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
		tr := &Trace{coef: map[int]int{}}
		evaluateTrace(p, tr)
		e := tuneEntry{
			phase:  float32(tr.phase) / 24,
			scale:  float32(tr.scale) / 128,
			target: float32(lambda*result + (1-lambda)*sigmoid(float64(score)/400)),
		}
		keys := make([]int, 0, len(tr.coef))
		for k, v := range tr.coef {
			if v != 0 {
				keys = append(keys, k)
			}
		}
		sort.Ints(keys)
		for _, k := range keys {
			e.idx = append(e.idx, int32(k))
			e.coef = append(e.coef, float32(tr.coef[k]))
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

func sigmoid(x float64) float64 { return 1 / (1 + math.Exp(-x)) }

// linearEval: avaliação (pov brancas) a partir do trace e dos pesos em float.
func linearEval(e *tuneEntry, mg, eg []float64) float64 {
	var m, g float64
	for i, k := range e.idx {
		c := float64(e.coef[i])
		m += c * mg[k]
		g += c * eg[k]
	}
	return m*float64(e.phase) + g*float64(e.scale)*(1-float64(e.phase))
}

func tuneLoss(data []tuneEntry, mg, eg []float64, K float64) float64 {
	workers := runtime.NumCPU()
	sums := make([]float64, workers)
	var wg sync.WaitGroup
	chunk := (len(data) + workers - 1) / workers
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			lo, hi := w*chunk, min((w+1)*chunk, len(data))
			for i := lo; i < hi; i++ {
				d := float64(data[i].target) - sigmoid(K*linearEval(&data[i], mg, eg)/400)
				sums[w] += d * d
			}
		}(w)
	}
	wg.Wait()
	t := 0.0
	for _, s := range sums {
		t += s
	}
	return t / float64(len(data))
}

func runTune(args []string) {
	fs := flag.NewFlagSet("tune", flag.ExitOnError)
	in := fs.String("in", "dados.txt", "dados do datagen")
	epochs := fs.Int("epochs", 600, "iterações")
	lr := fs.Float64("lr", 1.0, "taxa de aprendizado (Adam)")
	lambda := fs.Float64("lambda", 0.5, "peso do resultado da partida (resto: nota da busca)")
	out := fs.String("out", "tuned_params.go", "arquivo Go gerado")
	fs.Parse(args)

	start := time.Now()
	data, err := loadTuneData(*in, *lambda)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%d posições carregadas em %v\n", len(data), time.Since(start).Round(time.Millisecond))

	n := len(Params)
	mg, eg := make([]float64, n), make([]float64, n)
	for i, p := range Params {
		mg[i], eg[i] = float64(p.MG), float64(p.EG)
	}

	// 1. Constante K que melhor converte centipeões em probabilidade.
	K, best := 1.0, math.Inf(1)
	for k := 0.5; k <= 2.0; k += 0.05 {
		if l := tuneLoss(data, mg, eg, k); l < best {
			K, best = k, l
		}
	}
	fmt.Printf("K = %.2f, erro inicial = %.6f\n", K, best)

	// 2. Adam com gradiente sobre todas as posições.
	mMG, vMG := make([]float64, n), make([]float64, n)
	mEG, vEG := make([]float64, n), make([]float64, n)
	const b1, b2, eps = 0.9, 0.999, 1e-8
	workers := runtime.NumCPU()
	for ep := 1; ep <= *epochs; ep++ {
		gMG, gEG := make([][]float64, workers), make([][]float64, workers)
		var wg sync.WaitGroup
		chunk := (len(data) + workers - 1) / workers
		for w := 0; w < workers; w++ {
			gMG[w], gEG[w] = make([]float64, n), make([]float64, n)
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				lo, hi := w*chunk, min((w+1)*chunk, len(data))
				for i := lo; i < hi; i++ {
					e := &data[i]
					s := sigmoid(K * linearEval(e, mg, eg) / 400)
					// d(erro)/d(eval)
					d := -2 * (float64(e.target) - s) * s * (1 - s) * K / 400
					fm := d * float64(e.phase)
					fe := d * float64(e.scale) * (1 - float64(e.phase))
					for j, k := range e.idx {
						c := float64(e.coef[j])
						gMG[w][k] += fm * c
						gEG[w][k] += fe * c
					}
				}
			}(w)
		}
		wg.Wait()
		for k := 0; k < n; k++ {
			var gm, ge float64
			for w := 0; w < workers; w++ {
				gm += gMG[w][k]
				ge += gEG[w][k]
			}
			gm /= float64(len(data))
			ge /= float64(len(data))
			mMG[k] = b1*mMG[k] + (1-b1)*gm
			vMG[k] = b2*vMG[k] + (1-b2)*gm*gm
			mEG[k] = b1*mEG[k] + (1-b1)*ge
			vEG[k] = b2*vEG[k] + (1-b2)*ge*ge
			c1, c2 := 1-math.Pow(b1, float64(ep)), 1-math.Pow(b2, float64(ep))
			mg[k] -= *lr * (mMG[k] / c1) / (math.Sqrt(vMG[k]/c2) + eps)
			eg[k] -= *lr * (mEG[k] / c1) / (math.Sqrt(vEG[k]/c2) + eps)
		}
		if ep%50 == 0 || ep == *epochs {
			fmt.Printf("época %d: erro = %.6f (%v)\n", ep, tuneLoss(data, mg, eg, K), time.Since(start).Round(time.Second))
		}
	}

	// 3. Gera o arquivo Go com os pesos.
	var b strings.Builder
	b.WriteString("// Código gerado por \"gochess tune\". Não edite à mão.\n\npackage main\n\n")
	fmt.Fprintf(&b, "// %d posições, K=%.2f, erro final %.6f\n", len(data), K, tuneLoss(data, mg, eg, K))
	b.WriteString("var tunedParams = []Pair{\n")
	for k := 0; k < n; k++ {
		fmt.Fprintf(&b, "\t{%d, %d}, // %s\n", int(math.Round(mg[k])), int(math.Round(eg[k])), paramNames[k])
	}
	b.WriteString("}\n\nfunc loadTunedParams() {\n\tif len(tunedParams) == len(Params) {\n\t\tcopy(Params, tunedParams)\n\t}\n}\n")
	if err := os.WriteFile(*out, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("pesos gravados em", *out)
}
