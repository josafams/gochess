//go:build !(js && wasm)

package train

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gochess/internal/chess"
	"gochess/internal/eval"
)

// Formato compacto de treino: 32 bytes por posição, para caber centenas de
// milhões de posições na RAM.
//
//	[0:8]   ocupação (bitboard, LE)
//	[8:24]  peças: 4 bits cada (cor*6+tipo), na ordem das casas ocupadas
//	[24:26] nota em centipeões, pov brancas (int16 LE)
//	[26]    resultado: 0 pretas, 1 empate, 2 brancas, 255 desconhecido
//	[27]    lado a jogar
//	[28:32] reservado
const packedSize = 32

func packPosition(p *chess.Position, score int, result float64) [packedSize]byte {
	var b [packedSize]byte
	occ := uint64(p.All)
	binary.LittleEndian.PutUint64(b[0:], occ)
	i := 0
	for o := occ; o != 0; o &= o - 1 {
		sq := chess.LSB(chess.Bitboard(o))
		pc := byte(p.Board[sq])
		if i%2 == 0 {
			b[8+i/2] |= pc
		} else {
			b[8+i/2] |= pc << 4
		}
		i++
	}
	score = max(-32000, min(32000, score))
	binary.LittleEndian.PutUint16(b[24:], uint16(int16(score)))
	switch {
	case result < 0:
		b[26] = 255
	case result < 0.25:
		b[26] = 0
	case result > 0.75:
		b[26] = 2
	default:
		b[26] = 1
	}
	b[27] = byte(p.Side)
	return b
}

// packedFeatures preenche os índices de entrada das duas perspectivas
// (quem joga, adversário) e devolve quantas peças há.
func packedFeatures(b []byte, us, them *[32]int16) (n int, side int) {
	occ := binary.LittleEndian.Uint64(b[0:])
	side = int(b[27])
	for o := occ; o != 0 && n < 32; o &= o - 1 {
		sq := chess.LSB(chess.Bitboard(o))
		pc := b[8+n/2]
		if n%2 == 1 {
			pc >>= 4
		}
		pc &= 15
		c, pt := int(pc)/6, int(pc)%6
		us[n] = int16(eval.Feature(side, c, pt, sq))
		them[n] = int16(eval.Feature(side^1, c, pt, sq))
		n++
	}
	return n, side
}

// packedTarget: alvo do treino do ponto de vista de quem joga.
func packedTarget(b []byte, lambda float64) float32 {
	score := float64(int16(binary.LittleEndian.Uint16(b[24:])))
	res := b[26]
	if b[27] == chess.Black {
		score = -score
	}
	t := sigmoid(score / 400)
	if res != 255 {
		r := float64(res) / 2
		if b[27] == chess.Black {
			r = 1 - r
		}
		t = lambda*r + (1-lambda)*t
	}
	return float32(t)
}

// readTextLine lê "FEN | nota | resultado" e devolve o registro compacto.
func readTextLine(line string) ([packedSize]byte, bool) {
	parts := strings.Split(line, "|")
	if len(parts) != 3 {
		return [packedSize]byte{}, false
	}
	p, err := chess.ParseFEN(strings.TrimSpace(parts[0]))
	if err != nil || p.All.Count() > 32 {
		return [packedSize]byte{}, false
	}
	score, err1 := strconv.Atoi(strings.TrimSpace(parts[1]))
	result, err2 := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
	if err1 != nil || err2 != nil {
		return [packedSize]byte{}, false
	}
	// Resultado fracionário (ex.: o sigmoid gravado pelo conversor do Lichess)
	// não é resultado de partida: marca como desconhecido.
	if result != 0 && result != 0.5 && result != 1 {
		result = -1
	}
	return packPosition(p, score, result), true
}

// runPack: gochess pack -in dados.txt -out dados.bin
func Pack(args []string) {
	fs := flag.NewFlagSet("pack", flag.ExitOnError)
	in := fs.String("in", "dados.txt", "texto do datagen")
	out := fs.String("out", "dados.bin", "saída compacta")
	fs.Parse(args)
	f, err := os.Open(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	o, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer o.Close()
	w := bufio.NewWriterSize(o, 1<<20)
	defer w.Flush()
	sc := bufio.NewScanner(f)
	n, bad := 0, 0
	for sc.Scan() {
		rec, ok := readTextLine(sc.Text())
		if !ok {
			bad++
			continue
		}
		w.Write(rec[:])
		n++
	}
	fmt.Fprintf(os.Stderr, "%d posições gravadas em %s (%d linhas inválidas)\n", n, *out, bad)
}
