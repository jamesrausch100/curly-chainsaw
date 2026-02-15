package workday

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

// Workday is the beast. No public API for applicants. Every company runs their
// own Workday tenant at a unique URL like:
//   https://{company}.wd5.myworkdayjobs.com/en-US/{site}
//
// The job search uses a JSON API under the hood, but applying requires
// browser automation (handled by the Applicant engine with rod/chromedp).
// This client handles the discovery/search part via Workday's internal JSON endpoints.

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// Tenant represents a Workday company tenant to watch.
type Tenant struct {
	Company  string // human-readable company name
	Tenant   string // e.g., "microsoft"
	Instance string // e.g., "wd5"
	Site     string // e.g., "Microsoft_Careers"
}

// TenantSearchURL builds the job search API URL for a tenant.
func (t Tenant) TenantSearchURL() string {
	return fmt.Sprintf("https://%s.%s.myworkdayjobs.com/wday/cxs/%s/%s/jobs",
		t.Tenant, t.Instance, t.Tenant, t.Site)
}

// TenantJobURL builds the public URL for a specific job.
func (t Tenant) TenantJobURL(jobSlug string) string {
	return fmt.Sprintf("https://%s.%s.myworkdayjobs.com/en-US/%s/job/%s",
		t.Tenant, t.Instance, t.Site, jobSlug)
}

// Client implements ats.Client for Workday.
type Client struct {
	httpClient *http.Client
	tenants    []Tenant
}

type Option func(*Client)

func WithTenants(tenants []Tenant) Option {
	return func(c *Client) { c.tenants = tenants }
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
	return models.PlatformWorkday
}

// Workday internal API response types.
type wdSearchResponse struct {
	Total   int            `json:"total"`
	JobData []wdJobPosting `json:"jobPostings"`
}

type wdJobPosting struct {
	Title        string `json:"title"`
	ExternalPath string `json:"externalPath"`
	LocationName string `json:"locationsText"`
	PostedOn     string `json:"postedOn"`
	BulletFields []string `json:"bulletFields"`
}

type wdSearchRequest struct {
	AppliedFacets map[string]interface{} `json:"appliedFacets"`
	Limit         int                    `json:"limit"`
	Offset        int                    `json:"offset"`
	SearchText    string                 `json:"searchText"`
}

// SearchJobs queries all configured Workday tenants for jobs.
func (c *Client) SearchJobs(ctx context.Context, query models.SearchQuery) ([]models.Job, error) {
	var allJobs []models.Job

	searchText := strings.Join(query.Keywords, " ")

	for _, tenant := range c.tenants {
		jobs, err := c.searchTenant(ctx, tenant, searchText)
		if err != nil {
			// Log but don't fail — one tenant down shouldn't kill the whole search.
			fmt.Printf("workday: error searching %s: %v\n", tenant.Company, err)
			continue
		}
		allJobs = append(allJobs, jobs...)
	}

	return allJobs, nil
}

func (c *Client) searchTenant(ctx context.Context, tenant Tenant, searchText string) ([]models.Job, error) {
	var allJobs []models.Job
	offset := 0
	limit := 20

	for {
		searchReq := wdSearchRequest{
			AppliedFacets: map[string]interface{}{},
			Limit:         limit,
			Offset:        offset,
			SearchText:    searchText,
		}

		body, err := json.Marshal(searchReq)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, tenant.TenantSearchURL(),
			strings.NewReader(string(body)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
		}

		var result wdSearchResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}

		for _, wj := range result.JobData {
			allJobs = append(allJobs, normalizeWorkdayJob(wj, tenant))
		}

		offset += limit
		if offset >= result.Total || len(result.JobData) == 0 {
			break
		}

		// Rate limiting — be nice to Workday servers.
		time.Sleep(500 * time.Millisecond)
	}

	return allJobs, nil
}

func normalizeWorkdayJob(wj wdJobPosting, tenant Tenant) models.Job {
	posted, _ := time.Parse("2006-01-02", wj.PostedOn)
	remote := strings.Contains(strings.ToLower(wj.LocationName), "remote")

	slug := strings.TrimPrefix(wj.ExternalPath, "/")

	return models.Job{
		ID:           fmt.Sprintf("workday_%s_%s", tenant.Tenant, slug),
		Platform:     models.PlatformWorkday,
		ExternalID:   wj.ExternalPath,
		Company:      tenant.Company,
		Title:        wj.Title,
		Location:     wj.LocationName,
		Remote:       remote,
		URL:          tenant.TenantJobURL(slug),
		PostedAt:     posted,
		DiscoveredAt: time.Now(),
		Tags:         wj.BulletFields,
		Metadata: map[string]string{
			"tenant":   tenant.Tenant,
			"instance": tenant.Instance,
			"site":     tenant.Site,
		},
	}
}

// GetJob fetches details for a specific Workday job.
func (c *Client) GetJob(ctx context.Context, externalID string) (*models.Job, error) {
	for _, tenant := range c.tenants {
		url := fmt.Sprintf("https://%s.%s.myworkdayjobs.com/wday/cxs/%s/%s%s",
			tenant.Tenant, tenant.Instance, tenant.Tenant, tenant.Site, externalID)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			continue
		}

		var detail struct {
			JobPostingInfo struct {
				Title       string `json:"title"`
				Location    string `json:"location"`
				PostedOn    string `json:"startDate"`
				Description string `json:"jobDescription"`
			} `json:"jobPostingInfo"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
			continue
		}

		job := models.Job{
			ID:          fmt.Sprintf("workday_%s_%s", tenant.Tenant, externalID),
			Platform:    models.PlatformWorkday,
			ExternalID:  externalID,
			Company:     tenant.Company,
			Title:       detail.JobPostingInfo.Title,
			Description: detail.JobPostingInfo.Description,
			Location:    detail.JobPostingInfo.Location,
			URL:         tenant.TenantJobURL(strings.TrimPrefix(externalID, "/")),
		}
		return &job, nil
	}

	return nil, fmt.Errorf("workday: job %s not found", externalID)
}

// Apply for Workday requires browser automation. This returns an error directing
// the caller to use the browser-based application engine instead.
func (c *Client) Apply(_ context.Context, job models.Job, _ models.Profile, _ map[string]string) (*models.Application, error) {
	return nil, fmt.Errorf(
		"workday: direct API application not supported — use browser automation for %s at %s",
		job.Title, job.URL,
	)
}

func (c *Client) ApplicationStatus(_ context.Context, _ string) (models.ApplicationStatus, error) {
	return models.StatusApplied, nil
}
