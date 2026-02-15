package models

import "time"

// Platform represents a supported ATS platform.
type Platform string

const (
	PlatformAshby      Platform = "ashby"
	PlatformGreenhouse Platform = "greenhouse"
	PlatformWorkday    Platform = "workday"
)

// Job represents a normalized job listing from any ATS platform.
type Job struct {
	ID            string            `json:"id"`
	Platform      Platform          `json:"platform"`
	ExternalID    string            `json:"external_id"`
	Company       string            `json:"company"`
	Title         string            `json:"title"`
	Description   string            `json:"description"`
	Location      string            `json:"location"`
	Remote        bool              `json:"remote"`
	Salary        SalaryRange       `json:"salary,omitempty"`
	Department    string            `json:"department,omitempty"`
	Team          string            `json:"team,omitempty"`
	URL           string            `json:"url"`
	PostedAt      time.Time         `json:"posted_at"`
	DiscoveredAt  time.Time         `json:"discovered_at"`
	Tags          []string          `json:"tags,omitempty"`
	Requirements  []string          `json:"requirements,omitempty"`
	Benefits      []string          `json:"benefits,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	ApplicationID string            `json:"application_id,omitempty"`

	// Trust — populated by the Muster Protocol (DaaSTrustLayer).
	// Scores the company behind this listing: is the employer legit?
	CompanyTrustScore int    `json:"company_trust_score,omitempty"` // 0-100
	CompanyTrustGrade string `json:"company_trust_grade,omitempty"` // A-F
}

// SalaryRange represents compensation information.
type SalaryRange struct {
	Min      int    `json:"min,omitempty"`
	Max      int    `json:"max,omitempty"`
	Currency string `json:"currency,omitempty"`
	Period   string `json:"period,omitempty"` // yearly, monthly, hourly
}

// Profile represents a job seeker's profile.
type Profile struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Email          string          `json:"email"`
	Phone          string          `json:"phone,omitempty"`
	Location       string          `json:"location"`
	RemoteOnly     bool            `json:"remote_only"`
	ResumeFile     string          `json:"resume_file"`
	CoverLetterTpl string          `json:"cover_letter_template,omitempty"`
	Skills         []string        `json:"skills"`
	Experience     []Experience    `json:"experience"`
	Education      []Education     `json:"education"`
	Preferences    JobPreferences  `json:"preferences"`
	Links          map[string]string `json:"links,omitempty"` // linkedin, github, portfolio, etc.

	// Trust — populated by the Muster Protocol (DaaSTrustLayer).
	// Proves real capabilities beyond resume keywords.
	TrustScore int    `json:"trust_score,omitempty"` // 0-100
	TrustGrade string `json:"trust_grade,omitempty"` // A-F
	TrustProof string `json:"trust_proof,omitempty"` // SHA-256 chain proof from Muster
}

// Experience represents a work experience entry.
type Experience struct {
	Company     string `json:"company"`
	Title       string `json:"title"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date,omitempty"` // empty = current
	Description string `json:"description,omitempty"`
}

// Education represents an education entry.
type Education struct {
	Institution string `json:"institution"`
	Degree      string `json:"degree"`
	Field       string `json:"field"`
	EndDate     string `json:"end_date,omitempty"`
}

// JobPreferences captures what the user is looking for.
type JobPreferences struct {
	Titles        []string `json:"titles"`         // desired job titles
	MinSalary     int      `json:"min_salary"`
	MaxCommute    int      `json:"max_commute_mi"` // max commute in miles, 0 = no limit
	Industries    []string `json:"industries,omitempty"`
	CompanySizes  []string `json:"company_sizes,omitempty"` // startup, mid, enterprise
	ExcludeCompanies []string `json:"exclude_companies,omitempty"`
}

// Application tracks a job application.
type Application struct {
	ID          string            `json:"id"`
	JobID       string            `json:"job_id"`
	ProfileID   string            `json:"profile_id"`
	Platform    Platform          `json:"platform"`
	Status      ApplicationStatus `json:"status"`
	AppliedAt   time.Time         `json:"applied_at,omitempty"`
	MatchScore  float64           `json:"match_score"`
	Notes       string            `json:"notes,omitempty"`
	Responses   map[string]string `json:"responses,omitempty"` // question -> answer
}

// ApplicationStatus tracks the state of an application.
type ApplicationStatus string

const (
	StatusPending   ApplicationStatus = "pending"
	StatusApplied   ApplicationStatus = "applied"
	StatusFailed    ApplicationStatus = "failed"
	StatusSkipped   ApplicationStatus = "skipped"
	StatusReviewing ApplicationStatus = "reviewing"
)

// MatchResult pairs a job with a score and reasoning.
type MatchResult struct {
	Job    Job     `json:"job"`
	Score  float64 `json:"score"`  // 0.0 - 1.0
	Reason string  `json:"reason"` // why this job matched

	// Trust — when Muster is available, both sides are scored.
	CompanyTrusted   bool `json:"company_trusted,omitempty"`   // employer passed verification
	CandidateTrusted bool `json:"candidate_trusted,omitempty"` // candidate proved capabilities
}

// SearchQuery represents a job search request.
type SearchQuery struct {
	Keywords  []string `json:"keywords"`
	Location  string   `json:"location,omitempty"`
	Remote    *bool    `json:"remote,omitempty"`
	PostedAfter *time.Time `json:"posted_after,omitempty"`
	Platforms []Platform `json:"platforms,omitempty"` // empty = all
}
