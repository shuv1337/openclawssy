# Docker Setup Guide

## Quick Start with ZAI Coding Plan

Openclawssy is now pre-configured to use **ZAI's GLM-4.7 Coding Plan** as the default provider.

### Prerequisites

1. Docker and Docker Compose installed
2. A Z.AI API key from https://z.ai/subscribe

### Setup

1. **Copy the environment template:**
   ```bash
   cp .env.example .env
   ```

2. **Edit `.env` and add your ZAI API key:**
   ```bash
   ZAI_API_KEY=your-actual-api-key-here
   OPENCLAWSSY_TOKEN=your-secure-token-here
   ```

3. **Build and run:**
   ```bash
   docker-compose up --build
   ```

4. **Access the dashboard:**
   - Local: http://localhost:8081/dashboard
   - Tailscale: http://<tailscale-ip>:8081/dashboard (from any device on your tailnet)
   - Enter your bearer token (from `.env` or default: `change-me`)
   - Start chatting with the bot!

### Features

- **Chat Interface**: Built-in chat UI in the dashboard
- **ZAI Integration**: Pre-configured for GLM-4.7 Coding Plan
- **Secure Setup**: API key validation on startup
- **Persistent Storage**: Configuration and workspace are saved locally

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ZAI_API_KEY` | Yes | - | Your Z.AI API key for GLM-4.7 |
| `OPENCLAWSSY_TOKEN` | No | `change-me` | Bearer token for API/dashboard access |
| `DISCORD_BOT_TOKEN` | No | - | Optional Discord bot integration |

### Manual Docker Run

```bash
docker build -t openclawssy .
docker run -p 8081:8080 \
  -e ZAI_API_KEY=your-key-here \
  -e OPENCLAWSSY_TOKEN=your-token \
  -v $(pwd)/workspace:/app/workspace \
  -v $(pwd)/.openclawssy:/app/.openclawssy \
  openclawssy
```

**Note**: The container exposes port 8080 internally. Map it to any available port on your host (e.g., 8081 to avoid conflicts).

### API Endpoints

- **Dashboard**: http://localhost:8081/dashboard
- **Chat API**: POST `/v1/chat/messages`
- **Run API**: POST `/v1/runs`
- **Admin API**: `/api/admin/*`

All endpoints require Bearer token authentication.

### Tailscale Access

Openclawssy is configured to be accessible over Tailscale for secure remote access:

1. **Ensure Tailscale is running** on your host machine
2. **Get your Tailscale IP**:
   ```bash
   tailscale ip -4
   # or
   tailscale status
   ```

3. **Access from any device on your tailnet**:
   - Dashboard: `http://<tailscale-ip>:8081/dashboard`
   - API: `http://<tailscale-ip>:8081/v1/...`

4. **Security considerations**:
   - The server binds to all interfaces (`0.0.0.0`) by default for Docker/Tailscale compatibility
   - Always use a strong bearer token (set via `OPENCLAWSSY_TOKEN`)
   - Consider enabling TLS if accessing over untrusted networks
   - The bearer token is required for all API access

**Note**: When using Tailscale, the service is accessible from any device on your tailnet, not just localhost. Ensure your bearer token is kept secure!

### Troubleshooting

**Container exits immediately:**
- Check that `ZAI_API_KEY` is set in your `.env` file
- Run `docker-compose logs` to see error messages

**Can't access dashboard:**
- Verify the container is running: `docker-compose ps`
- Check the token matches what you set in `.env`
- View logs: `docker-compose logs -f`

**API errors:**
- Verify your ZAI API key is valid at https://z.ai
- Check network connectivity: `docker-compose exec openclawssy ping api.z.ai`
