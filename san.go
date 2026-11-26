package main

import "strings"

func normalizeSAN(s string) string {
	s = strings.TrimRight(s, "+#!?")
	s = strings.ReplaceAll(s, "=", "")
	s = strings.ReplaceAll(s, "0-0-0", "O-O-O")
	s = strings.ReplaceAll(s, "0-0", "O-O")
	return s
}

// ParseSAN aceita "Nf3", "exd5", "O-O", "e8=Q", "e8Q" e também notação UCI.
func (p *Position) ParseSAN(s string) (Move, bool) {
	want := normalizeSAN(s)
	for _, m := range p.LegalMoves() {
		if normalizeSAN(p.SAN(m)) == want {
			return m, true
		}
	}
	return p.ParseUCIMove(strings.ToLower(s))
}
