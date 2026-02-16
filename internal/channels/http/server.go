package httpchannel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const defaultAddr = "127.0.0.1:8080"

type Config struct {
	Addr        string
	BearerToken string
	Store       RunStore
	Executor    RunExecutor
	Chat        ChatConnector
	RegisterMux func(mux *http.ServeMux)
}

type Server struct {
	addr        string
	bearerToken string
	store       RunStore
	executor    RunExecutor
	chat        ChatConnector
	httpServer  *http.Server
}

type ChatMessage struct {
	UserID  string `json:"user_id"`
	RoomID  string `json:"room_id"`
	AgentID string `json:"agent_id,omitempty"`
	Message string `json:"message"`
}

type ChatConnector interface {
	HandleMessage(ctx context.Context, msg ChatMessage) (Run, error)
}

type RunExecutor interface {
	Execute(ctx context.Context, agentID, message string) (ExecutionResult, error)
}

type NopExecutor struct{}

func (NopExecutor) Execute(_ context.Context, _ string, _ string) (ExecutionResult, error) {
	return ExecutionResult{Output: "queued"}, nil
}

type postRunRequest struct {
	AgentID string `json:"agent_id"`
	Message string `json:"message"`
}

type postRunResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func NewServer(cfg Config) *Server {
	addr := cfg.Addr
	if addr == "" {
		addr = defaultAddr
	}
	store := cfg.Store
	if store == nil {
		store = NewInMemoryRunStore()
	}
	executor := cfg.Executor
	if executor == nil {
		executor = NopExecutor{}
	}

	s := &Server{
		addr:        addr,
		bearerToken: cfg.BearerToken,
		store:       store,
		executor:    executor,
		chat:        cfg.Chat,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/runs", s.handleRuns)
	mux.HandleFunc("/v1/runs/", s.handleRunByID)
	mux.HandleFunc("/v1/chat/messages", s.handleChatMessage)
	if cfg.RegisterMux != nil {
		cfg.RegisterMux(mux)
	}

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.authMiddleware(mux),
	}

	return s
}

func (s *Server) handleChatMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.chat == nil {
		http.Error(w, "chat connector is disabled", http.StatusNotFound)
		return
	}

	var req ChatMessage
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if req.UserID == "" || req.Message == "" {
		http.Error(w, "user_id and message are required", http.StatusBadRequest)
		return
	}

	run, err := s.chat.HandleMessage(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(postRunResponse{ID: run.ID, Status: run.Status})
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	if strings.TrimSpace(s.bearerToken) == "" {
		return errors.New("bearer token is required")
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpServer.ListenAndServe()
	}()
	return s.wait(ctx, errCh)
}

func (s *Server) ListenAndServeTLS(ctx context.Context, certFile, keyFile string) error {
	if strings.TrimSpace(s.bearerToken) == "" {
		return errors.New("bearer token is required")
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpServer.ListenAndServeTLS(certFile, keyFile)
	}()
	return s.wait(ctx, errCh)
}

func (s *Server) wait(ctx context.Context, errCh <-chan error) error {

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

func (s *Server) Addr() string {
	return s.addr
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req postRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if req.AgentID == "" || req.Message == "" {
		http.Error(w, "agent_id and message are required", http.StatusBadRequest)
		return
	}

	created, err := QueueRun(r.Context(), s.store, s.executor, req.AgentID, req.Message, "http")
	if err != nil {
		http.Error(w, "failed to queue run", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(postRunResponse{ID: created.ID, Status: created.Status})
}

func (s *Server) handleRunByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/v1/runs/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "run id is required", http.StatusBadRequest)
		return
	}

	run, err := s.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrRunNotFound) {
			http.Error(w, "run not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load run", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(run)
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow dashboard HTML to load without auth (token will be provided via URL param or prompt)
		if r.URL.Path == "/dashboard" && r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}

		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			http.Error(w, "invalid authorization scheme", http.StatusUnauthorized)
			return
		}

		token := strings.TrimSpace(parts[1])
		if token == "" || token != s.bearerToken {
			http.Error(w, "invalid bearer token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func newRunID() string {
	return fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
}
