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
		if len(evidence.TitleAnchors) > 0 || len(evidence.ExplicitRoleSignals) >= 2 || (len(evidence.ExplicitRoleSignals) > 0 && len(evidence.SpecificSignals) > 0) {
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
	if containsRoleFamily(result.StrongFamilies, RoleFamilyPythonBackend) {
		filteredFamilies := result.Families[:0]
		for _, family := range result.Families {
			if family.Family != RoleFamilyWebBackend || family.Strength != RoleEvidenceStrong {
				filteredFamilies = append(filteredFamilies, family)
			}
		}
		result.Families = filteredFamilies
		filteredStrong := result.StrongFamilies[:0]
		for _, family := range result.StrongFamilies {
			if family != RoleFamilyWebBackend {
				filteredStrong = append(filteredStrong, family)
			}
		}
		result.StrongFamilies = filteredStrong
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

func appendUniqueRoleFamily(values []RoleFamily, additions ...RoleFamily) []RoleFamily {
	seen := map[RoleFamily]bool{}
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

func containsRoleFamily(values []RoleFamily, want RoleFamily) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func resumeSupportsRoleFamily(identity ResumeIdentity, family RoleFamily) bool {
	return containsRoleFamily(identity.PrimaryRoleFamilies, family) || containsRoleFamily(identity.SecondaryRoleFamilies, family)
}

func finalizeRouteDecision(v VacancyInput, resumes []ResumeProfile, evidence VacancyRoleEvidence, candidates []ResumeScore) RouteDecision {
	decision := RouteDecision{
		VacancyID:         v.ID,
		Status:            RouteNoResume,
		Confidence:        ConfidenceLow,
		Reasons:           []string{},
		ReasonCode:        RouteReasonNoStrong,
		AlternativeScores: append([]ResumeScore(nil), candidates...),
		RoleEvidence:      evidence,
	}
	if len(candidates) == 0 {
		decision.Reasons = append(decision.Reasons, "no enabled resume is available")
		return decision
	}

	compatible := make([]ResumeScore, 0, len(candidates))
	for _, candidate := range candidates {
		if len(candidate.HardBlockers) == 0 {
			compatible = append(compatible, candidate)
		}
	}
	if len(compatible) == 0 {
		decision.Status = RouteReviewRequired
		decision.ReasonCode = RouteReasonNoSuitable
		decision.Reasons = append(decision.Reasons, "all enabled resumes have explicit hard incompatibilities")
		for _, candidate := range candidates {
			decision.Reasons = append(decision.Reasons, candidate.Title+": "+strings.Join(candidate.HardBlockers, ", "))
		}
		decision.HardBlockers = append([]string(nil), candidates[0].HardBlockers...)
		return decision
	}
	decision.Score = compatible[0].Score
	decision.TopRawScore, decision.TopNormalizedScore = compatible[0].RawFit, compatible[0].NormalizedScore
	if len(compatible) > 1 {
		decision.SecondRawScore, decision.SecondNormalizedScore = compatible[1].RawFit, compatible[1].NormalizedScore
		decision.AbsoluteMargin = compatible[0].RawFit - compatible[1].RawFit
		if compatible[0].RawFit > 0 {
			decision.RelativeMargin = float64(decision.AbsoluteMargin) / float64(compatible[0].RawFit)
		}
	}

	if len(evidence.StrongFamilies) == 0 {
		decision.Status = RouteReviewRequired
		decision.ReasonCode = RouteReasonLowEvidence
		decision.Reasons = append(decision.Reasons, "vacancy role family cannot be determined with sufficient evidence")
		return decision
	}

	supportedStrong := []RoleFamily{}
	for _, family := range evidence.StrongFamilies {
		for _, resume := range resumes {
			if resume.Enabled && resumeSupportsRoleFamily(resume.IdentityOrDerived(), family) {
				supportedStrong = appendUniqueRoleFamily(supportedStrong, family)
				break
			}
		}
	}
	if len(supportedStrong) == 0 {
		decision.Status = RouteReviewRequired
		decision.ReasonCode = RouteReasonOutOfScope
		decision.Reasons = append(decision.Reasons, "vacancy role family is not supported by any enabled resume")
		return decision
	}
	if len(supportedStrong) > 1 {
		decision.Status = RouteReviewRequired
		decision.ReasonCode = RouteReasonAmbiguous
		decision.Reasons = append(decision.Reasons, "multiple supported role families have strong competing evidence")
		return decision
	}

	targetFamily := supportedStrong[0]
	qualified := make([]ResumeScore, 0, len(compatible))
	for _, candidate := range compatible {
		if !containsRoleFamily(candidate.MatchedRoleFamilies, targetFamily) || candidate.SpecificEvidenceCount == 0 || len(candidate.StrongRoleEvidence) == 0 || candidate.GenericEvidenceRatio >= 0.80 || len(candidate.MismatchSignals) > 0 || candidate.Score < 12 {
			continue
		}
		qualified = append(qualified, candidate)
	}
	if len(qualified) == 0 {
		decision.Status = RouteReviewRequired
		decision.ReasonCode = RouteReasonNoSuitable
		decision.Reasons = append(decision.Reasons, "supported role family has no resume above the evidence floor")
		return decision
	}
	if len(qualified) > 1 {
		fitMargin := qualified[0].FitScore - qualified[1].FitScore
		fitRelative := float64(fitMargin) / float64(maxInt(qualified[0].FitScore, 1))
		if fitMargin < 10 || fitRelative < 0.12 {
			decision.Status = RouteReviewRequired
			decision.ReasonCode = RouteReasonAmbiguous
			decision.Reasons = append(decision.Reasons, "multiple resumes have competing strong evidence for the supported role family")
			return decision
		}
	}

	selected := qualified[0]
	decision.Status = RouteSelected
	decision.ReasonCode = RouteReasonSelected
	decision.SelectedResumeID, decision.SelectedResumeTitle = selected.ResumeID, selected.Title
	decision.Score = selected.Score
	decision.HardRequirements = requirementStates(v, selected, resumes)
	for _, requirement := range decision.HardRequirements {
		if requirement.Status == "met" {
			decision.Reasons = append(decision.Reasons, "selected resume covers "+requirement.Requirement)
		} else {
			decision.Reasons = append(decision.Reasons, "hard requirement remains unknown: "+requirement.Requirement)
		}
	}
	decision.Reasons = append(decision.Reasons, "selected by role-family evidence and specific supporting signals")
	if decision.Score >= 60 && (len(decision.HardRequirements) == 0 || allRequirementsMet(decision.HardRequirements)) {
		decision.Confidence = ConfidenceHigh
	} else {
		decision.Confidence = ConfidenceMedium
	}
	return decision
}

func (resume ResumeProfile) IdentityOrDerived() ResumeIdentity {
	identity := resume.Identity
	if len(identity.PrimaryRoleFamilies) == 0 {
		identity = DeriveResumeIdentity(resume)
	}
	return identity
}
