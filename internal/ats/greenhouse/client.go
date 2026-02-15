package greenhouse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

const (
	harvestBaseURL  = "https://harvest.greenhouse.io/v1"
	jobBoardBaseURL = "https://boards-api.greenhouse.io/v1/boards"
	jobBoardWebURL  = "https://boards.greenhouse.io"
)

// Client implements ats.Client for Greenhouse.
type Client struct {
	httpClient  *http.Client
	apiKey      string   // Harvest API key (if available)
	boardTokens []string // company board tokens
}

type Option func(*Client)

func WithAPIKey(key string) Option {
	return func(c *Client) { c.apiKey = key }
}

func WithBoardTokens(tokens []string) Option {
	return func(c *Client) { c.boardTokens = tokens }
}

func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

func New(opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) Platform() models.Platform {
	return models.PlatformGreenhouse
}

// Greenhouse job board API response types.
type ghBoardResponse struct {
	Jobs []ghJob `json:"jobs"`
}

type ghJob struct {
	ID          int64       `json:"id"`
	Title       string      `json:"title"`
	UpdatedAt   string      `json:"updated_at"`
	Location    ghLocation  `json:"location"`
	AbsoluteURL string     `json:"absolute_url"`
	Departments []ghDept    `json:"departments"`
	Content     string      `json:"content"` // HTML description (detail endpoint)
	Questions   []ghQuestion `json:"questions,omitempty"`
}

type ghLocation struct {
	Name string `json:"name"`
}

type ghDept struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type ghQuestion struct {
	Label    string   `json:"label"`
	Required bool     `json:"required"`
	Fields   []ghField `json:"fields"`
}

type ghField struct {
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	Values []ghValue `json:"values,omitempty"`
}

type ghValue struct {
	Label string `json:"label"`
	Value int64  `json:"value"`
}

// SearchJobs fetches jobs from all configured Greenhouse boards.
func (c *Client) SearchJobs(ctx context.Context, query models.SearchQuery) ([]models.Job, error) {
	var allJobs []models.Job

	for _, token := range c.boardTokens {
		jobs, err := c.fetchBoardJobs(ctx, token)
		if err != nil {
			return nil, fmt.Errorf("greenhouse: fetch board %s: %w", token, err)
		}

		for _, j := range jobs {
			if matchesQuery(j, query) {
				allJobs = append(allJobs, j)
			}
		}
	}

	return allJobs, nil
}

func (c *Client) fetchBoardJobs(ctx context.Context, boardToken string) ([]models.Job, error) {
	url := fmt.Sprintf("%s/%s/jobs?content=true", jobBoardBaseURL, boardToken)

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

	var result ghBoardResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	jobs := make([]models.Job, 0, len(result.Jobs))
	for _, gj := range result.Jobs {
		jobs = append(jobs, normalizeGHJob(gj, boardToken))
	}
	return jobs, nil
}

func normalizeGHJob(gj ghJob, boardToken string) models.Job {
	updated, _ := time.Parse("2006-01-02T15:04:05-07:00", gj.UpdatedAt)

	var dept string
	if len(gj.Departments) > 0 {
		dept = gj.Departments[0].Name
	}

	remote := strings.Contains(strings.ToLower(gj.Location.Name), "remote")

	return models.Job{
		ID:           fmt.Sprintf("greenhouse_%s_%d", boardToken, gj.ID),
		Platform:     models.PlatformGreenhouse,
		ExternalID:   fmt.Sprintf("%d", gj.ID),
		Company:      boardToken,
		Title:        gj.Title,
		Description:  gj.Content,
		Location:     gj.Location.Name,
		Remote:       remote,
		Department:   dept,
		URL:          gj.AbsoluteURL,
		PostedAt:     updated,
		DiscoveredAt: time.Now(),
	}
}

// GetJob fetches details for a specific Greenhouse job posting.
func (c *Client) GetJob(ctx context.Context, externalID string) (*models.Job, error) {
	for _, token := range c.boardTokens {
		url := fmt.Sprintf("%s/%s/jobs/%s?questions=true", jobBoardBaseURL, token, externalID)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			continue
		}

		var gj ghJob
		if err := json.NewDecoder(resp.Body).Decode(&gj); err != nil {
			continue
		}

		job := normalizeGHJob(gj, token)
		return &job, nil
	}

	return nil, fmt.Errorf("greenhouse: job %s not found", externalID)
}

// Apply submits an application via Greenhouse's job board API.
func (c *Client) Apply(ctx context.Context, job models.Job, profile models.Profile, responses map[string]string) (*models.Application, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// Standard fields
	fields := map[string]string{
		"first_name": firstName(profile.Name),
		"last_name":  lastName(profile.Name),
		"email":      profile.Email,
		"phone":      profile.Phone,
	}

	for k, v := range fields {
		if v != "" {
			if err := w.WriteField(k, v); err != nil {
				return nil, fmt.Errorf("write field %s: %w", k, err)
			}
		}
	}

	// Custom responses
	for k, v := range responses {
		if err := w.WriteField(k, v); err != nil {
			return nil, fmt.Errorf("write response field %s: %w", k, err)
		}
	}

	// Resume file
	if profile.ResumeFile != "" {
		if err := attachFile(w, "resume", profile.ResumeFile); err != nil {
			return nil, fmt.Errorf("attach resume: %w", err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/%s/jobs/%s/application", jobBoardBaseURL, job.Company, job.ExternalID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

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
		ID:        fmt.Sprintf("gh_app_%s_%d", job.ExternalID, time.Now().UnixMilli()),
		JobID:     job.ID,
		ProfileID: profile.ID,
		Platform:  models.PlatformGreenhouse,
		Status:    models.StatusApplied,
		AppliedAt: time.Now(),
		Responses: responses,
	}, nil
}

func (c *Client) ApplicationStatus(_ context.Context, _ string) (models.ApplicationStatus, error) {
	return models.StatusApplied, nil
}

func attachFile(w *multipart.Writer, fieldName, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	part, err := w.CreateFormFile(fieldName, filepath.Base(filePath))
	if err != nil {
		return err
	}

	_, err = io.Copy(part, f)
	return err
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
