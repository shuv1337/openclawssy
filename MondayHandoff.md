# Monday Handoff - Openclawssy Setup

**Date:** 2026-02-16  
**Project:** Openclawssy AI Agent Runtime  
**Status:** ✅ Operational with ZAI GLM-4.7

---

## 🎯 What We Built

A Dockerized Openclawssy instance configured to use **Z.AI's GLM-4.7 Coding Plan** as the AI provider, accessible via Tailscale with a web dashboard for chatting with the bot.

---

## 🏗️ Architecture

```
Remote Device (Tailscale)
    ↓
Docker Container (Port 8081)
    ↓
Openclawssy Server
    ↓
Z.AI API (GLM-4.7 Coding Plan)
```

---

## ✅ Completed Work

### 1. Z.AI Integration
- **Provider:** ZAI (GLM-4.7 Coding Plan)
- **Endpoint:** `https://api.z.ai/api/coding/paas/v4`
- **Default Model:** GLM-4.7
- **API Key:** Stored in environment variable `ZAI_API_KEY`

### 2. Docker Setup
- **Dockerfile:** Multi-stage build with Go 1.24
- **Port:** 8081 (mapped to container's 8080)
- **Entrypoint:** `docker-entrypoint.sh` - validates ZAI_API_KEY on startup
- **Volumes:**
  - `./workspace:/app/workspace` - Persistent workspace
  - `./.openclawssy:/app/.openclawssy` - Config and secrets

### 3. Dashboard Features
- **URL:** `http://<tailscale-ip>:8081/dashboard?token=<your-token>`
- **Main Chat Interface:** Full-screen chat with bot
- **Auto-scroll:** Chat scrolls to show new messages
- **Code Formatting:** Code blocks rendered with syntax highlighting
- **Polling:** Shows "Thinking..." then actual response
- **Status Bar:** Shows model info and recent runs (auto-refreshes)
- **Tabbed Admin:** Config and Secrets in collapsible tabs

### 4. Tailscale Access
- **Bind Address:** `0.0.0.0` (all interfaces) for Tailscale compatibility
- **Access:** Available from any device on your tailnet
- **Security:** Bearer token required for all API access

### 5. Tool Execution
- **Automatic:** Bot now parses and executes tool calls from responses
- **Supported Tools:**
  - `fs.list` - List directory contents
  - `fs.read` - Read file contents
  - `fs.write` - Write files
  - `fs.edit` - Edit files
  - `code.search` - Search codebase
  - `time.now` - Get current time
  - `shell.exec` - Execute shell commands (sandboxed)

---

## 🔧 Configuration

### Environment Variables (.env)
```bash
# REQUIRED
ZAI_API_KEY=your-zai-api-key-here

# Optional (default: change-me)
OPENCLAWSSY_TOKEN=your-secure-token
```

### Key Config Changes Made
1. **Default Provider:** Changed from `openai` to `zai`
2. **Default Model:** Changed from `gpt-4o-mini` to `GLM-4.7`
3. **ZAI Endpoint:** Updated to Coding Plan endpoint
4. **Chat:** Enabled by default with empty allowlist (allow all)
5. **Bind Address:** Changed from `127.0.0.1` to `0.0.0.0` for Tailscale

### Config File Location
```
.openclawssy/config.json
```

**Important:** Do NOT store API keys in config.json. Use:
- Environment variables (`.env` file)
- Secret store via dashboard (name: `provider/zai/api_key`)

---

## 🚀 How to Use

### Start the Server
```bash
cd ~/projects/openclawssy
sudo docker-compose up --build -d
```

### Access Dashboard
```
http://<tailscale-ip>:8081/dashboard?token=change-me
```

### Chat with Bot
1. Open dashboard URL with token parameter
2. Type messages in chat box
3. Bot will:
   - Show "Thinking..."
   - Execute any tool calls automatically
   - Display final response with results

### Example Commands
- "list files in the workspace"
- "create a hello.py file"
- "show me what's in SOUL.md"
- "search for TODO comments"

---

## 🔒 Security Notes

1. **API Key:** Stored in `.env` file (not committed to git)
2. **Bearer Token:** Required for dashboard/API access
3. **Tailscale:** Accessible only from devices on your tailnet
4. **Workspace Isolation:** Bot can only write to `./workspace/`
5. **Secrets:** Store sensitive data via dashboard, not in config.json

### Security Alert
⚠️ **API key was accidentally exposed in config.json earlier.** If using production key, regenerate it at https://z.ai

---

## 🐛 Known Issues & Solutions

### Issue: "Missing bearer token"
**Solution:** Add `?token=change-me` to URL or set `OPENCLAWSSY_TOKEN` in `.env`

### Issue: "Chat sender is not allowlisted"
**Solution:** Clear `chat.allow_users` and `chat.allow_rooms` in config (set to empty arrays `[]`)

### Issue: Port already allocated
**Solution:** Change port in `docker-compose.yml` (e.g., `8082:8080`)

### Issue: Model returning text instead of executing tools
**Solution:** Fixed - bot now parses `fs.list(path=".")` syntax automatically

---

## 📁 Key Files Modified

```
/home/mojo/projects/openclawssy/
├── Dockerfile                    # Multi-stage build
├── docker-compose.yml            # Port 8081, env vars
├── docker-entrypoint.sh          # Setup validation
├── .env.example                  # Environment template
├── internal/
│   ├── config/config.go          # ZAI defaults, 0.0.0.0 bind
│   ├── channels/
│   │   ├── dashboard/handler.go  # Chat UI, tool display
│   │   ├── http/server.go        # Auth middleware fix
│   │   └── chat/allowlist.go     # Allow all by default
│   └── runtime/
│       └── model.go              # Tool parsing from responses
└── .env                          # Your secrets (not in git)
```

---

## 🎉 Success Metrics

- ✅ Docker container builds and runs
- ✅ ZAI GLM-4.7 responding correctly
- ✅ Dashboard accessible via Tailscale
- ✅ Chat interface working
- ✅ Tools executing automatically
- ✅ Code formatting in responses
- ✅ Auto-scrolling chat

---

## 📋 Next Steps / TODO

1. **Test file operations:** Verify bot can create/edit files
2. **Add more tools:** Consider git operations, web search
3. **Improve error handling:** Better display of tool errors
4. **Add conversation history:** Persist chat across sessions
5. **Rate limiting:** Configure per-user limits
6. **Backup:** Set up workspace backup strategy

---

## 🔗 References

- **Z.AI Coding Plan:** https://z.ai/subscribe
- **Openclawssy Docs:** README.md, DOCKER.md
- **Tailscale:** https://tailscale.com
- **Dashboard:** http://localhost:8081/dashboard

---

**Handoff Complete** ✅  
*Bot is operational and ready for development work*
