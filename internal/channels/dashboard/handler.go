package dashboard

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	httpchannel "openclawssy/internal/channels/http"
	"openclawssy/internal/config"
	"openclawssy/internal/secrets"
)

type Handler struct {
	rootDir string
	store   httpchannel.RunStore
}

func New(rootDir string, store httpchannel.RunStore) *Handler {
	return &Handler{rootDir: rootDir, store: store}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/dashboard", h.serveDashboard)
	mux.HandleFunc("/api/admin/status", h.getStatus)
	mux.HandleFunc("/api/admin/config", h.handleConfig)
	mux.HandleFunc("/api/admin/secrets", h.handleSecrets)
}

func (h *Handler) serveDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}

func (h *Handler) getStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	runs, err := h.store.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cfg, err := config.LoadOrDefault(filepath.Join(h.rootDir, ".openclawssy", "config.json"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := map[string]any{
		"run_count": len(runs),
		"runs":      runs,
		"model": map[string]any{
			"provider": cfg.Model.Provider,
			"name":     cfg.Model.Name,
		},
		"discord_enabled": cfg.Discord.Enabled,
	}
	writeJSON(w, out)
}

func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(h.rootDir, ".openclawssy", "config.json")
	if r.Method == http.MethodGet {
		cfg, err := config.LoadOrDefault(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		cfg.Providers.OpenAI.APIKey = ""
		cfg.Providers.OpenRouter.APIKey = ""
		cfg.Providers.Requesty.APIKey = ""
		cfg.Providers.ZAI.APIKey = ""
		cfg.Providers.Generic.APIKey = ""
		cfg.Discord.Token = ""
		writeJSON(w, cfg)
		return
	}
	if r.Method == http.MethodPost {
		var cfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := config.Save(path, cfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (h *Handler) handleSecrets(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.LoadOrDefault(filepath.Join(h.rootDir, ".openclawssy", "config.json"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	store, err := secrets.NewStore(cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodGet {
		keys, err := store.ListKeys()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"keys": keys})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Name == "" || req.Value == "" {
			http.Error(w, "name and value are required", http.StatusBadRequest)
			return
		}
		if err := store.Set(req.Name, req.Value); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "stored": req.Name})
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

const dashboardHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>Openclawssy Dashboard</title>
<style>body{font-family:ui-monospace,Menlo,monospace;background:#0b1020;color:#e8eefc;padding:20px}button{margin:4px}textarea{width:100%;height:220px}input{margin:4px;width:360px}</style>
</head><body>
<h1>Openclawssy Dashboard</h1>
<p>Use Bearer token from server config.</p>
<button onclick="loadStatus()">Refresh Status</button>
<pre id="status"></pre>
<h2>Config</h2>
<button onclick="loadConfig()">Load Config</button>
<button onclick="saveConfig()">Save Config</button>
<textarea id="cfg"></textarea>
<h2>Secrets (Write-Only Values)</h2>
<input id="sname" placeholder="provider/openrouter/api_key"/>
<input id="svalue" placeholder="secret value" type="password"/>
<button onclick="setSecret()">Store Secret</button>
<button onclick="listSecrets()">List Secret Keys</button>
<pre id="secrets"></pre>
<script>
async function j(url,opts={}){const t=localStorage.getItem('token')||prompt('Bearer token');localStorage.setItem('token',t);opts.headers=Object.assign({'Authorization':'Bearer '+t,'Content-Type':'application/json'},opts.headers||{});const r=await fetch(url,opts);const txt=await r.text();try{return JSON.parse(txt)}catch(e){return {raw:txt,status:r.status}}}
async function loadStatus(){document.getElementById('status').textContent=JSON.stringify(await j('/api/admin/status'),null,2)}
async function loadConfig(){document.getElementById('cfg').value=JSON.stringify(await j('/api/admin/config'),null,2)}
async function saveConfig(){const body=document.getElementById('cfg').value;document.getElementById('status').textContent=JSON.stringify(await j('/api/admin/config',{method:'POST',body}),null,2)}
async function setSecret(){const name=document.getElementById('sname').value;const value=document.getElementById('svalue').value;document.getElementById('secrets').textContent=JSON.stringify(await j('/api/admin/secrets',{method:'POST',body:JSON.stringify({name,value})}),null,2);document.getElementById('svalue').value=''}
async function listSecrets(){document.getElementById('secrets').textContent=JSON.stringify(await j('/api/admin/secrets'),null,2)}
loadStatus();
</script></body></html>`
