# Builds do GoChess.
#   make          -> gochess para esta máquina (GOAMD64=v3: AVX2/BMI2/POPCNT; ~3,5% mais rápido)
#   make portable -> gochess-portable para CPUs x86-64 antigas (sem v3)
#   make h256     -> gochess-h256 com a rede antiga 768→256
GO ?= go

gochess:
	GOAMD64=v3 $(GO) build -o gochess ./cmd/gochess

portable:
	GOAMD64=v1 $(GO) build -o gochess-portable ./cmd/gochess

h256:
	GOAMD64=v3 $(GO) build -tags h256 -o gochess-h256 ./cmd/gochess

test:
	$(GO) vet ./... && $(GO) test ./...

.PHONY: gochess portable h256 test pgo

# Regenera o perfil do PGO (cmd/gochess/default.pgo) a partir do bench.
# Rode de novo depois de mudanças grandes na busca ou na avaliação.
pgo:
	GOAMD64=v3 $(GO) build -pgo=off -o /tmp/gochess-nopgo ./cmd/gochess
	/tmp/gochess-nopgo bench -cpuprofile /tmp/gochess-cpu.out > /dev/null
	$(GO) tool pprof -proto /tmp/gochess-cpu.out > cmd/gochess/default.pgo
