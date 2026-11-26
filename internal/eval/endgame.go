package eval

import (
	"sync"

	"gochess/internal/chess"
)

// Conhecimento de finais: tabela exata de rei e peão contra rei (KPK),
// gerada por análise retrógrada; mate com bispo e cavalo (KBNK); "mop-up"
// contra rei sozinho; e empates por material.

const (
	kpkUnknown byte = iota
	kpkInvalid
	kpkDraw
	kpkWin
)

var (
	kpkTable []byte
	kpkOnce  sync.Once
)

func kpkIdx(stm, wk, bk, wp int) int { return ((stm*64+wk)*64+bk)*64 + wp }

// buildKPK: brancas têm rei e peão. Itera até nada mudar:
// com brancas a jogar, é vitória se algum lance leva a vitória;
// com pretas a jogar, é empate se algum lance leva a empate.
func buildKPK() {
	t := make([]byte, 2*64*64*64)
	for stm := 0; stm < 2; stm++ {
		for wk := 0; wk < 64; wk++ {
			for bk := 0; bk < 64; bk++ {
				for wp := 0; wp < 64; wp++ {
					t[kpkIdx(stm, wk, bk, wp)] = kpkInitial(stm, wk, bk, wp)
				}
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for stm := 0; stm < 2; stm++ {
			for wk := 0; wk < 64; wk++ {
				for bk := 0; bk < 64; bk++ {
					for wp := 8; wp < 56; wp++ {
						i := kpkIdx(stm, wk, bk, wp)
						if t[i] != kpkUnknown {
							continue
						}
						if r := kpkClassify(t, stm, wk, bk, wp); r != kpkUnknown {
							t[i] = r
							changed = true
						}
					}
				}
			}
		}
	}
	for i := range t {
		if t[i] == kpkUnknown {
			t[i] = kpkDraw
		}
	}
	kpkTable = t
}

func kpkInitial(stm, wk, bk, wp int) byte {
	r := wp / 8
	if r == 0 || r == 7 || wk == bk || wk == wp || bk == wp || Dist(wk, bk) <= 1 {
		return kpkInvalid
	}
	if stm == chess.White {
		if chess.PawnAtt[chess.White][wp]&chess.SqBB(bk) != 0 {
			return kpkInvalid // rei preto em xeque com as brancas a jogar
		}
		if r == 6 {
			promo := wp + 8
			if promo != wk && promo != bk && (Dist(bk, promo) > 1 || Dist(wk, promo) <= 1) {
				return kpkWin
			}
		}
		return kpkUnknown
	}
	moves := chess.KingAtt[bk] &^ (chess.KingAtt[wk] | chess.PawnAtt[chess.White][wp])
	if moves&chess.SqBB(wp) != 0 {
		return kpkDraw // captura o peão indefeso
	}
	if moves == 0 {
		if chess.PawnAtt[chess.White][wp]&chess.SqBB(bk) != 0 {
			return kpkWin // mate
		}
		return kpkDraw // afogamento
	}
	return kpkUnknown
}

func kpkClassify(t []byte, stm, wk, bk, wp int) byte {
	if stm == chess.White {
		allDraw, any := true, false
		check := func(v byte) {
			any = true
			if v == kpkWin {
				allDraw = false
				any = true
			}
			if v != kpkDraw {
				allDraw = false
			}
		}
		moves := chess.KingAtt[wk] &^ chess.KingAtt[bk] &^ chess.SqBB(wp)
		for moves != 0 {
			to := chess.PopLSB(&moves)
			v := t[kpkIdx(chess.Black, to, bk, wp)]
			if v == kpkWin {
				return kpkWin
			}
			check(v)
		}
		if r := wp / 8; r < 6 {
			to := wp + 8
			if to != wk && to != bk {
				v := t[kpkIdx(chess.Black, wk, bk, to)]
				if v == kpkWin {
					return kpkWin
				}
				check(v)
				if r == 1 && to+8 != wk && to+8 != bk {
					v := t[kpkIdx(chess.Black, wk, bk, to+8)]
					if v == kpkWin {
						return kpkWin
					}
					check(v)
				}
			}
		}
		if !any || allDraw {
			return kpkDraw
		}
		return kpkUnknown
	}
	allWin := true
	moves := chess.KingAtt[bk] &^ (chess.KingAtt[wk] | chess.PawnAtt[chess.White][wp]) &^ chess.SqBB(wp)
	for moves != 0 {
		to := chess.PopLSB(&moves)
		v := t[kpkIdx(chess.White, wk, to, wp)]
		if v == kpkDraw {
			return kpkDraw
		}
		if v != kpkWin {
			allWin = false
		}
	}
	if allWin {
		return kpkWin
	}
	return kpkUnknown
}

// KPKProbe devolve se o lado com o peão vence (posição com só K+P vs K).
func KPKProbe(p *chess.Position) (strong int, win bool) {
	kpkOnce.Do(buildKPK)
	strong = chess.White
	if p.Pieces[chess.Black][chess.Pawn] != 0 {
		strong = chess.Black
	}
	wk, bk, wp := chess.LSB(p.Pieces[strong][chess.King]), chess.LSB(p.Pieces[strong^1][chess.King]), chess.LSB(p.Pieces[strong][chess.Pawn])
	stm := p.Side
	if strong == chess.Black { // espelha para que o lado forte seja sempre as brancas
		wk, bk, wp = wk^56, bk^56, wp^56
		stm ^= 1
	}
	return strong, kpkTable[kpkIdx(stm, wk, bk, wp)] == kpkWin
}

var simpleValue = [6]int{100, 320, 330, 500, 900, 0}

func nonPawnMaterial(p *chess.Position, c int) int {
	v := 0
	for pt := chess.Knight; pt <= chess.Queen; pt++ {
		v += p.Pieces[c][pt].Count() * simpleValue[pt]
	}
	return v
}

func edgeDist(sq int) int {
	f, r := sq%8, sq/8
	return min(min(f, 7-f), min(r, 7-r))
}

// evalEndgame trata finais com regra própria. ok=false: usar a avaliação normal.
func EvalEndgame(p *chess.Position) (int, bool) {
	wp, bp := p.Pieces[chess.White][chess.Pawn].Count(), p.Pieces[chess.Black][chess.Pawn].Count()
	if wp+bp > 1 {
		return 0, false
	}
	wm, bm := nonPawnMaterial(p, chess.White), nonPawnMaterial(p, chess.Black)
	stmSign := 1
	if p.Side == chess.Black {
		stmSign = -1
	}

	// Rei e peão contra rei: resultado exato.
	if wp+bp == 1 && wm == 0 && bm == 0 {
		strong, win := KPKProbe(p)
		if !win {
			return 0, true
		}
		v := 900 + 20*relRank(strong, chess.LSB(p.Pieces[strong][chess.Pawn]))
		if strong == chess.Black {
			v = -v
		}
		return v * stmSign, true
	}
	if wp+bp != 0 {
		return 0, false
	}

	// Sem peões.
	strong := chess.White
	if bm > wm {
		strong = chess.Black
	}
	sm, wkm := wm, bm
	if strong == chess.Black {
		sm, wkm = bm, wm
	}
	// Ninguém tem material para dar mate (K, K+menor, K+N+N contra rei sozinho).
	if sm <= 330 || (sm == 640 && p.Pieces[strong][chess.Knight].Count() == 2 && wkm == 0) {
		return 0, true
	}
	if wkm != 0 {
		return 0, false // ex.: torre contra bispo — fica com a avaliação normal (escalada)
	}

	// Lado fraco só com o rei: empurra-o para a borda e aproxima os reis.
	lk, sk := chess.LSB(p.Pieces[strong^1][chess.King]), chess.LSB(p.Pieces[strong][chess.King])
	v := sm + 1000
	sb := p.Pieces[strong]
	if sm == 650 && sb[chess.Bishop].Count() == 1 && sb[chess.Knight].Count() == 1 {
		// Bispo e cavalo: o mate só sai no canto da cor do bispo.
		bsq := chess.LSB(sb[chess.Bishop])
		corners := [2]int{0, 63} // casas escuras
		if (bsq%8+bsq/8)%2 == 1 {
			corners = [2]int{7, 56} // casas claras
		}
		cd := min(Dist(lk, corners[0]), Dist(lk, corners[1]))
		v += 40*(7-cd) + 10*(7-Dist(lk, sk))
	} else {
		v += 30*(3-edgeDist(lk)) + 10*(7-Dist(lk, sk))
	}
	if strong == chess.Black {
		v = -v
	}
	return v * stmSign, true
}

// endgameScale reduz a parte de final quando o lado melhor dificilmente vence.
func endgameScale(p *chess.Position) int {
	wm, bm := nonPawnMaterial(p, chess.White), nonPawnMaterial(p, chess.Black)
	strong := chess.White
	if bm+100*p.Pieces[chess.Black][chess.Pawn].Count() > wm+100*p.Pieces[chess.White][chess.Pawn].Count() {
		strong = chess.Black
	}
	sm, wkm := wm, bm
	if strong == chess.Black {
		sm, wkm = bm, wm
	}
	// Sem peões e vantagem de no máximo uma peça menor: quase sempre empate.
	if p.Pieces[strong][chess.Pawn] == 0 && sm-wkm <= 330 {
		if sm <= 330 {
			return 0
		}
		return 24
	}
	// Bispos de cores opostas, sem outras peças.
	wb, bb := p.Pieces[chess.White][chess.Bishop], p.Pieces[chess.Black][chess.Bishop]
	if wm == 330 && bm == 330 && wb.Count() == 1 && bb.Count() == 1 {
		ws, bs := chess.LSB(wb), chess.LSB(bb)
		if (ws%8+ws/8)%2 != (bs%8+bs/8)%2 {
			return 64
		}
	}
	return 128
}
