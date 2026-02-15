package resume

import (
	"regexp"
	"strings"

	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

// Parse extracts a structured Profile from raw resume text.
// This is a heuristic parser for standard resume formats — it looks for
// section headers and extracts content under each one.
func Parse(text string) models.Profile {
	var profile models.Profile

	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return profile
	}

	// Pass 1: extract contact info from the top few lines.
	profile.Name, profile.Email, profile.Phone, profile.Location = parseContactBlock(lines)

	// Pass 2: identify sections and extract structured data.
	sections := splitSections(lines)

	if content, ok := sections["technical skills"]; ok {
		profile.Skills = parseSkills(content)
	}

	if content, ok := sections["professional experience"]; ok {
		profile.Experience = parseExperience(content)
	}

	if content, ok := sections["education"]; ok {
		profile.Education = parseEducation(content)
	}

	return profile
}

var (
	emailRe = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	phoneRe = regexp.MustCompile(`\(?\d{3}\)?[\s.\-]?\d{3}[\s.\-]?\d{4}`)
)

func parseContactBlock(lines []string) (name, email, phone, location string) {
	// The first non-empty line is usually the name.
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			name = line
			break
		}
	}

	// Scan the first ~5 lines for email, phone, location.
	top := lines
	if len(top) > 8 {
		top = top[:8]
	}
	block := strings.Join(top, " ")

	if m := emailRe.FindString(block); m != "" {
		email = m
	}
	if m := phoneRe.FindString(block); m != "" {
		phone = m
	}

	// Location heuristic: look for "City, State" or "City State" patterns.
	locRe := regexp.MustCompile(`(?i)([A-Z][a-zA-Z\s]+,?\s*(?:AL|AK|AZ|AR|CA|CO|CT|DE|FL|GA|HI|ID|IL|IN|IA|KS|KY|LA|ME|MD|MA|MI|MN|MS|MO|MT|NE|NV|NH|NJ|NM|NY|NC|ND|OH|OK|OR|PA|RI|SC|SD|TN|TX|UT|VT|VA|WA|WV|WI|WY|Alabama|Alaska|Arizona|Arkansas|California|Colorado|Connecticut|Delaware|Florida|Georgia|Hawaii|Idaho|Illinois|Indiana|Iowa|Kansas|Kentucky|Louisiana|Maine|Maryland|Massachusetts|Michigan|Minnesota|Mississippi|Missouri|Montana|Nebraska|Nevada|New Hampshire|New Jersey|New Mexico|New York|North Carolina|North Dakota|Ohio|Oklahoma|Oregon|Pennsylvania|Rhode Island|South Carolina|South Dakota|Tennessee|Texas|Utah|Vermont|Virginia|Washington|West Virginia|Wisconsin|Wyoming))`)
	if m := locRe.FindStringSubmatch(block); len(m) > 1 {
		location = strings.TrimSpace(m[1])
	}

	return
}

// section header patterns
var sectionHeaders = []string{
	"professional summary",
	"technical skills",
	"professional experience",
	"education",
	"certifications",
	"projects",
}

func splitSections(lines []string) map[string][]string {
	sections := make(map[string][]string)
	currentSection := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		// Check if this line is a section header.
		isHeader := false
		for _, header := range sectionHeaders {
			if lower == header || strings.HasPrefix(lower, header) {
				currentSection = header
				isHeader = true
				break
			}
		}

		if !isHeader && currentSection != "" && trimmed != "" {
			sections[currentSection] = append(sections[currentSection], trimmed)
		}
	}

	return sections
}

func parseSkills(lines []string) []string {
	var skills []string
	seen := make(map[string]bool)

	for _, line := range lines {
		// Remove bullet markers and category labels (e.g., "Languages: Python, C#")
		line = strings.TrimLeft(line, "*•-– ")

		// Split on colon to separate category from skills.
		if idx := strings.Index(line, ":"); idx != -1 {
			line = line[idx+1:]
		}

		// Split by comma, slash, or pipe.
		parts := regexp.MustCompile(`[,/|]`).Split(line, -1)
		for _, part := range parts {
			skill := strings.TrimSpace(part)
			// Filter out noise — very short strings, generic terms.
			if len(skill) < 2 || len(skill) > 60 {
				continue
			}
			lower := strings.ToLower(skill)
			if seen[lower] {
				continue
			}
			seen[lower] = true
			skills = append(skills, skill)
		}
	}

	return skills
}

var dateRangeRe = regexp.MustCompile(`(?i)((?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\w*\.?\s+\d{4})\s*[–\-—]\s*((?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\w*\.?\s+\d{4}|Present)`)

func parseExperience(lines []string) []models.Experience {
	var experiences []models.Experience
	var current *models.Experience

	for _, line := range lines {
		// Check if this line contains a date range — indicates a new entry.
		if m := dateRangeRe.FindStringSubmatch(line); len(m) > 2 {
			if current != nil {
				experiences = append(experiences, *current)
			}

			current = &models.Experience{
				StartDate: m[1],
			}
			if strings.EqualFold(m[2], "Present") {
				current.EndDate = ""
			} else {
				current.EndDate = m[2]
			}

			// Extract company and title from the line.
			// Common formats: "Company | Location  Title | Date"
			// or "Company — Location  Title | Date"
			beforeDate := line[:strings.Index(line, m[0])]
			parts := regexp.MustCompile(`[|–—]`).Split(beforeDate, -1)

			if len(parts) >= 1 {
				current.Company = strings.TrimSpace(parts[0])
			}
			// Look for title in remaining parts — skip location-like strings.
			locTest := regexp.MustCompile(`(?i)^[A-Za-z\s]+,\s*[A-Z]{2}$`)
			for _, p := range parts[1:] {
				p = strings.TrimSpace(p)
				if p == "" || locTest.MatchString(p) {
					continue
				}
				current.Title = p
			}
			continue
		}

		// Bullet point under current experience.
		if current != nil && strings.HasPrefix(line, "*") {
			bullet := strings.TrimLeft(line, "* ")
			if current.Description == "" {
				current.Description = bullet
			} else {
				current.Description += "; " + bullet
			}
		}
	}

	if current != nil {
		experiences = append(experiences, *current)
	}

	return experiences
}

func parseEducation(lines []string) []models.Education {
	var education []models.Education

	for _, line := range lines {
		lower := strings.ToLower(line)

		// Look for degree indicators.
		if strings.Contains(lower, "bachelor") || strings.Contains(lower, "master") ||
			strings.Contains(lower, "associate") || strings.Contains(lower, "b.s.") ||
			strings.Contains(lower, "m.s.") || strings.Contains(lower, "ph.d") {

			edu := models.Education{}

			// Extract degree.
			degreeRe := regexp.MustCompile(`(?i)(Bachelor\s+of\s+\w+|Master\s+of\s+\w+|B\.S\.|M\.S\.|Ph\.D\.?)`)
			if m := degreeRe.FindString(line); m != "" {
				edu.Degree = m
			}

			// Extract field — look for "in <field>" after the degree.
			fieldRe := regexp.MustCompile(`(?i)\bin\s+([^(|]+?)(?:\s*\(|\s*\||$)`)
			if m := fieldRe.FindStringSubmatch(line); len(m) > 1 {
				edu.Field = strings.TrimSpace(m[1])
			}

			// Extract date.
			dateRe := regexp.MustCompile(`(?:May|June?|Dec\w*|Aug\w*|Jan\w*|Feb\w*|Mar\w*|Apr\w*|Jul\w*|Sep\w*|Oct\w*|Nov\w*)\s+\d{4}`)
			if m := dateRe.FindString(line); m != "" {
				edu.EndDate = m
			}

			education = append(education, edu)
			continue
		}

		// Institution line — usually the line before the degree line,
		// or contains "University" / "College".
		if (strings.Contains(lower, "university") || strings.Contains(lower, "college") ||
			strings.Contains(lower, "institute")) && len(education) == 0 {
			edu := models.Education{Institution: strings.TrimSpace(line)}
			education = append(education, edu)
		} else if len(education) > 0 && education[len(education)-1].Institution == "" {
			// Backfill institution from a prior line.
			if strings.Contains(lower, "university") || strings.Contains(lower, "college") {
				education[len(education)-1].Institution = strings.TrimSpace(line)
			}
		}
	}

	return education
}
