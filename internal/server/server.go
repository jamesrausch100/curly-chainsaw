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
	"github.com/jamesrausch100/curly-chainsaw/internal/trust"
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

	// Trust — optional Muster Protocol integration (DaaSTrustLayer).
	trustClient   *trust.Client
	trustEnricher *trust.Enricher
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

	// Wire up Muster Protocol if trust is enabled.
	if cfg.Trust.Enabled && cfg.Trust.GatewayURL != "" {
		tc := trust.NewClient(trust.Config{
			GatewayURL: cfg.Trust.GatewayURL,
			APIKey:     cfg.Trust.APIKey,
			TimeoutSec: cfg.Trust.TimeoutSec,
		})
		s.trustClient = tc
		s.trustEnricher = trust.NewEnricher(tc)
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

	// Trust — Muster Protocol (DaaSTrustLayer).
	s.mux.HandleFunc("/api/trust/verify", s.handleTrustVerify)
	s.mux.HandleFunc("/api/trust/status", s.handleTrustStatus)
	s.mux.HandleFunc("/api/trust/enroll", s.handleTrustEnroll)
	s.mux.HandleFunc("/api/trust/chain", s.handleTrustChain)
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

		// Enroll in Muster Protocol — bring this person INTO the trust system.
		enrolled := false
		if s.trustClient != nil && len(profile.Links) > 0 {
			enrolled = s.enrollProfile(r.Context(), &profile)
		}

		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"status":         "saved",
			"id":             profile.ID,
			"trust_enrolled": enrolled,
		})

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

	// Enrich matches with trust scores if Muster is available.
	if s.trustEnricher != nil && len(matches) > 0 {
		matches = s.trustEnricher.EnrichMatches(ctx, matches, *profile)
	}

	if len(matches) > 0 {
		s.store.TrackUserMatches(r.Context(), req.UserID, matches)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"discovered":    len(discResult.Jobs),
		"matched":       len(matches),
		"top_matches":   truncateMatches(matches, 20),
		"trust_enabled": s.trustClient != nil,
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

		// Record outcome in Muster — feed application result into trust chain.
		if s.trustClient != nil {
			go s.trustClient.RecordOutcome(r.Context(), trust.Outcome{
				EntityID: req.UserID,
				Action:   "application_sent",
				Target:   job.Company,
				Success:  true,
				Evidence: map[string]string{
					"job_id":    job.ID,
					"job_title": job.Title,
					"platform":  string(job.Platform),
				},
			})
		}
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

// enrollProfile registers a profile in the Muster Protocol.
// Step 1: Claim the entity. Step 2: Submit evidence (links).
// Step 3: Score them. This is how people enter the trust system.
func (s *Server) enrollProfile(ctx context.Context, profile *models.Profile) bool {
	// Find the primary identifier — prefer email, then first link.
	identifier := profile.Email
	if identifier == "" {
		for _, link := range profile.Links {
			if link != "" {
				identifier = link
				break
			}
		}
	}
	if identifier == "" {
		return false
	}

	// Step 1: Register entity.
	entity, err := s.trustClient.ClaimEntity(ctx, identifier, "person", map[string]string{
		"name":     profile.Name,
		"location": profile.Location,
	})
	if err != nil {
		return false
	}

	// Step 2: Submit evidence — all links become scorable data.
	if len(profile.Links) > 0 {
		s.trustClient.SubmitEvidence(ctx, entity.ID, profile.Links, nil)
	}

	// Step 3: Score them.
	score, err := s.trustClient.ScoreEntity(ctx, identifier)
	if err != nil {
		return false
	}

	// Write trust data back to the profile.
	profile.TrustScore = score.Score
	profile.TrustGrade = string(score.Grade)
	profile.TrustProof = score.ProofHash

	return true
}

func truncateMatches(matches []models.MatchResult, n int) []models.MatchResult {
	if len(matches) <= n {
		return matches
	}
	return matches[:n]
}

// --- Trust (Muster Protocol / DaaSTrustLayer) ---

func (s *Server) handleTrustVerify(w http.ResponseWriter, r *http.Request) {
	entity := r.URL.Query().Get("entity")
	if entity == "" {
		writeError(w, http.StatusBadRequest, "entity parameter required (domain, company, URL, etc.)")
		return
	}

	if s.trustClient == nil {
		writeError(w, http.StatusServiceUnavailable, "trust layer not configured — set trust.enabled=true and trust.gateway_url in config")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	score, err := s.trustClient.Verify(ctx, entity)
	if err != nil {
		writeError(w, http.StatusBadGateway, "muster gateway error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"entity":    score.Entity,
		"score":     score.Score,
		"grade":     score.Grade,
		"trusted":   score.Trusted,
		"action":    score.Grade.Action(),
		"breakdown": score.Breakdown,
		"proof":     score.ProofHash,
	})
}

func (s *Server) handleTrustEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	if s.trustClient == nil {
		writeError(w, http.StatusServiceUnavailable, "trust layer not configured")
		return
	}

	var req struct {
		UserID string `json:"user_id"`
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

	if len(profile.Links) == 0 {
		writeError(w, http.StatusBadRequest, "profile has no links — add github, linkedin, or portfolio first")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	enrolled := s.enrollProfile(ctx, profile)
	if !enrolled {
		writeError(w, http.StatusBadGateway, "enrollment failed — check Muster gateway")
		return
	}

	// Re-save profile with trust data.
	s.store.SaveProfile(r.Context(), *profile)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enrolled":    true,
		"trust_score": profile.TrustScore,
		"trust_grade": profile.TrustGrade,
	})
}

func (s *Server) handleTrustChain(w http.ResponseWriter, r *http.Request) {
	entityID := r.URL.Query().Get("entity_id")
	if entityID == "" {
		writeError(w, http.StatusBadRequest, "entity_id required")
		return
	}
	if s.trustClient == nil {
		writeError(w, http.StatusServiceUnavailable, "trust layer not configured")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	blocks, err := s.trustClient.GetChainHistory(ctx, entityID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "chain history error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"entity_id": entityID,
		"blocks":    blocks,
		"count":     len(blocks),
	})
}

func (s *Server) handleTrustStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"enabled": s.trustClient != nil,
	}

	if s.trustClient != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		status["gateway_reachable"] = s.trustClient.IsAvailable(ctx)
		status["gateway_url"] = s.cfg.Trust.GatewayURL
	}

	writeJSON(w, http.StatusOK, status)
}
