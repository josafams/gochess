//go:build !(js && wasm)

package main

import (
	"flag"
	"fmt"
	"os"

	"gochess/internal/bench"
	"gochess/internal/gui"
	"gochess/internal/match"
	"gochess/internal/train"
	"gochess/internal/uci"
)

func main() {
	if len(os.Args) < 2 {
		uci.Loop()
		return
	}
	args := os.Args[2:]
	switch os.Args[1] {
	case "datagen":
		train.Datagen(args)
	case "tune":
		train.Tune(args)
	case "train":
		train.Train(args)
	case "pack":
		train.Pack(args)
	case "lichess":
		train.Lichess(args)
	case "bench":
		bench.Run(args)
	case "match":
		match.Run(args)
	case "gui":
		fs := flag.NewFlagSet("gui", flag.ExitOnError)
		addr := fs.String("addr", "localhost:8080", "endereço do servidor")
		net := fs.String("net", "gochess-768x256.nnue", "rede NNUE do adversário \"GoChess + NNUE\" (se existir)")
		fs.Parse(args)
		if err := gui.Run(*addr, *net); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		uci.Loop()
	}
}
