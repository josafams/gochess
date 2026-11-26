//go:build !cgo

package fathom

// Sem cgo (ex.: WebAssembly) não há leitura de tablebases.

const (
	Loss        = 0
	BlessedLoss = 1
	Draw        = 2
	CursedWin   = 3
	Win         = 4
	Failed      = 0xFFFFFFFF
)

type Position struct {
	White, Black                                  uint64
	Kings, Queens, Rooks, Bishops, Knights, Pawns uint64
	Rule50                                        uint
	EP                                            uint
	WhiteToMove                                   bool
}

type RootMove struct {
	From, To int
	Promo    int
	WDL      int
	DTZ      int
}

const Available = false

func Init(path string) int                     { return 0 }
func ProbeWDL(p *Position) uint                { return Failed }
func ProbeRoot(p *Position) ([]RootMove, bool) { return nil, false }
