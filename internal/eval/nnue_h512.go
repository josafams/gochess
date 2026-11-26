//go:build !h256

package eval

// Tamanho da camada oculta: 768→512 é o padrão desde a rede aprovada em 25/11/2026
// (SPRT +43 contra a v1). Compile com -tags h256 para a rede antiga.
const Hidden = 512
