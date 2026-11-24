package main

import (
	"fmt"
	"math/bits"
	"strconv"
	"strings"
)

// Bitboard: um bit por casa. a1 = bit 0, h1 = bit 7, a8 = bit 56, h8 = bit 63.
type Bitboard uint64

const (
	White = 0
	Black = 1
)

const (
	Pawn = iota
	Knight
	Bishop
	Rook
	Queen
	King
)

const (
	CastleWK = 1 << iota
	CastleWQ
	CastleBK
	CastleBQ
)

const StartFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"

// Position guarda o estado completo do jogo. É pequena o bastante para ser
// copiada a cada lance (copy-make), o que dispensa a lógica de "desfazer lance".
type Position struct {
	Pieces   [2][6]Bitboard // [cor][tipo]
	Occ      [2]Bitboard    // ocupação por cor
	All      Bitboard       // ocupação total
	Board    [64]int8       // -1 = vazio; senão cor*6 + tipo (lookup rápido)
	Side     int            // quem joga
	Castle   uint8          // direitos de roque
	EP       int            // casa de en passant, -1 se não houver
	Halfmove int            // regra dos 50 lances
	Fullmove int
	Hash     uint64 // hash Zobrist, atualizado incrementalmente
}

func sqBB(sq int) Bitboard { return 1 << uint(sq) }

func lsb(b Bitboard) int { return bits.TrailingZeros64(uint64(b)) }

func popLSB(b *Bitboard) int {
	sq := lsb(*b)
	*b &= *b - 1
	return sq
}

func (b Bitboard) Count() int { return bits.OnesCount64(uint64(b)) }

func (p *Position) put(color, pt, sq int) {
	bb := sqBB(sq)
	p.Pieces[color][pt] |= bb
	p.Occ[color] |= bb
	p.All |= bb
	p.Board[sq] = int8(color*6 + pt)
	p.Hash ^= pieceKeys[color*6+pt][sq]
}

func (p *Position) remove(sq int) {
	pc := p.Board[sq]
	if pc < 0 {
		return
	}
	color, pt := int(pc)/6, int(pc)%6
	mask := ^sqBB(sq)
	p.Pieces[color][pt] &= mask
	p.Occ[color] &= mask
	p.All &= mask
	p.Board[sq] = -1
	p.Hash ^= pieceKeys[pc][sq]
}

func sqName(sq int) string {
	return string([]byte{byte('a' + sq%8), byte('1' + sq/8)})
}

func parseSquare(s string) int {
	if len(s) != 2 || s[0] < 'a' || s[0] > 'h' || s[1] < '1' || s[1] > '8' {
		return -1
	}
	return int(s[1]-'1')*8 + int(s[0]-'a')
}

func ParseFEN(fen string) (*Position, error) {
	f := strings.Fields(fen)
	if len(f) < 4 {
		return nil, fmt.Errorf("FEN inválida: %q", fen)
	}
	p := &Position{EP: -1, Fullmove: 1}
	for i := range p.Board {
		p.Board[i] = -1
	}

	rank, file := 7, 0
	for _, c := range f[0] {
		switch {
		case c == '/':
			rank--
			file = 0
		case c >= '1' && c <= '8':
			file += int(c - '0')
		default:
			idx := strings.IndexRune("PNBRQKpnbrqk", c)
			if idx < 0 || file > 7 || rank < 0 {
				return nil, fmt.Errorf("FEN inválida: %q", fen)
			}
			p.put(idx/6, idx%6, rank*8+file)
			file++
		}
	}

	if f[1] == "b" {
		p.Side = Black
	}
	for _, c := range f[2] {
		switch c {
		case 'K':
			p.Castle |= CastleWK
		case 'Q':
			p.Castle |= CastleWQ
		case 'k':
			p.Castle |= CastleBK
		case 'q':
			p.Castle |= CastleBQ
		}
	}
	if f[3] != "-" {
		p.EP = parseSquare(f[3])
	}
	if len(f) > 4 {
		p.Halfmove, _ = strconv.Atoi(f[4])
	}
	if len(f) > 5 {
		p.Fullmove, _ = strconv.Atoi(f[5])
	}
	if p.Pieces[White][King].Count() != 1 || p.Pieces[Black][King].Count() != 1 {
		return nil, fmt.Errorf("FEN precisa de exatamente um rei de cada cor")
	}
	p.Hash = p.computeHash()
	return p, nil
}

func (p *Position) String() string {
	var sb strings.Builder
	for r := 7; r >= 0; r-- {
		sb.WriteString(fmt.Sprintf(" %d  ", r+1))
		for f := 0; f < 8; f++ {
			pc := p.Board[r*8+f]
			if pc < 0 {
				sb.WriteString(". ")
			} else {
				sb.WriteByte("PNBRQKpnbrqk"[pc])
				sb.WriteByte(' ')
			}
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("\n    a b c d e f g h\n")
	side := "brancas"
	if p.Side == Black {
		side = "pretas"
	}
	sb.WriteString("Vez das " + side + "\n")
	return sb.String()
}

// FEN devolve a posição em notação FEN.
func (p *Position) FEN() string {
	var sb strings.Builder
	for r := 7; r >= 0; r-- {
		empty := 0
		for f := 0; f < 8; f++ {
			pc := p.Board[r*8+f]
			if pc < 0 {
				empty++
				continue
			}
			if empty > 0 {
				sb.WriteByte(byte('0' + empty))
				empty = 0
			}
			sb.WriteByte("PNBRQKpnbrqk"[pc])
		}
		if empty > 0 {
			sb.WriteByte(byte('0' + empty))
		}
		if r > 0 {
			sb.WriteByte('/')
		}
	}
	if p.Side == White {
		sb.WriteString(" w ")
	} else {
		sb.WriteString(" b ")
	}
	castle := ""
	for i, c := range "KQkq" {
		if p.Castle&(1<<i) != 0 {
			castle += string(c)
		}
	}
	if castle == "" {
		castle = "-"
	}
	sb.WriteString(castle)
	if p.EP >= 0 {
		sb.WriteString(" " + sqName(p.EP))
	} else {
		sb.WriteString(" -")
	}
	sb.WriteString(fmt.Sprintf(" %d %d", p.Halfmove, p.Fullmove))
	return sb.String()
}

func (p *Position) hasNonPawn(color int) bool {
	pc := &p.Pieces[color]
	return pc[Knight]|pc[Bishop]|pc[Rook]|pc[Queen] != 0
}

// InsufficientMaterial: rei contra rei, ou rei e uma peça menor contra rei.
func (p *Position) InsufficientMaterial() bool {
	for c := White; c <= Black; c++ {
		if p.Pieces[c][Pawn]|p.Pieces[c][Rook]|p.Pieces[c][Queen] != 0 {
			return false
		}
	}
	minors := 0
	for c := White; c <= Black; c++ {
		minors += (p.Pieces[c][Knight] | p.Pieces[c][Bishop]).Count()
	}
	return minors <= 1
}
