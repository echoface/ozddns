package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/echoface/ozddns/internal/app"
	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/storage"
)

func decode[T any](r *http.Request) (T, error) {
	var v T
	if r.Body == nil {
		return v, nil
	}
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(&v)
	if err != nil && err.Error() == "EOF" {
		return v, nil
	}
	return v, err
}

func statusFor(err error) int {
	if errors.Is(err, storage.ErrNotFound) {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

// ---- meta ----------------------------------------------------------------

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	b, err := s.App.Bootstrap(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) handleProviderTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.App.ProviderTypes())
}

func (s *Server) handleSourceTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.App.SourceTypes())
}

func (s *Server) handleInterfaces(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.App.Interfaces())
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.App.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleIP(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.App.CurrentIP(r.Context()))
}

func (s *Server) handleScheduler(w http.ResponseWriter, r *http.Request) {
	body, err := decode[struct {
		Running *bool `json:"running"`
	}](r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Running != nil {
		// Persist the pause flag so the choice survives a restart, then apply it.
		paused := !*body.Running
		if _, err := s.App.PatchSettings(r.Context(), app.SettingsInput{SchedulerPaused: &paused}); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"running": s.App.Running()})
}

// ---- providers -----------------------------------------------------------

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.ListProviders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleGetProvider(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.GetProvider(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	in, err := decode[app.ProviderInput](r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := s.App.CreateProvider(r.Context(), in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	in, err := decode[app.ProviderInput](r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := s.App.UpdateProvider(r.Context(), r.PathValue("id"), in)
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	if err := s.App.DeleteProvider(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleValidateProvider(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.ValidateProvider(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "provider": v})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": v})
}

func (s *Server) handleProviderZones(w http.ResponseWriter, r *http.Request) {
	zones, err := s.App.Zones(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, zones)
}

// ---- IP sources ----------------------------------------------------------

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.ListIPSources(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	in, err := decode[app.IPSourceInput](r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := s.App.CreateIPSource(r.Context(), in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) handleUpdateSource(w http.ResponseWriter, r *http.Request) {
	in, err := decode[app.IPSourceInput](r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := s.App.UpdateIPSource(r.Context(), r.PathValue("id"), in)
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	if err := s.App.DeleteIPSource(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTestSource(w http.ResponseWriter, r *http.Request) {
	ip, err := s.App.TestIPSource(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ip": ip})
}

// ---- jobs ----------------------------------------------------------------

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.ListJobs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	in, err := decode[app.JobInput](r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := s.App.CreateJob(r.Context(), in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) handleUpdateJob(w http.ResponseWriter, r *http.Request) {
	in, err := decode[app.JobInput](r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := s.App.UpdateJob(r.Context(), r.PathValue("id"), in)
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	if err := s.App.DeleteJob(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRunJob(w http.ResponseWriter, r *http.Request) {
	run, err := s.App.RunNow(r.Context(), r.PathValue("id"))
	if err != nil && run.JobID == "" {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleDryRun(w http.ResponseWriter, r *http.Request) {
	diff, err := s.App.DryRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, diff)
}

// handleRunAll reconciles every enabled job immediately ("Run all").
func (s *Server) handleRunAll(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.App.RunAllEnabled(r.Context()))
}

// ---- runs ----------------------------------------------------------------

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	runs, err := s.App.ListRuns(r.Context(), q.Get("job"), domain.RunStatus(q.Get("status")), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

// handlePurgeRuns applies the retention window immediately. The daemon also
// does this once an hour on its own; this is the manual/ops hook.
func (s *Server) handlePurgeRuns(w http.ResponseWriter, r *http.Request) {
	deleted, err := s.App.PurgeRuns(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	days := 0
	if st, err := s.App.GetSettings(r.Context()); err == nil {
		days = app.RetentionDays(st)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted, "keptDays": days})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid run id")
		return
	}
	run, err := s.App.GetRun(r.Context(), id)
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// ---- settings ------------------------------------------------------------

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.GetSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	in, err := decode[app.SettingsInput](r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := s.App.PatchSettings(r.Context(), in)
	if err != nil {
		// Validation failures (e.g. an out-of-range retention window) are the
		// caller's problem, not a server fault.
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	body, err := decode[struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}](r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.App.SetPassword(r.Context(), body.Username, body.Password); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTestNotify(w http.ResponseWriter, r *http.Request) {
	body, _ := decode[struct {
		ID string `json:"id"`
	}](r)
	if err := s.App.TestNotification(r.Context(), body.ID); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleExportConfig(w http.ResponseWriter, r *http.Request) {
	bundle, err := s.App.Export(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="ozddns-config.json"`)
	writeJSON(w, http.StatusOK, bundle)
}

func (s *Server) handleImportConfig(w http.ResponseWriter, r *http.Request) {
	bundle, err := decode[app.ConfigBundle](r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	stats, err := s.App.Import(r.Context(), bundle, true)
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	s.App.ApplySchedulerSettings(r.Context())
	writeJSON(w, http.StatusOK, stats)
}
