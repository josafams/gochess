//go:build !(js && wasm)

package gui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"gochess/internal/api"
	"gochess/web"
)

// runGUI serve a interface web e a API em /api.
func Run(addr, netPath string) error {
	detectGUIEngines(netPath)
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(web.Files)))
	mux.HandleFunc("/config.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		list, _ := json.Marshal(guiEngines)
		fmt.Fprintf(w, "window.GOCHESS_BACKEND = 'http';\nwindow.GOCHESS_ENGINES = %s;", list)
	})
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "use POST", http.StatusMethodNotAllowed)
			return
		}
		var req api.APIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if ge := findGUIEngine(req.Engine); req.Action == "think" && ge != nil && ge.path != "" {
			json.NewEncoder(w).Encode(externalThink(ge, req))
			return
		}
		json.NewEncoder(w).Encode(api.HandleAPI(req))
	})
	log.Printf("GoChess rodando em http://%s", addr)
	return http.ListenAndServe(addr, mux)
}
