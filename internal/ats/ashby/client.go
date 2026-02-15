package ashby

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

const (
	defaultBaseURL = "https://api.ashbyhq.com"
	jobBoardBase   = "https://jobs.ashbyhq.com"
)

// Client implements ats.Client for Ashby's API.
type Client struct {
	httpClient *http.Client
	baseURL    string
	boardTokens []string // company board tokens to scrape
}

// Option configures the Ashby client.
type Option func(*Client)

// WithBoardTokens sets which company job boards to watch.
func WithBoardTokens(tokens []string) Option {
	return func(c *Client) { c.boardTokens = tokens }
}

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// New creates a new Ashby client.
func New(opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    defaultBaseURL,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) Platform() models.Platform {
	return models.PlatformAshby
}

// ashbyJobPostingResponse represents Ashby's job board API response.
type ashbyJobPostingResponse struct {
	Jobs []ashbyJob `json:"jobs"`
}

type ashbyJob struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Location         string   `json:"locationName"`
	Department       string   `json:"departmentName"`
	Team             string   `json:"teamName"`
	IsRemote         bool     `json:"isRemote"`
	PublishedAt      string   `json:"publishedAt"`
	DescriptionHTML  string   `json:"descriptionHtml"`
	DescriptionPlain string   `json:"descriptionPlain"`
	CompensationTier string   `json:"compensationTierSummary"`
	EmploymentType   string   `json:"employmentType"`
}

// SearchJobs queries all configured Ashby job boards for matching postings.
func (c *Client) SearchJobs(ctx context.Context, query models.SearchQuery) ([]models.Job, error) {
	var allJobs []models.Job

	for _, token := range c.boardTokens {
		jobs, err := c.fetchBoardJobs(ctx, token)
		if err != nil {
			return nil, fmt.Errorf("ashby: fetch board %s: %w", token, err)
		}

		for _, j := range jobs {
			if matchesQuery(j, query) {
				allJobs = append(allJobs, j)
			}
		}
	}

	return allJobs, nil
}

// fetchBoardJobs fetches all jobs from an Ashby job board using the posting API.
func (c *Client) fetchBoardJobs(ctx context.Context, boardToken string) ([]models.Job, error) {
	url := fmt.Sprintf("%s/posting-api/job-board/%s", c.baseURL, boardToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var result ashbyJobPostingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	jobs := make([]models.Job, 0, len(result.Jobs))
	for _, aj := range result.Jobs {
		jobs = append(jobs, normalizeAshbyJob(aj, boardToken))
	}
	return jobs, nil
}

func normalizeAshbyJob(aj ashbyJob, boardToken string) models.Job {
	posted, _ := time.Parse(time.RFC3339, aj.PublishedAt)

	return models.Job{
		ID:           fmt.Sprintf("ashby_%s_%s", boardToken, aj.ID),
		Platform:     models.PlatformAshby,
		ExternalID:   aj.ID,
		Company:      boardToken, // resolved from board token
		Title:        aj.Title,
		Description:  aj.DescriptionPlain,
		Location:     aj.Location,
		Remote:       aj.IsRemote,
		Department:   aj.Department,
		Team:         aj.Team,
		URL:          fmt.Sprintf("%s/%s/%s", jobBoardBase, boardToken, aj.ID),
		PostedAt:     posted,
		DiscoveredAt: time.Now(),
		Metadata: map[string]string{
			"employment_type":    aj.EmploymentType,
			"compensation_tier":  aj.CompensationTier,
			"description_html":   aj.DescriptionHTML,
		},
	}
}

// GetJob fetches a single job from Ashby by its posting ID.
func (c *Client) GetJob(ctx context.Context, externalID string) (*models.Job, error) {
	// Ashby posting API: /posting-api/job-board/{boardToken}/posting/{postingId}
	for _, token := range c.boardTokens {
		url := fmt.Sprintf("%s/posting-api/job-board/%s/posting/%s", c.baseURL, token, externalID)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			continue // try next board
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			continue
		}

		if resp.StatusCode != http.StatusOK {
			continue
		}

		var aj ashbyJob
		if err := json.NewDecoder(resp.Body).Decode(&aj); err != nil {
			continue
		}

		job := normalizeAshbyJob(aj, token)
		return &job, nil
	}

	return nil, fmt.Errorf("ashby: job %s not found on any board", externalID)
}

// Apply submits an application through Ashby's posting API.
func (c *Client) Apply(ctx context.Context, job models.Job, profile models.Profile, responses map[string]string) (*models.Application, error) {
	payload := map[string]interface{}{
		"firstName": firstName(profile.Name),
		"lastName":  lastName(profile.Name),
		"email":     profile.Email,
		"phone":     profile.Phone,
		"linkedin":  profile.Links["linkedin"],
		"github":    profile.Links["github"],
		"website":   profile.Links["portfolio"],
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	url := fmt.Sprintf("%s/posting-api/job-board/%s/posting/%s/application", c.baseURL, job.Company, job.ExternalID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("submit application: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("application rejected (status %d): %s", resp.StatusCode, string(respBody))
	}

	return &models.Application{
		ID:        fmt.Sprintf("ashby_app_%s_%d", job.ExternalID, time.Now().UnixMilli()),
		JobID:     job.ID,
		ProfileID: profile.ID,
		Platform:  models.PlatformAshby,
		Status:    models.StatusApplied,
		AppliedAt: time.Now(),
		Responses: responses,
	}, nil
}

func (c *Client) ApplicationStatus(_ context.Context, _ string) (models.ApplicationStatus, error) {
	// Ashby's public API doesn't expose application status tracking.
	return models.StatusApplied, nil
}

func matchesQuery(job models.Job, query models.SearchQuery) bool {
	if len(query.Keywords) == 0 {
		return true
	}
	text := strings.ToLower(job.Title + " " + job.Description + " " + job.Department)
	for _, kw := range query.Keywords {
		if strings.Contains(text, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

func firstName(name string) string {
	parts := strings.Fields(name)
	if len(parts) > 0 {
		return parts[0]
	}
	return name
}

func lastName(name string) string {
	parts := strings.Fields(name)
	if len(parts) > 1 {
		return strings.Join(parts[1:], " ")
	}
	return ""
}
