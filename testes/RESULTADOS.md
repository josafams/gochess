# Resultados das medições

Régua: Stockfish 17.1 com `UCI_LimitStrength` (1 thread), cadência 10+0.1, aberturas UHO.
Em cadência rápida o UCI_Elo do Stockfish não corresponde a um rating oficial:
os números servem para comparar versões, não como Elo CCRL.

## Versão base (avaliação sem tuning) — 24/11/2026

| Adversário | Placar (+ = −) | Pontos | Elo de diferença (95%) |
|---|---|---|---|
| SF 2000 (20 partidas) | +19 =1 −0 | 97,5% | — |
| SF 2400 (60) | +20 =2 −38 | 35% | −108 [−208, −22] |
| SF 2700 (60) | +15 =2 −43 | 27% | −176 [−292, −88] |
| SF 3000 (60) | +1 =2 −57 | 3% | −585 |
| SF máximo (100, 20 simultâneas) | +0 =1 −99 | 0,5% | — |

Estimativa: **~2300**.

## Texel tuning (commit 5cafef6) — 24/11/2026

SPRT [0, 5] contra a base, 8+0.08: **H1 aceita, +415 ± 61 Elo** (322 partidas, +285 =20 −17).

| Adversário | Placar (+ = −) | Pontos | Elo de diferença (95%) |
|---|---|---|---|
| SF 2400 (100) | +78 =2 −20 | 79% | +230 [+157, +328] |
| SF 2700 (100) | +50 =7 −43 | 53,5% | +24 [−41, +92] |

Estimativa: **~2700**.

## NNUE 768→256 (rede `nnue-v1.nnue`) — 24/11/2026

Treino: 252 milhões de posições (base de avaliações do Lichess, CC0, + 13,7 milhões de self-play),
15 épocas, lambda 0,5, validação 0,007787 → 0,007156 (2h11 na CPU).

SPRT [0, 5] contra a versão tunada, 8+0.08: **H1 aceita, +238 ± 37 Elo**
(392 partidas, +293 =39 −60, 79,7%).

| Adversário | Placar (+ = −) | Pontos | Elo de diferença (95%) |
|---|---|---|---|
| SF 2700 (100) | +73 =12 −15 | 79% | +230 [+162, +319] |
| SF 3000 (100) | +13 =23 −64 | 24,5% | −196 [−269, −135] |

Estimativa: **~2850–2900** (a escala do SF limitado não é linear: 2930 pelo 2700, 2800 pelo 3000).

## Velocidade (commit 3a09f76) — 24/11/2026

`gochess bench` (40 posições, profundidade 12, 1 thread; a assinatura de nós não mudou: 12651361):
553 mil → **2,45 milhões nps** (AVX2 na NNUE, magic bitboards, zero alocação, escolha preguiçosa de lances).
