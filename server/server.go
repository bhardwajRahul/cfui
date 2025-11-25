package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"

	"cfui/config"
	"cfui/service"

	"github.com/BurntSushi/toml"
)

type Server struct {
	cfgMgr  *config.Manager
	runner  *service.Runner
	assets  embed.FS
	locales embed.FS
}

func NewServer(cfgMgr *config.Manager, runner *service.Runner, assets embed.FS, locales embed.FS) *Server {
	return &Server{
		cfgMgr:  cfgMgr,
		runner:  runner,
		assets:  assets,
		locales: locales,
	}
}

func (s *Server) Run(addr string) error {
	mux := http.NewServeMux()

	// API Endpoints
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/control", s.handleControl)
	mux.HandleFunc("/api/i18n/", s.handleI18n)

	// Static Files
	// The assets are in "web/dist", so we need to strip that prefix
	fsys, err := fs.Sub(s.assets, "web/dist")
	if err != nil {
		return err
	}
	mux.Handle("/", http.FileServer(http.FS(fsys)))

	log.Printf("Server listening on %s", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		json.NewEncoder(w).Encode(s.cfgMgr.Get())
		return
	}

	if r.Method == http.MethodPost {
		var cfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if err := s.cfgMgr.Save(cfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	running, err := s.runner.Status()
	status := "stopped"
	if running {
		status = "running"
	}

	resp := map[string]interface{}{
		"running": running,
		"status":  status,
	}
	if err != nil {
		resp["error"] = err.Error()
		resp["status"] = "error"
	}

	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var err error
	switch req.Action {
	case "start":
		err = s.runner.Start()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "stop":
		// For stop action, respond immediately and stop asynchronously
		// This prevents the client from getting "Failed to fetch" when the tunnel shuts down
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"action":  "stop",
			"message": "Tunnel stop initiated",
		})
		go func() {
			if stopErr := s.runner.Stop(); stopErr != nil {
				log.Printf("Error stopping tunnel: %v", stopErr)
			}
		}()
		return
	default:
		http.Error(w, "Invalid action", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"action":  req.Action,
		"message": "Tunnel started successfully",
	})
}

func (s *Server) handleI18n(w http.ResponseWriter, r *http.Request) {
	// Extract language from path: /api/i18n/en -> "en"
	lang := r.URL.Path[len("/api/i18n/"):]
	if lang == "" {
		lang = "en"
	}

	// Read the corresponding TOML file
	filePath := "locales/" + lang + ".toml"
	data, err := s.locales.ReadFile(filePath)
	if err != nil {
		http.Error(w, "Language not found", http.StatusNotFound)
		return
	}

	// Parse TOML into a map
	var translations map[string]map[string]string
	if err := toml.Unmarshal(data, &translations); err != nil {
		http.Error(w, "Failed to parse translations", http.StatusInternalServerError)
		return
	}

	// Convert to simplified format: key -> translation
	simple := make(map[string]string)
	for key, value := range translations {
		if other, ok := value["other"]; ok {
			simple[key] = other
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(simple)
}
