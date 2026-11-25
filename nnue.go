package main

import (
	"encoding/binary"
	"errors"
)

// NNUE: rede 768 → H (x2 perspectivas) → 1.
//
// Entradas: (cor relativa, tipo de peça, casa) do ponto de vista de cada lado
// (as pretas veem o tabuleiro espelhado). A primeira camada é um
// "acumulador": um lance só muda 2 a 4 entradas, então o acumulador do filho
// é o do pai mais/menos algumas colunas, em vez de recalcular tudo.
// Ativação SCReLU (clamp(x,0,QA)²), pesos quantizados em int16.

const (
	nnInputs = 768
	nnHidden = 256
	nnQA     = 255
	nnQB     = 64
	nnScale  = 400
)

type Net struct {
	FW [nnInputs][nnHidden]int16
	FB [nnHidden]int16
	OW [2 * nnHidden]int16
	OB int32

	simd bool // AVX2 disponível e |OW| <= 128 (ver screluDotAVX2)
}

// prepare decide se a inferência pode usar os kernels AVX2.
func (n *Net) prepare() {
	n.simd = hasAVX2
	for _, w := range n.OW {
		if w > 128 || w < -128 {
			n.simd = false
		}
	}
}

var currentNet *Net

const netMagic = "GCNN1"

// LoadNet lê o formato: "GCNN1", H (uint32), FW, FB, OW (int16 LE), OB (int32 LE).
func LoadNet(b []byte) (*Net, error) {
	if len(b) < 9 || string(b[:5]) != netMagic {
		return nil, errors.New("arquivo de rede inválido")
	}
	if binary.LittleEndian.Uint32(b[5:9]) != nnHidden {
		return nil, errors.New("tamanho da camada oculta não confere")
	}
	need := 9 + 2*(nnInputs*nnHidden+nnHidden+2*nnHidden) + 4
	if len(b) != need {
		return nil, errors.New("tamanho do arquivo de rede não confere")
	}
	n := &Net{}
	off := 9
	rd := func() int16 { v := int16(binary.LittleEndian.Uint16(b[off:])); off += 2; return v }
	for i := 0; i < nnInputs; i++ {
		for j := 0; j < nnHidden; j++ {
			n.FW[i][j] = rd()
		}
	}
	for j := 0; j < nnHidden; j++ {
		n.FB[j] = rd()
	}
	for j := 0; j < 2*nnHidden; j++ {
		n.OW[j] = rd()
	}
	n.OB = int32(binary.LittleEndian.Uint32(b[off:]))
	n.prepare()
	return n, nil
}

func (n *Net) Marshal() []byte {
	b := make([]byte, 0, 9+2*(nnInputs*nnHidden+3*nnHidden)+4)
	b = append(b, netMagic...)
	b = binary.LittleEndian.AppendUint32(b, nnHidden)
	for i := 0; i < nnInputs; i++ {
		for j := 0; j < nnHidden; j++ {
			b = binary.LittleEndian.AppendUint16(b, uint16(n.FW[i][j]))
		}
	}
	for j := 0; j < nnHidden; j++ {
		b = binary.LittleEndian.AppendUint16(b, uint16(n.FB[j]))
	}
	for j := 0; j < 2*nnHidden; j++ {
		b = binary.LittleEndian.AppendUint16(b, uint16(n.OW[j]))
	}
	return binary.LittleEndian.AppendUint32(b, uint32(n.OB))
}

// nnFeature: índice da entrada para a perspectiva `view`.
func nnFeature(view, color, pt, sq int) int {
	rel := 0
	if color != view {
		rel = 1
	}
	if view == Black {
		sq ^= 56
	}
	return (rel*6+pt)*64 + sq
}

type accumulator [2][nnHidden]int16

func (a *accumulator) refresh(n *Net, p *Position) {
	for v := 0; v < 2; v++ {
		a[v] = n.FB
	}
	for sq := 0; sq < 64; sq++ {
		if pc := p.Board[sq]; pc >= 0 {
			a.add(n, int(pc)/6, int(pc)%6, sq)
		}
	}
}

func (a *accumulator) add(n *Net, color, pt, sq int) {
	for v := 0; v < 2; v++ {
		w := &n.FW[nnFeature(v, color, pt, sq)]
		for j := range a[v] {
			a[v][j] += w[j]
		}
	}
}

func (a *accumulator) sub(n *Net, color, pt, sq int) {
	for v := 0; v < 2; v++ {
		w := &n.FW[nnFeature(v, color, pt, sq)]
		for j := range a[v] {
			a[v][j] -= w[j]
		}
	}
}

func screlu(x int16) int32 {
	v := int32(x)
	if v < 0 {
		return 0
	}
	if v > nnQA {
		v = nnQA
	}
	return v * v
}

func (n *Net) output(a *accumulator, side int) int {
	var sum int64
	us, them := &a[side], &a[side^1]
	if n.simd {
		sum = screluDot2AVX2(&us[0], &them[0], &n.OW[0], nnHidden)
	} else {
		for j := 0; j < nnHidden; j++ {
			sum += int64(screlu(us[j])) * int64(n.OW[j])
			sum += int64(screlu(them[j])) * int64(n.OW[nnHidden+j])
		}
	}
	v := (sum/nnQA + int64(n.OB)) * nnScale / (nnQA * nnQB)
	return int(v)
}

// EvaluateNNUE calcula do zero (usado em testes e na API).
func EvaluateNNUE(n *Net, p *Position) int {
	var a accumulator
	a.refresh(n, p)
	return n.output(&a, p.Side)
}

// nnueStack guarda um acumulador por ply e as mudanças de cada lance,
// aplicadas só quando a avaliação é realmente pedida.
type delta struct {
	color, pt, sq int
	add           bool
}

type nnEntry struct {
	acc      accumulator
	computed bool
	deltas   [4]delta
	n        int
}

type nnueStack struct {
	on      bool
	net     *Net
	entries [MaxPly + 2]nnEntry
}

func newNNUEStack() *nnueStack { return &nnueStack{} }

func (st *nnueStack) reset(p *Position) {
	st.on = false
}

func (st *nnueStack) enable(n *Net, p *Position) {
	st.on, st.net = true, n
	st.entries[0].acc.refresh(n, p)
	st.entries[0].computed = true
}

// push registra o lance m feito em p (ply) para o filho (ply+1).
func (st *nnueStack) push(ply int, p *Position, m Move) {
	if !st.on || ply+1 >= len(st.entries) {
		return
	}
	e := &st.entries[ply+1]
	e.computed, e.n = false, 0
	addD := func(c, pt, sq int, add bool) {
		e.deltas[e.n] = delta{c, pt, sq, add}
		e.n++
	}
	us := p.Side
	from, to := m.From(), m.To()
	pt := int(p.Board[from]) % 6
	addD(us, pt, from, false)
	if m&FlagEP != 0 {
		cap := to - 8
		if us == Black {
			cap = to + 8
		}
		addD(us^1, Pawn, cap, false)
	} else if pc := p.Board[to]; pc >= 0 {
		addD(int(pc)/6, int(pc)%6, to, false)
	}
	if pr := m.Promo(); pr != 0 {
		pt = pr
	}
	addD(us, pt, to, true)
	if m&FlagCastle != 0 {
		var rf, rt int
		switch to {
		case 6:
			rf, rt = 7, 5
		case 2:
			rf, rt = 0, 3
		case 62:
			rf, rt = 63, 61
		case 58:
			rf, rt = 56, 59
		}
		addD(us, Rook, rf, false)
		addD(us, Rook, rt, true)
	}
}

func (st *nnueStack) pushNull(ply int) {
	if !st.on || ply+1 >= len(st.entries) {
		return
	}
	e := &st.entries[ply+1]
	e.computed, e.n = false, 0
}

func (st *nnueStack) evaluate(p *Position, ply int) int {
	if !st.on {
		return Evaluate(p)
	}
	if ply >= len(st.entries) {
		var a accumulator
		a.refresh(st.net, p)
		return st.net.output(&a, p.Side)
	}
	i := ply
	for !st.entries[i].computed {
		i--
	}
	for j := i + 1; j <= ply; j++ {
		e := &st.entries[j]
		st.apply(&e.acc, &st.entries[j-1].acc, e.deltas[:e.n])
		e.computed = true
	}
	return st.net.output(&st.entries[ply].acc, p.Side)
}

// apply: dst = src com as mudanças de um lance. Os casos comuns (lance
// quieto, captura, roque) passam pelos kernels fundidos: uma única passada
// sobre o acumulador em vez de copiar e depois somar/subtrair coluna a coluna.
func (st *nnueStack) apply(dst, src *accumulator, ds []delta) {
	n := st.net
	na, ns := 0, 0
	for _, d := range ds {
		if d.add {
			na++
		} else {
			ns++
		}
	}
	// Formatos possíveis: 1+1 (quieto, promoção), 1+2 (captura, en passant,
	// promoção com captura), 2+2 (roque).
	fast := n.simd && ((na == 1 && (ns == 1 || ns == 2)) || (na == 2 && ns == 2))
	if !fast {
		*dst = *src
		for _, d := range ds {
			if d.add {
				dst.add(n, d.color, d.pt, d.sq)
			} else {
				dst.sub(n, d.color, d.pt, d.sq)
			}
		}
		return
	}
	for v := 0; v < 2; v++ {
		var a, s [2]*int16
		ia, is := 0, 0
		for _, d := range ds {
			w := &n.FW[nnFeature(v, d.color, d.pt, d.sq)][0]
			if d.add {
				a[ia] = w
				ia++
			} else {
				s[is] = w
				is++
			}
		}
		switch {
		case ia == 1 && is == 1:
			addSubAVX2(&dst[v][0], &src[v][0], a[0], s[0], nnHidden)
		case ia == 1:
			addSubSubAVX2(&dst[v][0], &src[v][0], a[0], s[0], s[1], nnHidden)
		default:
			addSubAVX2(&dst[v][0], &src[v][0], a[0], s[0], nnHidden)
			addSubAVX2(&dst[v][0], &dst[v][0], a[1], s[1], nnHidden)
		}
	}
}
