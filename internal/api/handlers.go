package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/lease"
	"example.com/steel-fireproofing-evidence-closure/internal/service"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

// commandEnvelope is the unified command request body.
type commandEnvelope struct {
	ExpectedVersion int64           `json:"expected_version"`
	Generation      int64           `json:"generation"`
	LogicalTime     int64           `json:"logical_time"`
	CommandType     string          `json:"command_type"`
	Payload         json.RawMessage `json:"payload"`
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req service.LockRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, errs.New(errs.CodeInvalidInput, "unreadable body"))
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, errs.New(errs.CodeInvalidInput, "invalid request body"))
		return
	}
	if req.TaskID == "" {
		req.TaskID = newTaskID(req.Floor, req.FireCompartment)
	}
	res, err := s.svc.Lock(r.Context(), req)
	respond(w, res, err)
}

func (s *Server) handleLock(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	var req service.LockRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, errs.New(errs.CodeInvalidInput, "unreadable body"))
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, errs.New(errs.CodeInvalidInput, "invalid request body"))
		return
	}
	req.TaskID = taskID
	res, err := s.svc.Lock(r.Context(), req)
	respond(w, res, err)
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	opID := r.Header.Get("Idempotency-Key")
	if opID == "" {
		writeError(w, http.StatusBadRequest, errs.New(errs.CodeInvalidInput, "Idempotency-Key header is required"))
		return
	}
	caller := r.Header.Get("X-Caller-ID")
	if caller == "" {
		caller = "http"
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, errs.New(errs.CodeInvalidInput, "unreadable body"))
		return
	}
	var env commandEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		writeError(w, http.StatusBadRequest, errs.New(errs.CodeInvalidInput, "invalid command body"))
		return
	}

	gen := task.Generation(env.Generation)
	clock := lease.LogicalClock(env.LogicalTime)
	ctx := r.Context()

	var res any
	switch env.CommandType {
	case "surface_prep":
		var c service.SurfacePrepCmd
		if err := decodePayload(env.Payload, &c); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		res, err = s.svc.SurfacePrep(ctx, taskID, caller, opID, body, env.ExpectedVersion, gen, clock, c)
	case "material_open":
		var c service.MaterialOpenCmd
		if err := decodePayload(env.Payload, &c); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		res, err = s.svc.MaterialOpen(ctx, taskID, caller, opID, body, env.ExpectedVersion, gen, clock, c)
	case "material_issue":
		var c service.MaterialIssueCmd
		if err := decodePayload(env.Payload, &c); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		res, err = s.svc.MaterialIssue(ctx, taskID, caller, opID, body, env.ExpectedVersion, gen, clock, c)
	case "primer":
		var c service.PrimerCmd
		if err := decodePayload(env.Payload, &c); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		res, err = s.svc.Primer(ctx, taskID, caller, opID, body, env.ExpectedVersion, gen, clock, c)
	case "coat":
		var c service.CoatCmd
		if err := decodePayload(env.Payload, &c); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		res, err = s.svc.Coat(ctx, taskID, caller, opID, body, env.ExpectedVersion, gen, clock, c)
	case "curing":
		var c service.CuringCmd
		if err := decodePayload(env.Payload, &c); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		res, err = s.svc.Curing(ctx, taskID, caller, opID, body, env.ExpectedVersion, gen, clock, c)
	case "dry_film":
		var c service.DryFilmCmd
		if err := decodePayload(env.Payload, &c); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		res, err = s.svc.DryFilm(ctx, taskID, caller, opID, body, env.ExpectedVersion, gen, clock, c)
	case "bond":
		var c service.BondCmd
		if err := decodePayload(env.Payload, &c); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		res, err = s.svc.Bond(ctx, taskID, caller, opID, body, env.ExpectedVersion, gen, clock, c)
	case "device":
		var c service.DeviceCmd
		if err := decodePayload(env.Payload, &c); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		res, err = s.svc.InvokeDevice(ctx, taskID, caller, opID, body, env.ExpectedVersion, gen, clock, c)
	case "defect":
		var c service.DefectCmd
		if err := decodePayload(env.Payload, &c); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		res, err = s.svc.RecordDefect(ctx, taskID, caller, opID, body, env.ExpectedVersion, gen, clock, c)
	default:
		writeError(w, http.StatusBadRequest, errs.New(errs.CodeInvalidInput, "unknown command type"))
		return
	}
	respond(w, res, err)
}

func (s *Server) handleEvaluateDefects(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	gen := generationFromQuery(r)
	res, err := s.svc.EvaluateDefects(r.Context(), taskID, gen)
	respond(w, res, err)
}

func (s *Server) handleRecoatGenerations(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	var req struct {
		FromGeneration int64 `json:"from_generation"`
	}
	body, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(body, &req)
	res, err := s.svc.CreateRecoatGeneration(r.Context(), taskID, task.Generation(req.FromGeneration))
	respond(w, res, err)
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	opID := r.Header.Get("Idempotency-Key")
	caller := r.Header.Get("X-Caller-ID")
	if caller == "" {
		caller = "http"
	}
	body, _ := io.ReadAll(r.Body)
	var env commandEnvelope
	_ = json.Unmarshal(body, &env)
	var c service.ReviewCmd
	if err := decodePayload(env.Payload, &c); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.svc.SubmitReview(r.Context(), taskID, caller, opID, body, env.ExpectedVersion, task.Generation(env.Generation), lease.LogicalClock(env.LogicalTime), c)
	respond(w, res, err)
}

func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	var c service.TerminalCmd
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeError(w, http.StatusBadRequest, errs.New(errs.CodeInvalidInput, "invalid request body"))
		return
	}
	res, err := s.svc.SubmitTerminal(r.Context(), taskID, c)
	respond(w, res, err)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.GetTask(r.Context(), r.PathValue("id"))
	respond(w, res, err)
}

func (s *Server) handleEvidence(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	res, err := s.svc.Evidence(r.Context(), taskID, after, limit)
	respond(w, res, err)
}

func (s *Server) handleMaterialBalance(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.MaterialBalance(r.Context(), r.PathValue("id"))
	respond(w, res, err)
}

func (s *Server) handleRecoatScope(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	gen := generationFromQuery(r)
	res, err := s.svc.RecoatScope(r.Context(), taskID, gen)
	respond(w, res, err)
}

func decodePayload(raw json.RawMessage, dst any) error {
	if err := json.Unmarshal(raw, dst); err != nil {
		return errs.New(errs.CodeInvalidInput, "invalid command payload")
	}
	return nil
}

func generationFromQuery(r *http.Request) task.Generation {
	g, _ := strconv.ParseInt(r.URL.Query().Get("generation"), 10, 64)
	if g == 0 {
		g = 1
	}
	return task.Generation(g)
}

// newTaskID generates a unique task identifier for the create endpoint. The
// floor/compartment participate only as a human-readable prefix.
func newTaskID(floor, compartment string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "task-" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return "task-" + hex.EncodeToString(b)
}
