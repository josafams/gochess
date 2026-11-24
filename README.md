# GoChess

Engine de xadrez em Go, para estudo, com interface web. Sem dependências externas.

## Usar

```sh
go test ./...                 # perft, hash, SEE, KPK, simetria, NNUE, SAN, PGN, táticas
go build -o gochess .

./gochess gui                 # interface em http://localhost:8080
./gochess                     # modo UCI (Arena, Cute Chess, En Croissant, lichess-bot)

./build_web.sh                # dist/: interface + engine em WebAssembly, sem servidor
python3 -m http.server -d dist

# Treino
./gochess datagen -duration 30m -nodes 4000 -workers 2 -out dados.txt
./gochess tune  -in dados.txt -epochs 1500          # gera tuned_params.go
./gochess train -in dados.txt -epochs 30 -out gochess.nnue
```

## Engine

**Busca** (`search.go`, `see.go`, `tt.go`)
- Negamax alfa-beta com PVS, aprofundamento iterativo e aspiration windows
- Tabela de transposição sem travas (compartilhada entre threads), hash Zobrist
- **Lazy SMP** (opção `Threads`)
- Null move, reverse futility, futility, **late move pruning**, **ProbCut**, LMR, IIR, mate distance pruning
- **SEE**: ordena capturas e poda as perdedoras (também na quiescence)
- Quiescence com evasões quando em xeque, delta pruning
- Ordenação: TT → promoção → boas capturas (MVV-LVA) → killers → history (com malus) → capturas ruins
- MultiPV, `go nodes`, `go mate`, `go ponder`/`ponderhit`, `movestogo`

**Avaliação** (`eval.go`, `endgame.go`)
- Todos os termos com peso de meio-jogo e de final, interpolados pela fase
- Material, tabelas peça-casa, mobilidade por peça
- Peões: dobrados, isolados, atrasados, apoiados, falange, passados (por fileira, caminho livre, distância dos reis)
- Torres em coluna aberta/semiaberta e na 7ª, par de bispos, postos avançados de cavalo
- Segurança do rei: ataques à zona do rei por tipo de peça, escudo de peões, colunas abertas
- Ameaças: peão atacando peça, peça menor atacando torre/dama, peças penduradas
- **Pesos ajustados por Texel tuning** (`tune.go` → `tuned_params.go`)
- Finais: **tabela exata KPK** gerada por análise retrógrada, mate de bispo e cavalo, mop-up, empates por material, bispos de cores opostas

**NNUE** (`nnue.go`, `train.go`)
- Rede 768 → 128 ×2 perspectivas → 1, SCReLU, int16, acumulador incremental por ply
- Treino em Go puro com Adam a partir dos dados de autojogo
- Ativada com a opção UCI `UseNNUE`

**Livro** (`book.go`): ~45 linhas de aberturas em SAN (opção `OwnBook`).

## Interface (`web/`)

- Jogar contra a engine (6 níveis) ou **analisar** (3 melhores linhas, contínuo)
- Arrastar ou tocar para mover, promoção, animação, som
- **Relógio** (1+0 a 15+10), a engine usa o tempo dela
- Navegar pelos lances (botões, setas do teclado, clique na súmula)
- Carregar **FEN** ou **PGN**, copiar FEN/PGN, desfazer, dica, girar
- Peças em SVG próprias, tema claro/escuro, celular e desktop

## O que não está aqui

- **Tablebases Syzygy**: exigem arquivos de vários GB e um port do código de leitura;
  em vez disso há a tabela KPK exata e regras de finais.
- **Livro Polyglot (.bin)**: o livro é embutido em SAN.
