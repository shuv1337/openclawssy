package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openclawssy/internal/channels/chat"
	"openclawssy/internal/channels/cli"
	"openclawssy/internal/channels/dashboard"
	"openclawssy/internal/channels/discord"
	httpchannel "openclawssy/internal/channels/http"
	"openclawssy/internal/chatstore"
	"openclawssy/internal/config"
	"openclawssy/internal/runtime"
	"openclawssy/internal/scheduler"
	"openclawssy/internal/secrets"
)

func main() {
	ctx := context.Background()
	engine, err := runtime.NewEngine(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	handlers := cli.Handlers{Init: initService{engine: engine}, Ask: askService{engine: engine}, Run: runService{engine: engine}, Doctor: doctorService{}, Cron: cronService{}, Out: os.Stdout, Err: os.Stderr}

	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	var code int
	switch os.Args[1] {
	case "init":
		code = handlers.HandleInit(ctx, os.Args[2:])
	case "setup":
		code = handleSetup(os.Args[2:])
	case "ask":
		code = handlers.HandleAsk(ctx, os.Args[2:])
	case "run":
		code = handlers.HandleRun(ctx, os.Args[2:])
	case "doctor":
		code = handlers.HandleDoctor(ctx, os.Args[2:])
	case "cron":
		code = handlers.HandleCron(ctx, os.Args[2:])
	case "serve":
		code = handleServe(ctx, engine, os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", os.Args[1])
		printUsage(os.Stderr)
		code = 2
	}

	os.Exit(code)
}

func printUsage(w *os.File) {
	fmt.Fprintln(w, "usage: openclawssy <subcommand> [flags]")
	fmt.Fprintln(w, "subcommands: init, setup, ask, run, serve, cron, doctor")
}

func handleServe(ctx context.Context, engine *runtime.Engine, args []string) int {
	serveCfg, err := cli.ParseServeArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	runStore, err := httpchannel.NewFileRunStore(serveCfg.RunsFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	exec := runtimeExecutor{engine: engine}
	runtimeCfg, err := config.LoadOrDefault(filepath.Join(".openclawssy", "config.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if secretStore, serr := secrets.NewStore(runtimeCfg); serr == nil {
		if token, ok, _ := secretStore.Get("discord/bot_token"); ok && strings.TrimSpace(token) != "" {
			runtimeCfg.Discord.Token = token
		}
	}

	jobsStore, err := scheduler.NewStore(serveCfg.JobsFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	schedulerExec := scheduler.NewExecutor(jobsStore, time.Second, func(agentID string, message string) {
		_, _ = httpchannel.QueueRun(context.Background(), runStore, exec, agentID, message, "scheduler")
	})
	schedulerExec.Start()
	defer schedulerExec.Stop()

	sharedChat, err := buildSharedChatConnector(runtimeCfg, runStore, exec)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	var dBot *discord.Bot
	if runtimeCfg.Discord.Enabled {
		dBot, err = discord.New(
			runtimeCfg,
			buildDiscordMessageHandler(sharedChat, runtimeCfg.Discord.DefaultAgentID),
			func(ctx context.Context, runID string) (discord.RunStatus, error) {
				run, err := runStore.Get(ctx, runID)
				if err != nil {
					return discord.RunStatus{}, err
				}
				return discord.RunStatus{Status: run.Status, Output: run.Output, Error: run.Error, ArtifactPath: run.ArtifactPath}, nil
			},
		)
		if err != nil {
			fmt.Fprintln(os.Stderr, "discord disabled:", err)
		} else if err := dBot.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "discord start failed:", err)
		} else {
			defer dBot.Stop()
		}
	}

	dash := dashboard.New(".", runStore)
	server := httpchannel.NewServer(httpchannel.Config{
		Addr:        serveCfg.Addr,
		BearerToken: serveCfg.Token,
		Store:       runStore,
		Executor:    exec,
		Chat:        buildDashboardChatConnector(runtimeCfg, sharedChat),
		RegisterMux: func(mux *http.ServeMux) {
			if runtimeCfg.Server.Dashboard {
				dash.Register(mux)
			}
		},
	})

	if runtimeCfg.Server.TLSEnabled {
		if err := server.ListenAndServeTLS(ctx, runtimeCfg.Server.TLSCertFile, runtimeCfg.Server.TLSKeyFile); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}

	if err := server.ListenAndServe(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}

type initService struct{ engine *runtime.Engine }

func (s initService) Init(_ context.Context, input cli.InitInput) error {
	if s.engine == nil {
		return errors.New("runtime engine is not configured")
	}
	eng := s.engine
	if input.Workspace != "" && input.Workspace != "." {
		custom, err := runtime.NewEngine(input.Workspace)
		if err != nil {
			return err
		}
		eng = custom
	}
	if err := eng.Init(input.AgentID, input.Force); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "initialized agent=%q workspace=%q force=%t\n", input.AgentID, input.Workspace, input.Force)
	return nil
}

type askService struct{ engine *runtime.Engine }

func (s askService) Ask(ctx context.Context, input cli.AskInput) (string, error) {
	if s.engine == nil {
		return "", errors.New("runtime engine is not configured")
	}
	res, err := s.engine.Execute(ctx, input.AgentID, input.Message)
	if err != nil {
		return "", err
	}
	return res.FinalText, nil
}

type runService struct{ engine *runtime.Engine }

func (s runService) Run(ctx context.Context, input cli.RunInput) (string, error) {
	if s.engine == nil {
		return "", errors.New("runtime engine is not configured")
	}
	message := input.Message
	if message == "" && input.MessageFile != "" {
		b, err := os.ReadFile(input.MessageFile)
		if err != nil {
			return "", err
		}
		message = strings.TrimSpace(string(b))
	}
	if strings.TrimSpace(message) == "" {
		return "", errors.New("message is empty")
	}
	res, err := s.engine.Execute(ctx, input.AgentID, message)
	if err != nil {
		return "", err
	}
	if input.Detached {
		return fmt.Sprintf("run %s accepted", res.RunID), nil
	}
	return fmt.Sprintf("run %s completed\nartifacts: %s\n%s", res.RunID, res.ArtifactPath, res.FinalText), nil
}

type doctorService struct{}

func (doctorService) Doctor(_ context.Context, input cli.DoctorInput) (string, error) {
	workspace := "workspace"
	_, wsErr := os.Stat(workspace)
	state := "missing"
	if wsErr == nil {
		state = "ok"
	}

	cfg, cfgErr := config.LoadOrDefault(filepath.Join(".openclawssy", "config.json"))
	providerState := "not configured"
	secretState := "missing"
	if cfgErr == nil {
		endpoint, err := providerForDoctor(cfg)
		if err == nil {
			apiKey := endpoint.APIKey
			if apiKey == "" && endpoint.APIKeyEnv != "" {
				apiKey = os.Getenv(endpoint.APIKeyEnv)
			}
			if apiKey != "" {
				providerState = fmt.Sprintf("%s/%s key=env", cfg.Model.Provider, cfg.Model.Name)
			} else {
				store, serr := secrets.NewStore(cfg)
				if serr == nil {
					if v, ok, _ := store.Get("provider/" + strings.ToLower(cfg.Model.Provider) + "/api_key"); ok && strings.TrimSpace(v) != "" {
						providerState = fmt.Sprintf("%s/%s key=secret-store", cfg.Model.Provider, cfg.Model.Name)
						secretState = "ok"
					} else {
						providerState = fmt.Sprintf("%s/%s key=missing (%s)", cfg.Model.Provider, cfg.Model.Name, endpoint.APIKeyEnv)
					}
				}
			}
		}
	}

	if input.Verbose {
		setup := []string{
			"1) openclawssy setup",
			"2) export OPENCLAWSSY_MASTER_KEY if not using local master key file",
			"3) store provider key via dashboard or wizard",
			"4) run `openclawssy serve --token <token>` and open https dashboard",
		}
		if cfgErr != nil {
			return fmt.Sprintf("doctor: workspace=%s (%s) model=%s secrets=%s\nsetup:\n- %s", workspace, state, providerState, secretState, strings.Join(setup, "\n- ")), nil
		}
		return fmt.Sprintf("doctor: workspace=%s (%s) model=%s secrets=%s", workspace, state, providerState, secretState), nil
	}
	return "doctor: ok", nil
}

type cronService struct{}

func (cronService) Cron(_ context.Context, input cli.CronInput) (string, error) {
	store, err := scheduler.NewStore(filepath.Join(".openclawssy", "scheduler", "jobs.json"))
	if err != nil {
		return "", err
	}

	switch strings.ToLower(strings.TrimSpace(input.Command)) {
	case "list":
		jobs := store.List()
		if len(jobs) == 0 {
			return "no jobs", nil
		}
		lines := make([]string, 0, len(jobs))
		for _, job := range jobs {
			lines = append(lines, fmt.Sprintf("%s %s %q enabled=%t", job.ID, job.Schedule, job.Message, job.Enabled))
		}
		return strings.Join(lines, "\n"), nil
	case "add":
		fs := flag.NewFlagSet("cron add", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		id := ""
		agentID := "default"
		schedule := ""
		message := ""
		mode := "isolated"
		notify := ""
		enabled := true
		fs.StringVar(&id, "id", "", "job id")
		fs.StringVar(&agentID, "agent", "default", "agent id")
		fs.StringVar(&schedule, "schedule", "", "schedule (@every 1m or RFC3339)")
		fs.StringVar(&message, "message", "", "message")
		fs.StringVar(&mode, "mode", "isolated", "execution mode")
		fs.StringVar(&notify, "notify-target", "", "notify target")
		fs.BoolVar(&enabled, "enabled", true, "enable job")
		if err := fs.Parse(input.Args); err != nil {
			return "", err
		}
		if schedule == "" || message == "" {
			return "", errors.New("-schedule and -message are required")
		}
		if id == "" {
			id = fmt.Sprintf("job_%d", time.Now().UTC().UnixNano())
		}
		if err := store.Add(scheduler.Job{ID: id, Schedule: schedule, AgentID: agentID, Message: message, Mode: mode, NotifyTarget: notify, Enabled: enabled}); err != nil {
			return "", err
		}
		return "added job " + id, nil
	case "remove":
		fs := flag.NewFlagSet("cron remove", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		id := ""
		fs.StringVar(&id, "id", "", "job id")
		if err := fs.Parse(input.Args); err != nil {
			return "", err
		}
		if id == "" {
			return "", errors.New("-id is required")
		}
		if err := store.Remove(id); err != nil {
			return "", err
		}
		return "removed job " + id, nil
	default:
		return "", fmt.Errorf("unsupported cron command: %s", input.Command)
	}
}

type runtimeExecutor struct{ engine *runtime.Engine }

func (e runtimeExecutor) Execute(ctx context.Context, agentID, message string) (httpchannel.ExecutionResult, error) {
	res, err := e.engine.Execute(ctx, agentID, message)
	if err != nil {
		return httpchannel.ExecutionResult{}, err
	}
	return httpchannel.ExecutionResult{Output: res.FinalText, ArtifactPath: res.ArtifactPath, DurationMS: res.DurationMS, ToolCalls: res.ToolCalls, Provider: res.Provider, Model: res.Model}, nil
}

func buildSharedChatConnector(cfg config.Config, store httpchannel.RunStore, exec httpchannel.RunExecutor) (*chat.Connector, error) {
	if !cfg.Chat.Enabled && !cfg.Discord.Enabled {
		return nil, nil
	}
	chatStore, err := chatstore.NewStore(filepath.Join(".openclawssy", "agents"))
	if err != nil {
		return nil, fmt.Errorf("create chat store: %w", err)
	}
	defaultAgentID := strings.TrimSpace(cfg.Chat.DefaultAgentID)
	if defaultAgentID == "" {
		defaultAgentID = strings.TrimSpace(cfg.Discord.DefaultAgentID)
	}
	if defaultAgentID == "" {
		defaultAgentID = "default"
	}
	return &chat.Connector{
		DefaultAgentID: defaultAgentID,
		Store:          chatStore,
		HistoryLimit:   30,
		Queue: func(ctx context.Context, agentID, message, source string) (chat.QueuedRun, error) {
			run, err := httpchannel.QueueRun(ctx, store, exec, agentID, message, source)
			if err != nil {
				return chat.QueuedRun{}, err
			}
			return chat.QueuedRun{ID: run.ID, Status: run.Status}, nil
		},
	}, nil
}

func buildDashboardChatConnector(cfg config.Config, connector *chat.Connector) httpchannel.ChatConnector {
	if !cfg.Chat.Enabled || connector == nil {
		return nil
	}
	return scopedChatAdapter{
		connector:      connector,
		source:         "dashboard",
		defaultAgentID: cfg.Chat.DefaultAgentID,
		allow:          chat.NewAllowlist(cfg.Chat.AllowUsers, cfg.Chat.AllowRooms),
		limiter:        chat.NewRateLimiter(cfg.Chat.RateLimitPerMin, time.Minute),
	}
}

func buildDiscordMessageHandler(connector *chat.Connector, defaultAgentID string) discord.MessageHandler {
	return func(ctx context.Context, msg discord.Message) (discord.Response, error) {
		if connector == nil {
			return discord.Response{}, errors.New("chat connector is disabled")
		}
		agentID := strings.TrimSpace(msg.AgentID)
		if agentID == "" {
			agentID = strings.TrimSpace(defaultAgentID)
		}
		if agentID == "" {
			agentID = "default"
		}
		queued, err := connector.HandleMessage(ctx, chat.Message{UserID: msg.UserID, RoomID: msg.RoomID, AgentID: agentID, Source: "discord", Text: msg.Text})
		if err != nil {
			return discord.Response{}, err
		}
		return discord.Response{ID: queued.ID, Status: queued.Status, Response: queued.Response}, nil
	}
}

type scopedChatAdapter struct {
	connector      *chat.Connector
	source         string
	defaultAgentID string
	allow          *chat.Allowlist
	limiter        *chat.RateLimiter
}

func (a scopedChatAdapter) HandleMessage(ctx context.Context, msg httpchannel.ChatMessage) (httpchannel.ChatResponse, error) {
	if a.allow != nil && !a.allow.MessageAllowed(msg.UserID, msg.RoomID) {
		return httpchannel.ChatResponse{}, chat.ErrNotAllowlisted
	}
	if a.limiter != nil && !a.limiter.Allow(msg.UserID+":"+msg.RoomID) {
		return httpchannel.ChatResponse{}, chat.ErrRateLimited
	}
	agentID := strings.TrimSpace(msg.AgentID)
	if agentID == "" {
		agentID = strings.TrimSpace(a.defaultAgentID)
	}
	queued, err := a.connector.HandleMessage(ctx, chat.Message{UserID: msg.UserID, RoomID: msg.RoomID, AgentID: agentID, Source: a.source, Text: msg.Message})
	if err != nil {
		return httpchannel.ChatResponse{}, err
	}
	return httpchannel.ChatResponse{ID: queued.ID, Status: queued.Status, Response: queued.Response}, nil
}

func providerForDoctor(cfg config.Config) (config.ProviderEndpointConfig, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Model.Provider)) {
	case "openai":
		return cfg.Providers.OpenAI, nil
	case "openrouter":
		return cfg.Providers.OpenRouter, nil
	case "requesty":
		return cfg.Providers.Requesty, nil
	case "zai":
		return cfg.Providers.ZAI, nil
	case "generic":
		return cfg.Providers.Generic, nil
	default:
		return config.ProviderEndpointConfig{}, errors.New("unsupported provider")
	}
}

func handleSetup(args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	force := fs.Bool("force", false, "overwrite existing config")
	_ = fs.Parse(args)

	eng, err := runtime.NewEngine(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := eng.Init("default", *force); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	cfgPath := filepath.Join(".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	in := bufio.NewReader(os.Stdin)
	fmt.Println("Openclawssy guided setup (ZAI Coding Plan Edition)")
	fmt.Println("Default: ZAI provider with GLM-4.7 model")
	fmt.Println("Get your API key at: https://z.ai/subscribe")
	fmt.Println("Press Enter to accept defaults.")

	cfg.Model.Provider = prompt(in, "Provider (zai=GLM-4.7 Coding Plan)", cfg.Model.Provider)
	cfg.Model.Name = prompt(in, "Model name", cfg.Model.Name)

	apiKey := prompt(in, "Provider API key (stored encrypted; optional if env used)", "")

	tls := prompt(in, "Enable HTTPS dashboard? [y/N]", "N")
	if strings.EqualFold(tls, "y") {
		cfg.Server.TLSEnabled = true
		if err := ensureSelfSigned(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile); err != nil {
			fmt.Fprintln(os.Stderr, "warning: failed to create certs:", err)
		}
	}

	discordEnabled := prompt(in, "Enable Discord bot bridge? [y/N]", "N")
	if strings.EqualFold(discordEnabled, "y") {
		cfg.Discord.Enabled = true
		discordToken := prompt(in, "Discord bot token (stored encrypted; optional if env used)", "")
		if discordToken != "" {
			cfg.Discord.Token = ""
			if err := ensureMasterKey(cfg.Secrets.MasterKeyFile); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			store, err := secrets.NewStore(cfg)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			if err := store.Set("discord/bot_token", discordToken); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
	}

	if apiKey != "" {
		if err := ensureMasterKey(cfg.Secrets.MasterKeyFile); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		store, err := secrets.NewStore(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := store.Set("provider/"+strings.ToLower(cfg.Model.Provider)+"/api_key", apiKey); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	if err := config.Save(cfgPath, cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Println("Setup complete.")
	fmt.Println("Next:")
	fmt.Println("1) openclawssy doctor -v")
	fmt.Println("2) openclawssy serve --token <token>")
	if cfg.Server.TLSEnabled {
		fmt.Printf("3) open https://%s:%d/dashboard\n", cfg.Server.BindAddress, cfg.Server.Port)
	}
	return 0
}

func prompt(r *bufio.Reader, label, def string) string {
	fmt.Printf("%s [%s]: ", label, def)
	v, _ := r.ReadString('\n')
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	return v
}

func ensureMasterKey(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if _, err := secrets.GenerateAndWriteMasterKey(path); err != nil {
		return err
	}
	return nil
}

func ensureSelfSigned(certPath, keyPath string) error {
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			return nil
		}
	}
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "openclawssy.local"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  nil,
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return err
	}
	certOut, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return err
	}
	if err := os.Chmod(certPath, 0o600); err != nil {
		return err
	}
	keyOut, err := os.Create(keyPath)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}); err != nil {
		return err
	}
	return os.Chmod(keyPath, 0o600)
}
