// Package api implements the "Go HTTP API" component: JSON command and query
// endpoints, the uniform rejection envelope, health checks and the recovery
// readiness report. It exposes the domain through the service layer without
// shipping a frontend.
package api

import (
	"encoding/json"
	"net/http"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/service"
)

// ErrorEnvelope is the uniform rejection body returned for every failure.
type ErrorEnvelope struct {
	Code           string   `json:"code"`
	Message        string   `json:"message"`
	OrderedReasons []string `json:"ordered_reasons"`
	OperationID    string   `json:"operation_id"`
	CurrentVersion int64    `json:"current_version"`
}

// Server exposes the HTTP surface, the service and readiness state.
type Server struct {
	svc   *service.Service
	ready func() bool
}

// NewServer builds a server over the given service. When ready is nil the
// server reports ready once constructed.
func NewServer(svc *service.Service, ready func() bool) *Server {
	if ready == nil {
		ready = func() bool { return true }
	}
	return &Server{svc: svc, ready: ready}
}

// Handler returns the routed HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", s.handleLive)
	mux.HandleFunc("GET /health/ready", s.handleReady)
	mux.HandleFunc("POST /v1/tasks", s.handleCreateTask)
	mux.HandleFunc("POST /v1/tasks/{id}/lock", s.handleLock)
	mux.HandleFunc("POST /v1/tasks/{id}/commands", s.handleCommand)
	mux.HandleFunc("POST /v1/tasks/{id}/defects/evaluate", s.handleEvaluateDefects)
	mux.HandleFunc("POST /v1/tasks/{id}/recoat-generations", s.handleRecoatGenerations)
	mux.HandleFunc("POST /v1/tasks/{id}/reviews", s.handleReview)
	mux.HandleFunc("POST /v1/tasks/{id}/terminal-decisions", s.handleTerminal)
	mux.HandleFunc("GET /v1/tasks/{id}", s.handleGetTask)
	mux.HandleFunc("GET /v1/tasks/{id}/evidence", s.handleEvidence)
	mux.HandleFunc("GET /v1/tasks/{id}/material-balance", s.handleMaterialBalance)
	mux.HandleFunc("GET /v1/tasks/{id}/recoat-scope", s.handleRecoatScope)
	return mux
}

func (s *Server) handleLive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if s.ready() {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
		return
	}
	writeError(w, http.StatusServiceUnavailable, errs.New(errs.CodeInvalidInput, "not ready"))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, e error) {
	env := ErrorEnvelope{Code: string(errs.CodeInvalidInput), Message: e.Error()}
	if de, ok := e.(*errs.Error); ok {
		env.Code = string(de.Code)
		env.Message = de.Message
		env.OrderedReasons = de.Reasons
		env.OperationID = de.OperationID
		env.CurrentVersion = de.CurrentVersion
	}
	writeJSON(w, status, env)
}

// statusFor maps a domain error to an HTTP status code.
func statusFor(e error) int {
	if de, ok := e.(*errs.Error); ok {
		switch de.Code {
		case errs.CodeNotFound:
			return http.StatusNotFound
		case errs.CodeGenerationStale, errs.CodeGenerationConflict,
			errs.CodeIdempotencyConflict, errs.CodeTerminalAlreadyDecided:
			return http.StatusConflict
		case errs.CodeLeaseBusy, errs.CodeMaterialInsufficient:
			return http.StatusConflict
		default:
			return http.StatusBadRequest
		}
	}
	return http.StatusInternalServerError
}

func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
