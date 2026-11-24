#!/bin/sh
# Mede o GoChess contra o Stockfish limitado, com página ao vivo em http://localhost:8090
#   testes/gauntlet.sh ELO [JOGOS] [TC] [ENGINE] [CONCORRENCIA]
# Ex.: testes/gauntlet.sh 2100 200 10+0.1 ./gochess-base 5
# Opções do GoChess: E1OPTS="OwnBook=false,Hash=64,EvalFile=rede.nnue"; nome nos arquivos: TAG=nnue1
set -e
cd "$(dirname "$0")/.."
ELO=${1:?uso: gauntlet.sh ELO [JOGOS] [TC] [ENGINE] [CONC]}
GAMES=${2:-200}; TC=${3:-10+0.1}; ENG=${4:-./gochess-base}; CONC=${5:-5}
mkdir -p testes/out
OUT="testes/out/gauntlet_${TAG:-$(basename "$ENG")}_sf$ELO"
"${MATCH:-./gochess-tools}" match -addr "${ADDR:-localhost:8090}" -e1 "$ENG" -e1opts "${E1OPTS:-OwnBook=false,Hash=64}" -e2opts "UCI_LimitStrength=true,UCI_Elo=$ELO,Hash=64,Threads=1" \
  -games "$GAMES" -tc "$TC" -concurrency "$CONC" -keep=false \
  -pgn "$OUT.pgn" \
  | tee "$OUT.log"
