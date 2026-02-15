package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jamesrausch100/curly-chainsaw/internal/applicant"
	"github.com/jamesrausch100/curly-chainsaw/internal/ats"
	"github.com/jamesrausch100/curly-chainsaw/internal/config"
	"github.com/jamesrausch100/curly-chainsaw/internal/discovery"
	"github.com/jamesrausch100/curly-chainsaw/internal/matcher"
	"github.com/jamesrausch100/curly-chainsaw/internal/storage"
	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

// Server is the HTTP API + web dashboard server.
type Server struct {
	store     *storage.Store
	registry  *ats.Registry
	discovery *discovery.Engine
	matcher   *matcher.Matcher
	applicant *applicant.Engine
	cfg       *config.Config
	mux       *http.ServeMux
}

// New creates a new server.
func New(store *storage.Store, registry *ats.Registry, cfg *config.Config) *Server {
	s := &Server{
		store:     store,
		registry:  registry,
		discovery: discovery.New(registry),
		matcher:   matcher.NewDefault(),
		applicant: applicant.New(registry),
		cfg:       cfg,
		mux:       http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	// Dashboard.
	s.mux.HandleFunc("/", s.handleDashboard)

	// API endpoints.
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/stats", s.handleStats)

	// User/profile.
	s.mux.HandleFunc("/api/profile", s.handleProfile)

	// Jobs.
	s.mux.HandleFunc("/api/jobs/search", s.handleJobSearch)
	s.mux.HandleFunc("/api/jobs/discover", s.handleDiscover)
	s.mux.HandleFunc("/api/jobs/matches", s.handleMatches)

	// Applications.
	s.mux.HandleFunc("/api/applications", s.handleApplications)
	s.mux.HandleFunc("/api/apply", s.handleApply)

	// Recruiter.
	s.mux.HandleFunc("/api/recruiter/search", s.handleRecruiterSearch)
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Start starts the HTTP server.
func (s *Server) Start(addr string) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      s,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	fmt.Printf("curly-chainsaw server starting on %s\n", addr)
	return srv.ListenAndServe()
}

// --- Handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	err := s.store.Ping(ctx)
	status := "ok"
	if err != nil {
		status = "redis_down"
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":    status,
		"platforms": fmt.Sprintf("%d", len(s.registry.Platforms())),
	})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.GetGlobalStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		userID := r.URL.Query().Get("user_id")
		if userID == "" {
			writeError(w, http.StatusBadRequest, "user_id required")
			return
		}
		profile, err := s.store.GetProfile(r.Context(), userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if profile == nil {
			writeError(w, http.StatusNotFound, "profile not found")
			return
		}
		writeJSON(w, http.StatusOK, profile)

	case http.MethodPost:
		var profile models.Profile
		if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if profile.ID == "" {
			writeError(w, http.StatusBadRequest, "id is required")
			return
		}
		if err := s.store.SaveProfile(r.Context(), profile); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "saved", "id": profile.ID})

	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleJobSearch(w http.ResponseWriter, r *http.Request) {
	keywords := r.URL.Query().Get("q")
	if keywords == "" {
		writeError(w, http.StatusBadRequest, "q (search query) required")
		return
	}

	platform := r.URL.Query().Get("platform")

	query := models.SearchQuery{
		Keywords: strings.Split(keywords, ","),
	}
	if platform != "" {
		query.Platforms = []models.Platform{models.Platform(platform)}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result := s.discovery.Discover(ctx, query)

	// Store discovered jobs.
	if len(result.Jobs) > 0 {
		s.store.SaveJobs(r.Context(), result.Jobs)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total":    len(result.Jobs),
		"new":      result.NewCount,
		"jobs":     result.Jobs,
		"duration": result.Duration.String(),
	})
}

func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	var req struct {
		UserID   string   `json:"user_id"`
		Keywords []string `json:"keywords"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	profile, err := s.store.GetProfile(r.Context(), req.UserID)
	if err != nil || profile == nil {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}

	keywords := req.Keywords
	if len(keywords) == 0 {
		keywords = profile.Preferences.Titles
	}

	query := models.SearchQuery{Keywords: keywords}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	discResult := s.discovery.Discover(ctx, query)
	if len(discResult.Jobs) > 0 {
		s.store.SaveJobs(r.Context(), discResult.Jobs)
		s.store.TrackUserJobs(r.Context(), req.UserID, discResult.Jobs)
	}

	// Match against profile.
	matches := s.matcher.Match(discResult.Jobs, *profile)
	if len(matches) > 0 {
		s.store.TrackUserMatches(r.Context(), req.UserID, matches)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"discovered": len(discResult.Jobs),
		"matched":    len(matches),
		"top_matches": truncateMatches(matches, 20),
	})
}

func (s *Server) handleMatches(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id required")
		return
	}

	jobIDs, err := s.store.GetUserMatches(r.Context(), userID, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var jobs []models.Job
	for _, id := range jobIDs {
		job, err := s.store.GetJob(r.Context(), id)
		if err != nil || job == nil {
			continue
		}
		jobs = append(jobs, *job)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total": len(jobs),
		"jobs":  jobs,
	})
}

func (s *Server) handleApplications(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id required")
		return
	}

	apps, err := s.store.GetUserApplications(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total":        len(apps),
		"applications": apps,
	})
}

func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	var req struct {
		UserID string   `json:"user_id"`
		JobIDs []string `json:"job_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	profile, err := s.store.GetProfile(r.Context(), req.UserID)
	if err != nil || profile == nil {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}

	var applied, failed int
	for _, jobID := range req.JobIDs {
		job, err := s.store.GetJob(r.Context(), jobID)
		if err != nil || job == nil {
			failed++
			continue
		}

		client, ok := s.registry.Get(job.Platform)
		if !ok {
			failed++
			continue
		}

		app, err := client.Apply(r.Context(), *job, *profile, nil)
		if err != nil {
			failed++
			continue
		}

		s.store.SaveApplication(r.Context(), *app)
		applied++
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"applied": applied,
		"failed":  failed,
	})
}

func (s *Server) handleRecruiterSearch(w http.ResponseWriter, r *http.Request) {
	keywords := r.URL.Query().Get("skills")
	if keywords == "" {
		writeError(w, http.StatusBadRequest, "skills parameter required (comma-separated)")
		return
	}

	profiles, err := s.store.FindCandidateProfiles(r.Context(), strings.Split(keywords, ","))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Redact sensitive info for recruiter view.
	for i := range profiles {
		profiles[i].Phone = ""
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total":      len(profiles),
		"candidates": profiles,
	})
}

// --- Dashboard ---

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML))
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func truncateMatches(matches []models.MatchResult, n int) []models.MatchResult {
	if len(matches) <= n {
		return matches
	}
	return matches[:n]
}
