#!/bin/sh
# SPRT entre duas versões do GoChess (fastchess). Para assistir, abra o PGN depois.
#   testes/sprt.sh NOVO BASE [TC] [CONC] [OPCOES_NOVO]
# Ex.: testes/sprt.sh ./gochess-tuned ./gochess-base
set -e
cd "$(dirname "$0")/.."
NEW=${1:?uso: sprt.sh NOVO BASE}; BASE=${2:?}; TC=${3:-8+0.08}; CONC=${4:-5}; NEWOPT=${5:-}
FC=$HOME/chess-tools/fc-src/fastchess
BOOK=$HOME/chess-tools/UHO_Lichess_4852_v1.epd
mkdir -p testes/out
TAG="$(basename "$NEW")_vs_$(basename "$BASE")"
"$FC" -engine cmd="$NEW" name=novo option.OwnBook=false option.Hash=16 $NEWOPT \
      -engine cmd="$BASE" name=base option.OwnBook=false option.Hash=16 \
      -each tc="$TC" proto=uci \
      -openings file="$BOOK" format=epd order=random \
      -rounds 20000 -repeat -concurrency "$CONC" -recover \
      -sprt elo0=0 elo1=5 alpha=0.05 beta=0.05 model=normalized \
      -pgnout file="testes/out/sprt_$TAG.pgn" \
  | tee "testes/out/sprt_$TAG.log"
