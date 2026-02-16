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
.header{background:#151b2e;padding:10px 20px;border-bottom:1px solid #2a3447;display:flex;justify-content:space-between;align-items:center;gap:12px;flex-wrap:wrap}
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
.admin-section{background:#0d1325;padding:20px;border-top:1px solid #2a3447;max-height:420px;overflow-y:auto}
.admin-tabs{display:flex;gap:10px;margin-bottom:15px}
.admin-tab{padding:8px 16px;background:#151b2e;border:1px solid #333;border-radius:4px;cursor:pointer;color:#8a9}
.admin-tab.active{background:#1a3a5c;color:#e8eefc}
.admin-panel{display:none}
.admin-panel.active{display:block}
.section-title{color:#9db2d4;margin:14px 0 8px 0;font-size:0.9em;font-weight:bold}
.form-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}
.field{display:flex;flex-direction:column;gap:4px}
.field label{font-size:0.8em;color:#8a9}
.field input,.field select,.field textarea{width:100%;background:#0b1020;color:#e8eefc;border:1px solid #333;padding:8px;border-radius:4px;font-family:inherit}
.field textarea{min-height:72px;resize:vertical}
.field.full{grid-column:1 / -1}
.toggles{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px 14px;margin:8px 0}
.toggle{display:flex;align-items:center;gap:8px;font-size:0.85em;color:#d1d9eb}
.toggle input{margin:0}
.button-row{display:flex;flex-wrap:wrap;gap:8px;margin-bottom:10px}
button{background:#2a4a6c;border:none;color:#e8eefc;padding:8px 16px;border-radius:4px;cursor:pointer}
button:hover{background:#3a5a7c}
button.secondary{background:#254054}
button.secondary:hover{background:#2d4c61}
.msg{min-height:18px;font-size:0.82em;margin:0 0 8px 0}
.msg.error{color:#ff8f8f}
.msg.success{color:#8ff0c1}
pre{background:#0b1020;padding:10px;border-radius:4px;overflow-x:auto;font-size:0.85em}
.helper-box{padding:10px;border:1px solid #2a3447;border-radius:6px;background:#10182b;margin-bottom:10px}
.helper-label{font-size:0.8em;color:#8a9;margin-bottom:4px}
details{margin-top:8px}
summary{cursor:pointer;color:#9db2d4}
@media (max-width:900px){
  .admin-section{max-height:none}
  .form-grid,.toggles{grid-template-columns:1fr}
  .chat-input{flex-wrap:wrap}
  .chat-input button{padding:10px 14px}
}
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
<button onclick="startNewChat()">New chat</button>
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
<div class="admin-tab active" onclick="showTab(event,'config')">Config</div>
<div class="admin-tab" onclick="showTab(event,'secrets')">Secrets</div>
</div>
<div class="admin-panel active" id="config-panel">
<div class="button-row">
<button onclick="loadConfig()">Load</button>
<button onclick="saveConfig()">Save</button>
</div>
<div class="msg" id="cfgMessage"></div>

<div class="form-grid">
<div class="field">
<label for="cfgProvider">Provider</label>
<select id="cfgProvider" onchange="onProviderChange()">
<option value="openai">openai</option>
<option value="openrouter">openrouter</option>
<option value="requesty">requesty</option>
<option value="zai">zai</option>
<option value="generic">generic</option>
</select>
</div>
<div class="field">
<label for="cfgModelName">Model name</label>
<input id="cfgModelName" type="text" placeholder="gpt-4.1-mini"/>
</div>
<div class="field">
<label for="cfgTemperature">Temperature</label>
<input id="cfgTemperature" type="number" step="0.1" placeholder="0.2"/>
</div>
<div class="field" id="genericBaseRow" style="display:none">
<label for="cfgGenericBaseURL">Generic base_url</label>
<input id="cfgGenericBaseURL" type="text" placeholder="https://example.com/v1"/>
</div>
</div>

<div class="section-title">Feature Toggles</div>
<div class="toggles">
<label class="toggle"><input id="cfgChatEnabled" type="checkbox"/>Chat enabled</label>
<label class="toggle"><input id="cfgDiscordEnabled" type="checkbox"/>Discord enabled</label>
<label class="toggle"><input id="cfgSandboxActive" type="checkbox"/>Sandbox active</label>
<label class="toggle"><input id="cfgShellExecEnabled" type="checkbox"/>Shell exec enabled</label>
</div>

<div class="section-title">Allowlists</div>
<div class="form-grid">
<div class="field">
<label for="cfgChatUsers">Chat users</label>
<textarea id="cfgChatUsers" placeholder="one entry per line"></textarea>
</div>
<div class="field">
<label for="cfgChatRooms">Chat rooms</label>
<textarea id="cfgChatRooms" placeholder="one entry per line"></textarea>
</div>
<div class="field">
<label for="cfgDiscordUsers">Discord users</label>
<textarea id="cfgDiscordUsers" placeholder="one entry per line"></textarea>
</div>
<div class="field">
<label for="cfgDiscordChannels">Discord channels</label>
<textarea id="cfgDiscordChannels" placeholder="one entry per line"></textarea>
</div>
<div class="field full">
<label for="cfgDiscordGuilds">Discord guilds</label>
<textarea id="cfgDiscordGuilds" placeholder="one entry per line"></textarea>
</div>
</div>

<details>
<summary>Raw JSON preview</summary>
<pre id="cfgRawPreview"></pre>
</details>
</div>

<div class="admin-panel" id="secrets-panel">
<div class="helper-box">
<div class="helper-label">Provider key helper</div>
<div class="field full">
<label for="providerSecretName">Recommended key name</label>
<input id="providerSecretName" type="text" readonly/>
</div>
<div class="button-row">
<input id="providerSecretValue" type="password" placeholder="provider API key" style="flex:1;min-width:230px;background:#0b1020;color:#e8eefc;border:1px solid #333;padding:8px;border-radius:4px"/>
<button onclick="storeProviderSecret()">Store provider key</button>
</div>
</div>

<div class="helper-box">
<div class="helper-label">Discord helper</div>
<div class="button-row">
<input id="discordTokenValue" type="password" placeholder="discord bot token" style="flex:1;min-width:230px;background:#0b1020;color:#e8eefc;border:1px solid #333;padding:8px;border-radius:4px"/>
<button onclick="storeDiscordToken()">Store discord bot token</button>
</div>
</div>

<div class="button-row">
<input id="sname" placeholder="provider/zai/api_key" style="width:300px;background:#0b1020;color:#e8eefc;border:1px solid #333;padding:8px;border-radius:4px"/>
<input id="svalue" placeholder="secret value" type="password" style="width:300px;background:#0b1020;color:#e8eefc;border:1px solid #333;padding:8px;border-radius:4px"/>
<button onclick="setSecret()">Store Secret</button>
<button class="secondary" onclick="listSecrets()">List Keys</button>
</div>
<pre id="secrets"></pre>
</div>
</div>
</div>

<script>
let chatMessages=[];
let currentConfig=null;

function byId(id){return document.getElementById(id);}

function showTab(evt,tab){
document.querySelectorAll('.admin-tab').forEach(function(t){t.classList.remove('active');});
document.querySelectorAll('.admin-panel').forEach(function(p){p.classList.remove('active');});
evt.target.classList.add('active');
byId(tab+'-panel').classList.add('active');
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

async function j(url,opts){
opts=opts||{};
const t=getToken();
if(!t){alert('Bearer token required');return {ok:false,error:'missing token'};}
opts.headers=Object.assign({'Authorization':'Bearer '+t,'Content-Type':'application/json'},opts.headers||{});
const r=await fetch(url,opts);
const txt=await r.text();
let parsed;
try{parsed=JSON.parse(txt);}catch(e){parsed={raw:txt};}
if(!r.ok){
if(!parsed.error){parsed.error=parsed.raw||txt||('request failed with status '+r.status);}
parsed.ok=false;
parsed.status=r.status;
return parsed;
}
if(parsed&&typeof parsed==='object'&&typeof parsed.ok==='undefined')parsed.ok=true;
return parsed;
}

function setCfgMessage(message,type){
const el=byId('cfgMessage');
el.textContent=message||'';
el.className='msg'+(type?' '+type:'');
}

function listToText(items){
if(!Array.isArray(items)||items.length===0)return '';
return items.join('\n');
}

function textToList(value){
if(!value)return [];
return value.split(/\n|,/).map(function(v){return v.trim();}).filter(function(v){return v.length>0;});
}

function providerSecretKey(provider){
return 'provider/'+provider+'/api_key';
}

function updateProviderSecretName(){
const provider=byId('cfgProvider').value||'zai';
byId('providerSecretName').value=providerSecretKey(provider);
}

function onProviderChange(){
const provider=byId('cfgProvider').value;
byId('genericBaseRow').style.display=provider==='generic'?'flex':'none';
updateProviderSecretName();
updateRawPreview();
}

function ensureConfigShape(cfg){
if(!cfg.model)cfg.model={};
if(!cfg.providers)cfg.providers={};
if(!cfg.providers.generic)cfg.providers.generic={};
if(!cfg.chat)cfg.chat={};
if(!cfg.discord)cfg.discord={};
if(!cfg.sandbox)cfg.sandbox={};
if(!cfg.shell)cfg.shell={};
}

function populateForm(cfg){
ensureConfigShape(cfg);
byId('cfgProvider').value=cfg.model.provider||'zai';
byId('cfgModelName').value=cfg.model.name||'';
byId('cfgTemperature').value=(typeof cfg.model.temperature==='number')?String(cfg.model.temperature):'';
byId('cfgGenericBaseURL').value=(cfg.providers.generic&&cfg.providers.generic.base_url)||'';
byId('cfgChatEnabled').checked=!!cfg.chat.enabled;
byId('cfgDiscordEnabled').checked=!!cfg.discord.enabled;
byId('cfgSandboxActive').checked=!!cfg.sandbox.active;
byId('cfgShellExecEnabled').checked=!!cfg.shell.enable_exec;
byId('cfgChatUsers').value=listToText(cfg.chat.allow_users);
byId('cfgChatRooms').value=listToText(cfg.chat.allow_rooms);
byId('cfgDiscordUsers').value=listToText(cfg.discord.allow_users);
byId('cfgDiscordChannels').value=listToText(cfg.discord.allow_channels);
byId('cfgDiscordGuilds').value=listToText(cfg.discord.allow_guilds);
onProviderChange();
}

function cloneConfig(cfg){
return JSON.parse(JSON.stringify(cfg||{}));
}

function collectConfigFromForm(){
const cfg=cloneConfig(currentConfig);
ensureConfigShape(cfg);
const provider=byId('cfgProvider').value.trim().toLowerCase();
const modelName=byId('cfgModelName').value.trim();
const tempRaw=byId('cfgTemperature').value.trim();
cfg.model.provider=provider;
cfg.model.name=modelName;
if(tempRaw===''){
delete cfg.model.temperature;
}else{
cfg.model.temperature=Number(tempRaw);
}
cfg.chat.enabled=byId('cfgChatEnabled').checked;
cfg.discord.enabled=byId('cfgDiscordEnabled').checked;
cfg.sandbox.active=byId('cfgSandboxActive').checked;
cfg.shell.enable_exec=byId('cfgShellExecEnabled').checked;
cfg.chat.allow_users=textToList(byId('cfgChatUsers').value);
cfg.chat.allow_rooms=textToList(byId('cfgChatRooms').value);
cfg.discord.allow_users=textToList(byId('cfgDiscordUsers').value);
cfg.discord.allow_channels=textToList(byId('cfgDiscordChannels').value);
cfg.discord.allow_guilds=textToList(byId('cfgDiscordGuilds').value);
if(provider==='generic'){
cfg.providers.generic.base_url=byId('cfgGenericBaseURL').value.trim();
}
return cfg;
}

function validateConfigForm(){
const provider=byId('cfgProvider').value.trim();
const modelName=byId('cfgModelName').value.trim();
const tempRaw=byId('cfgTemperature').value.trim();
if(!provider)return 'provider is required';
if(!modelName)return 'model name is required';
if(tempRaw!==''&&Number.isNaN(Number(tempRaw)))return 'temperature must be numeric';
if(provider==='generic'&&!byId('cfgGenericBaseURL').value.trim())return 'generic base_url is required';
return '';
}

function updateRawPreview(){
try{byId('cfgRawPreview').textContent=JSON.stringify(collectConfigFromForm(),null,2);}catch(e){byId('cfgRawPreview').textContent='';}
}

async function loadStatus(){
const data=await j('/api/admin/status');
if(data.model){byId('modelInfo').textContent=data.model.provider+'/'+data.model.name;}
let html='Runs: '+(data.run_count||0);
if(data.runs&&data.runs.length>0){
html+='<br><br>Recent:<br>';
data.runs.slice(0,5).forEach(function(run){
const status=run.status==='completed'?'✓':run.status==='failed'?'✗':'⏳';
html+=status+' '+run.id+' ('+run.source+')<br>';
});
}
byId('status').innerHTML=html;
}

async function loadConfig(){
setCfgMessage('','');
const cfg=await j('/api/admin/config');
if(!cfg.ok){
setCfgMessage('Failed to load config: '+(cfg.error||'unknown error'),'error');
return;
}
const cleanCfg=cloneConfig(cfg);
delete cleanCfg.ok;
delete cleanCfg.status;
delete cleanCfg.error;
delete cleanCfg.raw;
currentConfig=cleanCfg;
populateForm(currentConfig);
updateRawPreview();
}

async function saveConfig(){
const validationError=validateConfigForm();
if(validationError){
setCfgMessage(validationError,'error');
return;
}
const nextCfg=collectConfigFromForm();
const result=await j('/api/admin/config',{method:'POST',body:JSON.stringify(nextCfg)});
if(result.ok){
setCfgMessage('Config saved.','success');
await loadConfig();
await loadStatus();
return;
}
setCfgMessage('Save failed: '+(result.error||JSON.stringify(result)),'error');
}

async function storeSecret(name,value){
if(!name||!value)return {ok:false,error:'name and value are required'};
return j('/api/admin/secrets',{method:'POST',body:JSON.stringify({name:name,value:value})});
}

async function setSecret(){
const name=byId('sname').value.trim();
const value=byId('svalue').value;
if(!name||!value){alert('Name and value required');return;}
const result=await storeSecret(name,value);
byId('secrets').textContent=JSON.stringify(result,null,2);
if(result.ok){
byId('svalue').value='';
}
}

async function storeProviderSecret(){
const name=byId('providerSecretName').value.trim();
const value=byId('providerSecretValue').value;
if(!value){alert('Provider key value required');return;}
const result=await storeSecret(name,value);
byId('secrets').textContent=JSON.stringify(result,null,2);
if(result.ok)byId('providerSecretValue').value='';
}

async function storeDiscordToken(){
const value=byId('discordTokenValue').value;
if(!value){alert('Discord token value required');return;}
const result=await storeSecret('discord/bot_token',value);
byId('secrets').textContent=JSON.stringify(result,null,2);
if(result.ok)byId('discordTokenValue').value='';
}

async function listSecrets(){
byId('secrets').textContent=JSON.stringify(await j('/api/admin/secrets'),null,2);
}

function formatContent(text){
if(!text)return'';
text=text.replace(/\u0026/g,'\u0026amp;').replace(/\u003c/g,'\u0026lt;').replace(/\u003e/g,'\u0026gt;');
text=text.replace(/\u0060\u0060\u0060(\w+)?\n([\s\S]*?)\u0060\u0060\u0060/g,'\u003cpre\u003e\u003ccode\u003e$2\u003c/code\u003e\u003c/pre\u003e');
text=text.replace(/\u0060([^\u0060]+)\u0060/g,'\u003ccode\u003e$1\u003c/code\u003e');
text=text.replace(/\n/g,'\u003cbr\u003e');
return text;
}

function renderChat(){
const container=byId('chatHistory');
container.innerHTML=chatMessages.map(function(m){
const roleClass=m.role==='user'?'chat-user':'chat-assistant';
const roleLabel=m.role==='user'?'You':'Bot';
const content=formatContent(m.content);
return'<div class="chat-message '+roleClass+'"><strong>'+roleLabel+':</strong><br>'+content+'</div>';
}).join('');
container.scrollTop=container.scrollHeight;
}

async function pollRun(runId){
for(let i=0;i<60;i++){
await new Promise(function(resolve){setTimeout(resolve,1000);});
const run=await j('/v1/runs/'+runId);
if(run.status==='completed'&&run.output)return run.output;
if(run.status==='failed')return'Error: '+(run.error||'Run failed');
}
return'Timeout waiting for response';
}

async function sendChat(){
await sendChatWithText('');
}

async function startNewChat(){
await sendChatWithText('/new');
}

async function sendChatWithText(forcedMessage){
const input=byId('chatMessage');
const message=(forcedMessage&&forcedMessage.trim())?forcedMessage.trim():input.value.trim();
if(!message)return;
chatMessages.push({role:'user',content:message});
renderChat();
input.value='';
const thinkingIdx=chatMessages.length;
chatMessages.push({role:'assistant',content:'Thinking...'});
renderChat();
try{
const result=await j('/v1/chat/messages',{method:'POST',body:JSON.stringify({user_id:'dashboard_user',message:message,agent_id:'default'})});
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

function wireFormPreviewUpdates(){
['cfgProvider','cfgModelName','cfgTemperature','cfgGenericBaseURL','cfgChatEnabled','cfgDiscordEnabled','cfgSandboxActive','cfgShellExecEnabled','cfgChatUsers','cfgChatRooms','cfgDiscordUsers','cfgDiscordChannels','cfgDiscordGuilds'].forEach(function(id){
const el=byId(id);
if(!el)return;
el.addEventListener('input',updateRawPreview);
el.addEventListener('change',updateRawPreview);
});
}

wireFormPreviewUpdates();
loadStatus();
loadConfig();
setInterval(loadStatus,30000);
</script></body></html>`
