package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

// Keys:
//   user:{id}              -> JSON Profile
//   user:{id}:jobs         -> sorted set of job IDs by discovered_at
//   user:{id}:applications -> sorted set of application IDs by applied_at
//   user:{id}:matches      -> sorted set of job IDs by match score
//   job:{id}               -> JSON Job
//   job:index:platform:{p} -> set of job IDs for platform p
//   job:index:company:{c}  -> set of job IDs for company c
//   app:{id}               -> JSON Application
//   recruiter:candidates:{keyword} -> set of user IDs matching keyword
//   stats:global           -> hash with counters

// Store provides Redis-backed persistence for the job platform.
type Store struct {
	rdb *redis.Client
}

// New creates a new Redis store.
func New(addr, password string, db int) *Store {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &Store{rdb: rdb}
}

// Ping checks Redis connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return s.rdb.Ping(ctx).Err()
}

// Close closes the Redis connection.
func (s *Store) Close() error {
	return s.rdb.Close()
}

// --- User/Profile operations ---

// SaveProfile stores a user profile.
func (s *Store) SaveProfile(ctx context.Context, profile models.Profile) error {
	data, err := json.Marshal(profile)
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}

	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, userKey(profile.ID), data, 0)

	// Index by skills for recruiter search.
	for _, skill := range profile.Skills {
		pipe.SAdd(ctx, recruiterCandidatesKey(skill), profile.ID)
	}
	for _, title := range profile.Preferences.Titles {
		pipe.SAdd(ctx, recruiterCandidatesKey(title), profile.ID)
	}

	pipe.HIncrBy(ctx, "stats:global", "total_users", 1)

	_, err = pipe.Exec(ctx)
	return err
}

// GetProfile retrieves a user profile.
func (s *Store) GetProfile(ctx context.Context, userID string) (*models.Profile, error) {
	data, err := s.rdb.Get(ctx, userKey(userID)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var profile models.Profile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, err
	}
	return &profile, nil
}

// --- Job operations ---

// SaveJob stores a job and indexes it.
func (s *Store) SaveJob(ctx context.Context, job models.Job) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}

	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, jobKey(job.ID), data, 72*time.Hour) // TTL: 3 days
	pipe.SAdd(ctx, jobPlatformKey(string(job.Platform)), job.ID)
	pipe.SAdd(ctx, jobCompanyKey(job.Company), job.ID)
	pipe.HIncrBy(ctx, "stats:global", "total_jobs", 1)

	_, err = pipe.Exec(ctx)
	return err
}

// SaveJobs stores multiple jobs.
func (s *Store) SaveJobs(ctx context.Context, jobs []models.Job) error {
	pipe := s.rdb.Pipeline()
	for _, job := range jobs {
		data, err := json.Marshal(job)
		if err != nil {
			continue
		}
		pipe.Set(ctx, jobKey(job.ID), data, 72*time.Hour)
		pipe.SAdd(ctx, jobPlatformKey(string(job.Platform)), job.ID)
		pipe.SAdd(ctx, jobCompanyKey(job.Company), job.ID)
	}
	pipe.HIncrBy(ctx, "stats:global", "total_jobs", int64(len(jobs)))

	_, err := pipe.Exec(ctx)
	return err
}

// GetJob retrieves a job by ID.
func (s *Store) GetJob(ctx context.Context, jobID string) (*models.Job, error) {
	data, err := s.rdb.Get(ctx, jobKey(jobID)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var job models.Job
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// GetJobsByPlatform returns all job IDs for a platform.
func (s *Store) GetJobsByPlatform(ctx context.Context, platform string) ([]models.Job, error) {
	ids, err := s.rdb.SMembers(ctx, jobPlatformKey(platform)).Result()
	if err != nil {
		return nil, err
	}
	return s.getJobsByIDs(ctx, ids)
}

// --- User job tracking ---

// TrackUserJobs associates discovered jobs with a user.
func (s *Store) TrackUserJobs(ctx context.Context, userID string, jobs []models.Job) error {
	pipe := s.rdb.Pipeline()
	for _, job := range jobs {
		pipe.ZAdd(ctx, userJobsKey(userID), redis.Z{
			Score:  float64(job.DiscoveredAt.Unix()),
			Member: job.ID,
		})
	}
	_, err := pipe.Exec(ctx)
	return err
}

// TrackUserMatches stores match results for a user.
func (s *Store) TrackUserMatches(ctx context.Context, userID string, matches []models.MatchResult) error {
	pipe := s.rdb.Pipeline()
	for _, m := range matches {
		pipe.ZAdd(ctx, userMatchesKey(userID), redis.Z{
			Score:  m.Score,
			Member: m.Job.ID,
		})
	}
	_, err := pipe.Exec(ctx)
	return err
}

// GetUserMatches returns a user's top matches by score.
func (s *Store) GetUserMatches(ctx context.Context, userID string, limit int) ([]string, error) {
	return s.rdb.ZRevRangeByScore(ctx, userMatchesKey(userID), &redis.ZRangeBy{
		Min:   "-inf",
		Max:   "+inf",
		Count: int64(limit),
	}).Result()
}

// --- Application operations ---

// SaveApplication stores an application record.
func (s *Store) SaveApplication(ctx context.Context, app models.Application) error {
	data, err := json.Marshal(app)
	if err != nil {
		return fmt.Errorf("marshal application: %w", err)
	}

	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, appKey(app.ID), data, 0)
	pipe.ZAdd(ctx, userAppsKey(app.ProfileID), redis.Z{
		Score:  float64(app.AppliedAt.Unix()),
		Member: app.ID,
	})
	pipe.HIncrBy(ctx, "stats:global", "total_applications", 1)

	_, err = pipe.Exec(ctx)
	return err
}

// GetUserApplications returns a user's applications.
func (s *Store) GetUserApplications(ctx context.Context, userID string) ([]models.Application, error) {
	ids, err := s.rdb.ZRevRange(ctx, userAppsKey(userID), 0, -1).Result()
	if err != nil {
		return nil, err
	}

	apps := make([]models.Application, 0, len(ids))
	for _, id := range ids {
		data, err := s.rdb.Get(ctx, appKey(id)).Bytes()
		if err != nil {
			continue
		}
		var app models.Application
		if err := json.Unmarshal(data, &app); err != nil {
			continue
		}
		apps = append(apps, app)
	}
	return apps, nil
}

// --- Recruiter operations ---

// FindCandidates finds user IDs matching given keywords (skills/titles).
func (s *Store) FindCandidates(ctx context.Context, keywords []string) ([]string, error) {
	keys := make([]string, len(keywords))
	for i, kw := range keywords {
		keys[i] = recruiterCandidatesKey(kw)
	}

	// Union across all keyword sets.
	return s.rdb.SUnion(ctx, keys...).Result()
}

// FindCandidateProfiles returns full profiles matching keywords.
func (s *Store) FindCandidateProfiles(ctx context.Context, keywords []string) ([]models.Profile, error) {
	ids, err := s.FindCandidates(ctx, keywords)
	if err != nil {
		return nil, err
	}

	profiles := make([]models.Profile, 0, len(ids))
	for _, id := range ids {
		p, err := s.GetProfile(ctx, id)
		if err != nil || p == nil {
			continue
		}
		profiles = append(profiles, *p)
	}
	return profiles, nil
}

// --- Stats ---

// GlobalStats returns platform-wide statistics.
type GlobalStats struct {
	TotalUsers        int64 `json:"total_users"`
	TotalJobs         int64 `json:"total_jobs"`
	TotalApplications int64 `json:"total_applications"`
}

func (s *Store) GetGlobalStats(ctx context.Context) (*GlobalStats, error) {
	result, err := s.rdb.HGetAll(ctx, "stats:global").Result()
	if err != nil {
		return nil, err
	}

	stats := &GlobalStats{}
	if v, ok := result["total_users"]; ok {
		fmt.Sscanf(v, "%d", &stats.TotalUsers)
	}
	if v, ok := result["total_jobs"]; ok {
		fmt.Sscanf(v, "%d", &stats.TotalJobs)
	}
	if v, ok := result["total_applications"]; ok {
		fmt.Sscanf(v, "%d", &stats.TotalApplications)
	}
	return stats, nil
}

// --- helpers ---

func (s *Store) getJobsByIDs(ctx context.Context, ids []string) ([]models.Job, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	pipe := s.rdb.Pipeline()
	cmds := make([]*redis.StringCmd, len(ids))
	for i, id := range ids {
		cmds[i] = pipe.Get(ctx, jobKey(id))
	}
	pipe.Exec(ctx)

	jobs := make([]models.Job, 0, len(ids))
	for _, cmd := range cmds {
		data, err := cmd.Bytes()
		if err != nil {
			continue
		}
		var job models.Job
		if err := json.Unmarshal(data, &job); err != nil {
			continue
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func userKey(id string) string                { return "user:" + id }
func userJobsKey(id string) string            { return "user:" + id + ":jobs" }
func userAppsKey(id string) string            { return "user:" + id + ":applications" }
func userMatchesKey(id string) string         { return "user:" + id + ":matches" }
func jobKey(id string) string                 { return "job:" + id }
func jobPlatformKey(p string) string          { return "job:index:platform:" + p }
func jobCompanyKey(c string) string           { return "job:index:company:" + c }
func appKey(id string) string                 { return "app:" + id }
func recruiterCandidatesKey(kw string) string { return "recruiter:candidates:" + kw }
