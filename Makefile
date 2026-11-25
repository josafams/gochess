# Builds do GoChess.
#   make          -> gochess para esta máquina (GOAMD64=v3: AVX2/BMI2/POPCNT; ~3,5% mais rápido)
#   make portable -> gochess-portable para CPUs x86-64 antigas (sem v3)
#   make h512     -> gochess-h512 com a rede 768→512
GO ?= go

gochess:
	GOAMD64=v3 $(GO) build -o gochess .

portable:
	$(GO) build -o gochess-portable .

h512:
	GOAMD64=v3 $(GO) build -tags h512 -o gochess-h512 .

test:
	$(GO) vet ./... && $(GO) test ./...

.PHONY: gochess portable h512 test
