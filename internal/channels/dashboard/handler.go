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
<style>
*{box-sizing:border-box}
body{font-family:ui-monospace,Menlo,monospace;background:#0b1020;color:#e8eefc;margin:0;padding:0;height:100vh;display:flex;flex-direction:column}
.header{background:#151b2e;padding:10px 20px;border-bottom:1px solid #2a3447;display:flex;justify-content:space-between;align-items:center}
.header h1{margin:0;font-size:1.2em}
.main-content{flex:1;display:flex;flex-direction:column;overflow:hidden}
.chat-section{flex:1;display:flex;flex-direction:column;padding:20px;min-height:0}
.chat-container{flex:1;border:1px solid #333;padding:15px;overflow-y:auto;background:#0d1325;border-radius:8px;margin-bottom:10px;scroll-behavior:smooth}
.chat-message{margin:8px 0;padding:12px;border-radius:8px;max-width:85%;word-wrap:break-word}
.chat-user{background:#1a3a5c;margin-left:auto;margin-right:0}
.chat-assistant{background:#1a2e1a;margin-left:0;margin-right:auto;border-left:3px solid #4a9}
.chat-assistant pre{background:#0a150a;padding:10px;border-radius:4px;overflow-x:auto;margin:8px 0}
.chat-assistant code{background:#1a2e1a;padding:2px 6px;border-radius:3px;font-size:0.9em}
.chat-input{display:flex;gap:10px;padding:0}
.chat-input input{flex:1;padding:12px;border:1px solid #333;background:#151b2e;color:#e8eefc;border-radius:6px;font-size:1em}
.chat-input button{padding:12px 24px;background:#2a5;border:none;color:#fff;border-radius:6px;cursor:pointer;font-weight:bold}
.chat-input button:hover{background:#3b6}
.status-section{background:#151b2e;padding:15px 20px;border-top:1px solid #2a3447;max-height:200px;overflow-y:auto}
.status-header{display:flex;justify-content:space-between;align-items:center;margin-bottom:10px}
.status-header h3{margin:0;font-size:0.9em;color:#8a9}
.status-content{font-size:0.85em;background:#0b1020;padding:10px;border-radius:4px;max-height:120px;overflow-y:auto}
.admin-section{background:#0d1325;padding:20px;border-top:1px solid #2a3447;max-height:300px;overflow-y:auto}
.admin-tabs{display:flex;gap:10px;margin-bottom:15px}
.admin-tab{padding:8px 16px;background:#151b2e;border:1px solid #333;border-radius:4px;cursor:pointer;color:#8a9}
.admin-tab.active{background:#1a3a5c;color:#e8eefc}
.admin-panel{display:none}
.admin-panel.active{display:block}
textarea{width:100%;height:150px;background:#0b1020;color:#e8eefc;border:1px solid #333;padding:10px;border-radius:4px;font-family:monospace}
input[type="text"],input[type="password"]{background:#0b1020;color:#e8eefc;border:1px solid #333;padding:8px;border-radius:4px;margin:4px}
button{background:#2a4a6c;border:none;color:#e8eefc;padding:8px 16px;border-radius:4px;cursor:pointer;margin:4px}
button:hover{background:#3a5a7c}
pre{background:#0b1020;padding:10px;border-radius:4px;overflow-x:auto;font-size:0.85em}
.tool-call{background:#1a2a1a;border-left:3px solid #4a9;padding:10px;margin:8px 0;border-radius:0 4px 4px 0}
.tool-call code{background:#0a1a0a}
</style>
</head><body>
<div class="header">
<h1>Openclawssy Dashboard</h1>
<span style="color:#8a9;font-size:0.8em">Model: <span id="modelInfo">Loading...</span></span>
</div>

<div class="main-content">
<div class="chat-section">
<div class="chat-container" id="chatHistory"></div>
<div class="chat-input">
<input id="chatMessage" placeholder="Type your message and press Enter..." onkeypress="if(event.key==='Enter')sendChat()"/>
<button onclick="sendChat()">Send</button>
</div>
</div>

<div class="status-section">
<div class="status-header">
<h3>Status & Recent Runs</h3>
<button onclick="loadStatus()" style="padding:4px 12px;font-size:0.8em">Refresh</button>
</div>
<div class="status-content" id="status">Loading...</div>
</div>

<div class="admin-section">
<div class="admin-tabs">
<div class="admin-tab active" onclick="showTab('config')">Config</div>
<div class="admin-tab" onclick="showTab('secrets')">Secrets</div>
</div>
<div class="admin-panel active" id="config-panel">
<button onclick="loadConfig()">Load</button>
<button onclick="saveConfig()">Save</button>
<textarea id="cfg"></textarea>
</div>
<div class="admin-panel" id="secrets-panel">
<input id="sname" placeholder="provider/zai/api_key" style="width:300px"/>
<input id="svalue" placeholder="secret value" type="password" style="width:300px"/>
<button onclick="setSecret()">Store Secret</button>
<button onclick="listSecrets()">List Keys</button>
<pre id="secrets"></pre>
</div>
</div>
</div>

<script>
let chatMessages=[];
function showTab(tab){
document.querySelectorAll('.admin-tab').forEach(t=>t.classList.remove('active'));
document.querySelectorAll('.admin-panel').forEach(p=>p.classList.remove('active'));
event.target.classList.add('active');
document.getElementById(tab+'-panel').classList.add('active');
}
function getToken(){
const params=new URLSearchParams(window.location.search);
const urlToken=params.get('token');
if(urlToken){localStorage.setItem('token',urlToken);return urlToken;}
const stored=localStorage.getItem('token');
if(stored)return stored;
const prompted=prompt('Bearer token');
if(prompted)localStorage.setItem('token',prompted);
return prompted;
}
async function j(url,opts={}){
const t=getToken();
if(!t){alert('Bearer token required');return {error:'missing token'};}
opts.headers=Object.assign({'Authorization':'Bearer '+t,'Content-Type':'application/json'},opts.headers||{});
const r=await fetch(url,opts);
const txt=await r.text();
try{return JSON.parse(txt)}catch(e){return {raw:txt,status:r.status}}
}
async function loadStatus(){
const data=await j('/api/admin/status');
if(data.model){document.getElementById('modelInfo').textContent=data.model.provider+'/'+data.model.name;}
let html='Runs: '+(data.run_count||0);
if(data.runs&&data.runs.length>0){
html+='<br><br>Recent:<br>';
data.runs.slice(0,5).forEach(run=>{
const status=run.status==='completed'?'✓':run.status==='failed'?'✗':'⏳';
html+=status+' '+run.id+' ('+run.source+')<br>';
});
}
document.getElementById('status').innerHTML=html;
}
async function loadConfig(){document.getElementById('cfg').value=JSON.stringify(await j('/api/admin/config'),null,2);}
async function saveConfig(){
const body=document.getElementById('cfg').value;
const result=await j('/api/admin/config',{method:'POST',body});
alert(result.ok?'Saved!':'Error: '+JSON.stringify(result));
}
async function setSecret(){
const name=document.getElementById('sname').value;
const value=document.getElementById('svalue').value;
if(!name||!value){alert('Name and value required');return;}
const result=await j('/api/admin/secrets',{method:'POST',body:JSON.stringify({name,value})});
document.getElementById('secrets').textContent=JSON.stringify(result,null,2);
document.getElementById('svalue').value='';
}
async function listSecrets(){document.getElementById('secrets').textContent=JSON.stringify(await j('/api/admin/secrets'),null,2);}

function formatContent(text){
if(!text)return'';
text=text.replace(/\u0026/g,'\u0026amp;').replace(/\u003c/g,'\u0026lt;').replace(/\u003e/g,'\u0026gt;');
text=text.replace(/\u0060\u0060\u0060(\w+)?\n([\s\S]*?)\u0060\u0060\u0060/g,'\u003cpre\u003e\u003ccode\u003e$2\u003c/code\u003e\u003c/pre\u003e');
text=text.replace(/\u0060([^\u0060]+)\u0060/g,'\u003ccode\u003e$1\u003c/code\u003e');
text=text.replace(/\n/g,'\u003cbr\u003e');
return text;
}
function renderChat(){
const container=document.getElementById('chatHistory');
container.innerHTML=chatMessages.map(m=>{
const roleClass=m.role==='user'?'chat-user':'chat-assistant';
const roleLabel=m.role==='user'?'You':'Bot';
const content=formatContent(m.content);
return'<div class="chat-message '+roleClass+'"><strong>'+roleLabel+':</strong><br>'+content+'</div>';
}).join('');
container.scrollTop=container.scrollHeight;
}
async function pollRun(runId){
for(let i=0;i<60;i++){
await new Promise(r=>setTimeout(r,1000));
const run=await j('/v1/runs/'+runId);
if(run.status==='completed'&&run.output)return run.output;
if(run.status==='failed')return'Error: '+(run.error||'Run failed');
}
return'Timeout waiting for response';
}
async function sendChat(){
const input=document.getElementById('chatMessage');
const message=input.value.trim();
if(!message)return;
chatMessages.push({role:'user',content:message});
renderChat();
input.value='';
const thinkingIdx=chatMessages.length;
chatMessages.push({role:'assistant',content:'Thinking...'});
renderChat();
try{
const result=await j('/v1/chat/messages',{method:'POST',body:JSON.stringify({user_id:'dashboard_user',message,agent_id:'default'})});
if(result.error){
chatMessages[thinkingIdx]={role:'assistant',content:'Error: '+result.error};
}else if(result.id){
const output=await pollRun(result.id);
chatMessages[thinkingIdx]={role:'assistant',content:output};
}else if(result.response){
chatMessages[thinkingIdx]={role:'assistant',content:result.response};
}else{
chatMessages[thinkingIdx]={role:'assistant',content:JSON.stringify(result)};
}
renderChat();
loadStatus();
}catch(e){
chatMessages[thinkingIdx]={role:'assistant',content:'Error: '+e.message};
renderChat();
}
}
loadStatus();
setInterval(loadStatus,30000);
</script></body></html>`
