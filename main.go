//go:build !(js && wasm)

// GoChess: engine de xadrez em Go, para estudo.
//
//	board.go    bitboards, FEN
//	attacks.go  tabelas de ataque e detecção de xeque
//	movegen.go  geração de lances, make (copy-make), perft, SAN
//	zobrist.go  hash da posição
//	tt.go       tabela de transposição
//	eval.go     material + tabelas peça-casa
//	search.go   alfa-beta/PVS, null move, LMR, aspiration windows...
//	uci.go      protocolo UCI
//	api.go      API JSON da interface web
//	gui.go      servidor local da interface  (gochess gui)
//	wasm.go     a mesma API rodando no navegador (WebAssembly)
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "datagen" {
		runDatagen(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "tune" {
		runTune(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "train" {
		runTrain(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "pack" {
		runPack(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "lichess" {
		runLichess(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "bench" {
		runBench(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "match" {
		runMatch(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "gui" {
		fs := flag.NewFlagSet("gui", flag.ExitOnError)
		addr := fs.String("addr", "localhost:8080", "endereço do servidor")
		net := fs.String("net", "gochess-768x256.nnue", "rede NNUE do adversário \"GoChess + NNUE\" (se existir)")
		fs.Parse(os.Args[2:])
		if err := runGUI(*addr, *net); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	uciLoop()
}
