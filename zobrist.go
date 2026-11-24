package main

// Hash Zobrist: cada (peça, casa), lado a jogar, direitos de roque e coluna de
// en passant recebem um número aleatório de 64 bits. O hash da posição é o XOR
// de tudo que está presente, e é atualizado incrementalmente a cada lance.

var (
	pieceKeys  [12][64]uint64
	sideKey    uint64
	castleKeys [16]uint64
	epKeys     [8]uint64
)

func init() {
	// xorshift64* com semente fixa: o hash é o mesmo em toda execução.
	seed := uint64(0x9E3779B97F4A7C15)
	rnd := func() uint64 {
		seed ^= seed >> 12
		seed ^= seed << 25
		seed ^= seed >> 27
		return seed * 2685821657736338717
	}
	for pc := 0; pc < 12; pc++ {
		for sq := 0; sq < 64; sq++ {
			pieceKeys[pc][sq] = rnd()
		}
	}
	sideKey = rnd()
	for i := range castleKeys {
		castleKeys[i] = rnd()
	}
	for i := range epKeys {
		epKeys[i] = rnd()
	}
}

// computeHash recalcula o hash do zero (usado em testes para validar o incremental).
func (p *Position) computeHash() uint64 {
	var h uint64
	for sq := 0; sq < 64; sq++ {
		if pc := p.Board[sq]; pc >= 0 {
			h ^= pieceKeys[pc][sq]
		}
	}
	if p.Side == Black {
		h ^= sideKey
	}
	h ^= castleKeys[p.Castle]
	if p.EP >= 0 {
		h ^= epKeys[p.EP%8]
	}
	return h
}
