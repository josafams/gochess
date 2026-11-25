//go:build !h512

package main

// Tamanho da camada oculta. Compile com -tags h512 para a rede 768→512.
const nnHidden = 256
