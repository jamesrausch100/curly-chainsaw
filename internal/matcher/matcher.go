package matcher

import (
	"sort"
	"strings"

	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

// Matcher scores jobs against a user profile and preferences.
type Matcher struct {
	weights Weights
}

// Weights controls how much each signal contributes to the match score.
type Weights struct {
	TitleMatch    float64 // how much a title match matters
	SkillMatch    float64 // how much skill overlap matters
	LocationMatch float64 // location/remote preference match
	SalaryMatch   float64 // meets salary minimum
	ExcludeMatch  float64 // penalty for excluded companies
}

// DefaultWeights returns sensible default scoring weights.
func DefaultWeights() Weights {
	return Weights{
		TitleMatch:    0.35,
		SkillMatch:    0.30,
		LocationMatch: 0.15,
		SalaryMatch:   0.15,
		ExcludeMatch:  1.0, // full weight penalty
	}
}

// New creates a matcher with the given weights.
func New(w Weights) *Matcher {
	return &Matcher{weights: w}
}

// NewDefault creates a matcher with default weights.
func NewDefault() *Matcher {
	return New(DefaultWeights())
}

// Match scores a slice of jobs against a profile and returns sorted results.
func (m *Matcher) Match(jobs []models.Job, profile models.Profile) []models.MatchResult {
	results := make([]models.MatchResult, 0, len(jobs))

	for _, job := range jobs {
		result := m.ScoreJob(job, profile)
		if result.Score > 0 {
			results = append(results, result)
		}
	}

	// Sort by score descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

// ScoreJob computes a match score for a single job against a profile.
func (m *Matcher) ScoreJob(job models.Job, profile models.Profile) models.MatchResult {
	var score float64
	var reasons []string

	// Check exclusion first — hard reject.
	for _, excluded := range profile.Preferences.ExcludeCompanies {
		if strings.EqualFold(job.Company, excluded) {
			return models.MatchResult{
				Job:    job,
				Score:  0,
				Reason: "excluded company",
			}
		}
	}

	// Title match.
	titleScore := m.scoreTitleMatch(job.Title, profile.Preferences.Titles)
	score += titleScore * m.weights.TitleMatch
	if titleScore > 0 {
		reasons = append(reasons, "title match")
	}

	// Skill match.
	skillScore := m.scoreSkillMatch(job, profile.Skills)
	score += skillScore * m.weights.SkillMatch
	if skillScore > 0.5 {
		reasons = append(reasons, "strong skill overlap")
	} else if skillScore > 0 {
		reasons = append(reasons, "some skill overlap")
	}

	// Location/remote match.
	locationScore := m.scoreLocationMatch(job, profile)
	score += locationScore * m.weights.LocationMatch
	if locationScore > 0 {
		if job.Remote {
			reasons = append(reasons, "remote")
		} else {
			reasons = append(reasons, "location match")
		}
	}

	// Salary match.
	salaryScore := m.scoreSalaryMatch(job, profile.Preferences.MinSalary)
	score += salaryScore * m.weights.SalaryMatch
	if salaryScore > 0 && job.Salary.Min > 0 {
		reasons = append(reasons, "meets salary range")
	}

	// Clamp to [0, 1].
	if score > 1.0 {
		score = 1.0
	}

	reason := strings.Join(reasons, ", ")
	if reason == "" {
		reason = "weak match"
	}

	return models.MatchResult{
		Job:    job,
		Score:  score,
		Reason: reason,
	}
}

func (m *Matcher) scoreTitleMatch(jobTitle string, desiredTitles []string) float64 {
	lower := strings.ToLower(jobTitle)
	bestScore := 0.0

	for _, desired := range desiredTitles {
		desiredLower := strings.ToLower(desired)

		// Exact match.
		if lower == desiredLower {
			return 1.0
		}

		// Contains match.
		if strings.Contains(lower, desiredLower) || strings.Contains(desiredLower, lower) {
			if 0.8 > bestScore {
				bestScore = 0.8
			}
		}

		// Word overlap.
		overlap := wordOverlap(lower, desiredLower)
		if overlap > bestScore {
			bestScore = overlap
		}
	}

	return bestScore
}

func (m *Matcher) scoreSkillMatch(job models.Job, skills []string) float64 {
	if len(skills) == 0 {
		return 0
	}

	text := strings.ToLower(job.Title + " " + job.Description + " " + strings.Join(job.Tags, " "))

	matched := 0
	for _, skill := range skills {
		if strings.Contains(text, strings.ToLower(skill)) {
			matched++
		}
	}

	return float64(matched) / float64(len(skills))
}

func (m *Matcher) scoreLocationMatch(job models.Job, profile models.Profile) float64 {
	// Remote preference.
	if profile.RemoteOnly {
		if job.Remote {
			return 1.0
		}
		return 0.0
	}

	// If the job is remote, it's always a match.
	if job.Remote {
		return 1.0
	}

	// Location text match.
	if profile.Location != "" && job.Location != "" {
		profileLoc := strings.ToLower(profile.Location)
		jobLoc := strings.ToLower(job.Location)

		if strings.Contains(jobLoc, profileLoc) || strings.Contains(profileLoc, jobLoc) {
			return 1.0
		}

		// City-level match (check first word as rough city match).
		profileCity := strings.Split(profileLoc, ",")[0]
		jobCity := strings.Split(jobLoc, ",")[0]
		if strings.TrimSpace(profileCity) == strings.TrimSpace(jobCity) {
			return 0.8
		}
	}

	return 0.3 // unknown location gets a small default score
}

func (m *Matcher) scoreSalaryMatch(job models.Job, minSalary int) float64 {
	if minSalary == 0 {
		return 0.5 // no preference, neutral
	}
	if job.Salary.Max == 0 && job.Salary.Min == 0 {
		return 0.3 // no salary info, slight penalty
	}
	if job.Salary.Max >= minSalary {
		return 1.0
	}
	if job.Salary.Min >= minSalary {
		return 0.8
	}
	return 0.0 // below minimum
}

// wordOverlap computes the Jaccard similarity of words in two strings.
func wordOverlap(a, b string) float64 {
	wordsA := toWordSet(a)
	wordsB := toWordSet(b)

	intersection := 0
	for w := range wordsA {
		if wordsB[w] {
			intersection++
		}
	}

	union := len(wordsA)
	for w := range wordsB {
		if !wordsA[w] {
			union++
		}
	}

	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}

func toWordSet(s string) map[string]bool {
	words := strings.Fields(s)
	set := make(map[string]bool, len(words))
	for _, w := range words {
		// Skip tiny words (a, an, the, of, etc.)
		if len(w) > 2 {
			set[w] = true
		}
	}
	return set
}
