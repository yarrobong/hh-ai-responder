package careeragent

import "strings"

// RoleFamily is a bounded semantic role category used only for deterministic
// resume routing. It is not a claim about candidate experience beyond the
// trusted resume fields from which it was derived.
type RoleFamily string

const (
	RoleFamilyPythonBackend          RoleFamily = "PYTHON_BACKEND"
	RoleFamilyWebBackend             RoleFamily = "WEB_BACKEND"
	RoleFamilyAutomationIntegrations RoleFamily = "AUTOMATION_INTEGRATIONS"
	RoleFamilyTechSupport            RoleFamily = "TECH_SUPPORT"
	RoleFamilySystemAdmin            RoleFamily = "SYSTEM_ADMIN"
	RoleFamilyFrontend               RoleFamily = "FRONTEND"
	RoleFamilyFlutter                RoleFamily = "FLUTTER"
	RoleFamilyOneC                   RoleFamily = "ONE_C"
	RoleFamilySystemAnalyst          RoleFamily = "SYSTEM_ANALYST"
)

type RoleEvidenceStrength string

const (
	RoleEvidenceNone   RoleEvidenceStrength = "NONE"
	RoleEvidenceWeak   RoleEvidenceStrength = "WEAK"
	RoleEvidenceStrong RoleEvidenceStrength = "STRONG"
)

type RoleFamilyEvidence struct {
	Family              RoleFamily           `json:"family"`
	Strength            RoleEvidenceStrength `json:"strength"`
	TitleAnchors        []string             `json:"title_anchors,omitempty"`
	ExplicitRoleSignals []string             `json:"explicit_role_signals,omitempty"`
	SpecificSignals     []string             `json:"specific_signals,omitempty"`
	GenericSignals      []string             `json:"generic_signals,omitempty"`
	MismatchSignals     []string             `json:"mismatch_signals,omitempty"`
}

type VacancyRoleEvidence struct {
	Families          []RoleFamilyEvidence `json:"families,omitempty"`
	StrongFamilies    []RoleFamily         `json:"strong_families,omitempty"`
	SpecificSignals   []string             `json:"specific_signals,omitempty"`
	GenericSignals    []string             `json:"generic_signals,omitempty"`
	EvidenceAvailable bool                 `json:"evidence_available"`
}

type roleFamilyRule struct {
	family         RoleFamily
	titleAnchors   []string
	roleTokens     []string
	specificTokens []string
	genericTokens  []string
	mismatchTokens []string
}

var reset6RoleFamilyRules = []roleFamilyRule{
	{
		family:         RoleFamilyPythonBackend,
		titleAnchors:   []string{"python developer", "python backend", "django developer", "backend python"},
		roleTokens:     []string{"python", "django", "backend"},
		specificTokens: []string{"python", "django", "postgresql", "rest_api", "celery"},
		genericTokens:  []string{"api", "linux", "sql", "docker", "git"},
	},
	{
		family:         RoleFamilyWebBackend,
		titleAnchors:   []string{"backend developer", "web developer", "fullstack developer"},
		roleTokens:     []string{"backend", "developer", "fullstack"},
		specificTokens: []string{"php", "laravel", "vuejs", "react", "nodejs", "express", "soap"},
		genericTokens:  []string{"api", "linux", "sql", "docker", "git"},
	},
	{
		family:         RoleFamilyAutomationIntegrations,
		titleAnchors:   []string{"automation engineer", "integration specialist", "implementation specialist"},
		roleTokens:     []string{"automation", "integration", "implementation"},
		specificTokens: []string{"automation", "integration", "implementation", "webhooks", "bitrix24", "crm"},
		genericTokens:  []string{"api", "linux", "sql", "docker", "git"},
	},
	{
		family:         RoleFamilyTechSupport,
		titleAnchors:   []string{"technical support", "support engineer", "first line support", "application support"},
		roleTokens:     []string{"support", "troubleshooting", "diagnostics", "installation"},
		specificTokens: []string{"support", "troubleshooting", "diagnostics", "installation", "crm"},
		genericTokens:  []string{"api", "linux", "sql", "docker", "git"},
	},
	{
		family:         RoleFamilySystemAdmin,
		titleAnchors:   []string{"system administrator", "linux administrator", "sysadmin"},
		roleTokens:     []string{"administrator", "sysadmin", "devops"},
		specificTokens: []string{"linux", "dns", "network", "devops", "kubernetes"},
		genericTokens:  []string{"sql", "git", "api"},
	},
	{
		family:         RoleFamilyFrontend,
		titleAnchors:   []string{"frontend developer", "front end developer", "vue developer", "react developer"},
		roleTokens:     []string{"frontend", "vuejs", "react"},
		specificTokens: []string{"vuejs", "react", "typescript", "javascript", "html", "css"},
		genericTokens:  []string{"api", "git", "docker"},
	},
	{
		family:         RoleFamilyFlutter,
		titleAnchors:   []string{"flutter developer", "flutter engineer"},
		roleTokens:     []string{"flutter"},
		specificTokens: []string{"flutter", "dart"},
		genericTokens:  []string{"api", "git"},
	},
	{
		family:         RoleFamilyOneC,
		titleAnchors:   []string{"1c developer", "1c consultant", "one c developer"},
		roleTokens:     []string{"onec", "1c"},
		specificTokens: []string{"onec", "1c"},
		genericTokens:  []string{"sql", "api", "git"},
	},
	{
		family:         RoleFamilySystemAnalyst,
		titleAnchors:   []string{"system analyst", "системный аналитик"},
		roleTokens:     []string{"analyst", "system"},
		specificTokens: []string{"xml", "soap", "sql"},
		genericTokens:  []string{"api", "git"},
	},
}

var reset6ResumeStrongAnchorTokens = map[string]bool{
	"python": true, "django": true, "backend": true, "automation": true,
	"integration": true, "implementation": true, "support": true,
	"troubleshooting": true, "diagnostics": true, "installation": true,
	"php": true, "laravel": true, "soap": true, "react": true,
	"vuejs": true, "frontend": true, "flutter": true, "dart": true,
	"onec": true, "1c": true, "administrator": true, "sysadmin": true,
	"analyst": true,
}

// ClassifyVacancyRole determines role families from vacancy evidence first.
// The resumes argument is retained for API compatibility and future bounded
// context, but it must never filter or hide an otherwise strong family.
func ClassifyVacancyRole(v VacancyInput, resumes []ResumeProfile) VacancyRoleEvidence {
	_ = resumes
	titleText := strings.Join([]string{v.Title, strings.Join(v.ProfessionalRoles, " ")}, " ")
	structuredText := strings.Join([]string{v.Title, strings.Join(v.ProfessionalRoles, " "), strings.Join(v.RequiredSkills, " "), strings.Join(v.KeySkills, " ")}, " ")
	allText := strings.Join([]string{structuredText, v.Description}, " ")
	titleTokens := tokens(titleText)
	structuredTokens := tokens(structuredText)
	allTokens := tokens(allText)

	result := VacancyRoleEvidence{}
	for _, rule := range reset6RoleFamilyRules {
		evidence := RoleFamilyEvidence{Family: rule.family}
		for _, phrase := range rule.titleAnchors {
			if containsCanonicalPhrase(titleText, phrase) {
				evidence.TitleAnchors = appendUniqueCanonical(evidence.TitleAnchors, phrase)
			}
		}
		for _, token := range rule.roleTokens {
			if titleTokens[token] {
				evidence.ExplicitRoleSignals = appendUniqueCanonical(evidence.ExplicitRoleSignals, token)
			}
		}
		for _, token := range rule.specificTokens {
			if structuredTokens[token] || allTokens[token] {
				evidence.SpecificSignals = appendUniqueCanonical(evidence.SpecificSignals, token)
			}
		}
		for _, token := range rule.genericTokens {
			if structuredTokens[token] || allTokens[token] {
				evidence.GenericSignals = appendUniqueCanonical(evidence.GenericSignals, token)
			}
		}
		if len(evidence.TitleAnchors) > 0 || len(evidence.ExplicitRoleSignals) >= 2 {
			evidence.Strength = RoleEvidenceStrong
		} else if len(evidence.ExplicitRoleSignals) > 0 || len(evidence.SpecificSignals) >= 2 {
			evidence.Strength = RoleEvidenceWeak
		} else {
			continue
		}
		result.Families = append(result.Families, evidence)
		result.SpecificSignals = appendUniqueStrings(result.SpecificSignals, evidence.SpecificSignals...)
		result.GenericSignals = appendUniqueStrings(result.GenericSignals, evidence.GenericSignals...)
		if evidence.Strength == RoleEvidenceStrong {
			result.StrongFamilies = append(result.StrongFamilies, evidence.Family)
		}
	}
	result.EvidenceAvailable = len(result.Families) > 0 || len(result.GenericSignals) > 0
	return result
}

func resumeRoleFamilies(resume ResumeProfile) (primary, secondary []RoleFamily) {
	text := strings.Join([]string{resume.Title, resume.DesiredRole, strings.Join(resume.Skills, " ")}, " ")
	tokensValue := tokens(text)
	add := func(values *[]RoleFamily, family RoleFamily) {
		for _, existing := range *values {
			if existing == family {
				return
			}
		}
		*values = append(*values, family)
	}
	if (tokensValue["python"] || tokensValue["django"]) && (tokensValue["backend"] || tokensValue["developer"]) {
		add(&primary, RoleFamilyPythonBackend)
	}
	if tokensValue["backend"] || tokensValue["developer"] || tokensValue["php"] || tokensValue["laravel"] {
		add(&primary, RoleFamilyWebBackend)
	}
	if tokensValue["automation"] || tokensValue["integration"] || tokensValue["implementation"] {
		add(&primary, RoleFamilyAutomationIntegrations)
	}
	if tokensValue["support"] || tokensValue["troubleshooting"] || tokensValue["diagnostics"] {
		add(&primary, RoleFamilyTechSupport)
	}
	if tokensValue["administrator"] || tokensValue["sysadmin"] || tokensValue["devops"] {
		add(&primary, RoleFamilySystemAdmin)
	}
	if tokensValue["frontend"] || tokensValue["vuejs"] || tokensValue["react"] {
		add(&primary, RoleFamilyFrontend)
	}
	if tokensValue["flutter"] || tokensValue["dart"] {
		add(&primary, RoleFamilyFlutter)
	}
	if tokensValue["onec"] || tokensValue["1c"] {
		add(&primary, RoleFamilyOneC)
	}
	if tokensValue["analyst"] && tokensValue["system"] {
		add(&primary, RoleFamilySystemAnalyst)
	}
	if len(primary) > 1 {
		secondary = append(secondary, primary[1:]...)
		primary = primary[:1]
	}
	return primary, secondary
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range additions {
		if value != "" && !seen[value] {
			values = append(values, value)
			seen[value] = true
		}
	}
	return values
}
