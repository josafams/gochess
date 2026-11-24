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
