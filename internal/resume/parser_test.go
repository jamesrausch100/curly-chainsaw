package resume

import (
	"strings"
	"testing"
)

const jamesResume = `JAMES NICHOLAS RAUSCH
Denver Colorado | (505) 529-9669 | JamesRausch100@gmail.com
PROFESSIONAL SUMMARY
Operations & Automation Engineer with significant cross-domain project management skills amid a computer science background leading to demonstrated success deploying automation solutions as a team within high-volume semiconductor manufacturing. Proven ability to design, test, and migrate factory systems using Python, C#, React, and Selenium, delivering measurable improvements in cycle time, SOP deployment, and work-content reduction during my time at Intel and TSMC. Seeking roles in operation management, automation/test engineering and process optimization. Available for both remote and in office work five days a week & on call availability.
TECHNICAL SKILLS
* Languages: Python, C#, C++, JavaScript, SQL
* Web & Infrastructure: React, Selenium, AWS (S3), Hadoop, Spark, Maven, Git/GitLab, DNS, PfSense
* Data & Analysis: Microsoft SQL Server, SharePoint, PowerBI, Excel, Prompt Engineering
* Manufacturing & Process: Cycle Time Reduction, KPI Management, 6S, ISO 9001, Digital Twin Infrastructure, Fault Detection, Work Content Reduction, Project Management, People Management, Delegation, Site Operations, Engineering Resource Management
* Automation & Systems: Test Automation, Manufacturing Systems Integration, Workflow Automation, SOP Digitization, Factory Tooling
PROFESSIONAL EXPERIENCE
Market2Agent.com | Denver, CO Design Architect & Systems Lead | Jan 2026 – Present
* Leading end-to-end architecture of a platform that enables businesses to be discovered, interpreted, and recommended by AI agents.
* Designed and implemented multi-model LLM orchestration pipelines.
* Engineered GEO (Generative Engine Optimization) audit systems.
TSMC| Phoenix, AZ Intelligent Manufacturing Engineer Manufacturing Excellence Program| Sept 2024 – July 2025
* Reduced SOP deployment cycle time by 50%.
* Local architectural planning for a 4D digital twin infrastructure.
* Improved baseline performance for automated running-hold workflows by more than 20%.
Intel Corporation | Chandler, AZ Engineer/Manufacturing Systems & Automation Intern| Jan 2023 – May 2024
* Built a React-based web application to manage local work-content reduction submissions.
* Automated unit testing for reticle and mask management systems by writing over 400 Python and Selenium test cases.
* Migrated end-of-life services for the DOMA metrology toolkit (LITEL).
CodaKid Corporation — Scottsdale, AZ  K–8 Computer Science Tutor | Oct 2021 - Jan 2023
* Provided one-on-one and small-group tutoring to students ages 8–14.
EDUCATION
Arizona State University (ASU) | Tempe, AZ Bachelor of Science in Computer Science (Focus: Cybersecurity) | May 2024
* Honors: Cum Laude (3.72 GPA)`

func TestParse_ContactInfo(t *testing.T) {
	profile := Parse(jamesResume)

	if profile.Name != "JAMES NICHOLAS RAUSCH" {
		t.Fatalf("name: got %q", profile.Name)
	}
	if profile.Email != "JamesRausch100@gmail.com" {
		t.Fatalf("email: got %q", profile.Email)
	}
	if profile.Phone != "(505) 529-9669" {
		t.Fatalf("phone: got %q", profile.Phone)
	}
	if !strings.Contains(profile.Location, "Denver") {
		t.Fatalf("location should contain Denver: got %q", profile.Location)
	}
}

func TestParse_Skills(t *testing.T) {
	profile := Parse(jamesResume)

	if len(profile.Skills) == 0 {
		t.Fatal("expected skills to be parsed")
	}

	// Check a few specific skills are present.
	skillSet := make(map[string]bool)
	for _, s := range profile.Skills {
		skillSet[strings.ToLower(s)] = true
	}

	for _, expected := range []string{"python", "react", "sql", "selenium"} {
		if !skillSet[expected] {
			t.Errorf("missing expected skill: %s", expected)
		}
	}

	t.Logf("Parsed %d skills: %v", len(profile.Skills), profile.Skills)
}

func TestParse_Experience(t *testing.T) {
	profile := Parse(jamesResume)

	if len(profile.Experience) == 0 {
		t.Fatal("expected experience to be parsed")
	}

	// Should have 4 entries.
	if len(profile.Experience) < 3 {
		t.Fatalf("expected at least 3 experience entries, got %d", len(profile.Experience))
	}

	// First entry should be Market2Agent (most recent).
	first := profile.Experience[0]
	if !strings.Contains(first.Company, "Market2Agent") {
		t.Errorf("first company: got %q", first.Company)
	}

	for i, exp := range profile.Experience {
		t.Logf("Experience[%d]: %s at %s (%s - %s)", i, exp.Title, exp.Company, exp.StartDate, exp.EndDate)
	}
}

func TestParse_Education(t *testing.T) {
	profile := Parse(jamesResume)

	if len(profile.Education) == 0 {
		t.Fatal("expected education to be parsed")
	}

	edu := profile.Education[0]
	if !strings.Contains(edu.Degree, "Bachelor") {
		t.Errorf("degree: got %q", edu.Degree)
	}
	if !strings.Contains(edu.Field, "Computer Science") {
		t.Errorf("field: got %q", edu.Field)
	}

	t.Logf("Education: %s in %s (%s)", edu.Degree, edu.Field, edu.EndDate)
}

func TestParse_EmptyInput(t *testing.T) {
	profile := Parse("")
	if profile.Name != "" {
		t.Fatalf("expected empty profile for empty input, got name: %q", profile.Name)
	}
}
