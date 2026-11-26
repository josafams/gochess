// Package web guarda os arquivos da interface (HTML, CSS e JS) embutidos no binário.
package web

import "embed"

// Files tem a interface principal (index.html, style.css, app.js).
//
//go:embed index.html style.css app.js
var Files embed.FS

// MatchPage é a página que acompanha partidas entre engines (gochess match).
//
//go:embed match.html
var MatchPage []byte
