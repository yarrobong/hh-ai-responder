// Package candidateparity defines the semantic equality contract used by the
// Candidate migration verifier and import idempotency checks.
package candidateparity

import (
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"time"

	"hh-ai-responder/internal/candidate"
)

// Difference is intentionally value-free. Parity diagnostics identify the
// affected field without copying private candidate data into reports/logs.
type Difference struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
}

// Result is the structured result of Candidate semantic comparison.
type Result struct {
	Equivalent  bool         `json:"equivalent"`
	Differences []Difference `json:"differences,omitempty"`
}

// CompareCandidateSemantic compares Candidate knowledge, not its JSON
// representation. Typed instants are compared after conversion to UTC,
// identity collections are represented by stable identity keys, JSON objects
// are compared as maps, and all other arrays retain their order.
func CompareCandidateSemantic(source, persisted candidate.Candidate) Result {
	left, leftErr := semanticValue(source)
	right, rightErr := semanticValue(persisted)
	result := Result{Equivalent: false, Differences: []Difference{}}
	if leftErr != nil {
		result.Differences = append(result.Differences, Difference{Path: "candidate", Kind: "normalization_error", Detail: "source candidate cannot be represented semantically"})
	}
	if rightErr != nil {
		result.Differences = append(result.Differences, Difference{Path: "candidate", Kind: "normalization_error", Detail: "persisted candidate cannot be represented semantically"})
	}
	if leftErr != nil || rightErr != nil {
		return result
	}

	var differences []Difference
	differences = append(differences, duplicateIdentityDifferences(source, "source")...)
	differences = append(differences, duplicateIdentityDifferences(persisted, "persisted")...)
	diffJSON("candidate", left, right, &differences)
	result.Differences = differences
	result.Equivalent = len(differences) == 0
	return result
}

// Equivalent is the shared boolean form used by persistence idempotency.
func Equivalent(left, right candidate.Candidate) bool {
	return CompareCandidateSemantic(left, right).Equivalent
}

var timeType = reflect.TypeOf(time.Time{})

func semanticValue(value candidate.Candidate) (any, error) {
	// JSON round-trip makes the comparison follow the persisted wire shape,
	// while the typed clone still lets us normalize only time.Time values. Date
	// and period strings remain ordinary strings and are never guessed as dates.
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var clone candidate.Candidate
	if err := json.Unmarshal(raw, &clone); err != nil {
		return nil, err
	}
	normalizeTimes(reflect.ValueOf(&clone).Elem())
	raw, err = json.Marshal(clone)
	if err != nil {
		return nil, err
	}
	var result any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	normalizeCandidateCollections(result)
	return result, nil
}

func normalizeTimes(value reflect.Value) {
	if !value.IsValid() {
		return
	}
	if value.Type() == timeType {
		if value.CanSet() {
			value.Set(reflect.ValueOf(value.Interface().(time.Time).UTC()))
		}
		return
	}
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !value.IsNil() {
			normalizeTimes(value.Elem())
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			field := value.Field(index)
			if field.CanSet() {
				normalizeTimes(field)
			}
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			normalizeTimes(value.Index(index))
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			normalizeTimes(value.MapIndex(key))
		}
	}
}

func normalizeCandidateCollections(value any) {
	root, ok := value.(map[string]any)
	if !ok {
		return
	}
	for _, field := range []string{
		"contacts", "external_references", "education", "languages", "experience",
		"skills", "projects", "achievements", "preferences", "constraints",
		"stories", "claims", "unknowns", "proposals",
	} {
		convertEntityCollection(root, field)
	}

	if skills, ok := root["skills"].(map[string]any); ok {
		for _, item := range skills {
			if skill, ok := item.(map[string]any); ok {
				for _, field := range []string{"source_ids", "claim_ids"} {
					convertStringIdentityCollection(skill, field)
				}
				convertEntityCollection(skill, "capabilities")
				convertEntityCollection(skill, "uses")
				convertEntityCollection(skill, "source_assertions")
				if assertions, ok := skill["source_assertions"].(map[string]any); ok {
					for _, assertion := range assertions {
						if value, ok := assertion.(map[string]any); ok {
							convertStringIdentityCollection(value, "projects")
							convertEntityCollection(value, "uses")
						}
					}
				}
			}
		}
	}
	if projects, ok := root["projects"].(map[string]any); ok {
		for _, item := range projects {
			if project, ok := item.(map[string]any); ok {
				for _, field := range []string{"source_ids", "claim_ids", "related_skills", "achievement_ids", "story_ids"} {
					convertStringIdentityCollection(project, field)
				}
				convertEntityCollection(project, "skill_uses")
			}
		}
	}
	if experience, ok := root["experience"].(map[string]any); ok {
		for _, item := range experience {
			if value, ok := item.(map[string]any); ok {
				convertEntityCollection(value, "skills_used")
				convertStringIdentityCollection(value, "story_ids")
			}
		}
	}
}

func convertEntityCollection(parent map[string]any, field string) {
	items, ok := parent[field].([]any)
	if !ok {
		return
	}
	converted := make(map[string]any, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			converted["#"+strconv.Itoa(index)] = item
			continue
		}
		id, ok := object["id"].(string)
		if !ok || id == "" {
			converted["#"+strconv.Itoa(index)] = object
			continue
		}
		converted[id] = object
	}
	parent[field] = converted
}

func convertStringIdentityCollection(parent map[string]any, field string) {
	items, ok := parent[field].([]any)
	if !ok {
		return
	}
	converted := make(map[string]any, len(items))
	for index, item := range items {
		key, ok := item.(string)
		if !ok || key == "" {
			key = "#" + strconv.Itoa(index)
		}
		converted[key] = item
	}
	parent[field] = converted
}

func duplicateIdentityDifferences(value candidate.Candidate, side string) []Difference {
	differences := []Difference{}
	check := func(path string, ids []string) {
		seen := map[string]bool{}
		for _, id := range ids {
			if id == "" || !seen[id] {
				seen[id] = true
				continue
			}
			differences = append(differences, Difference{Path: path + "[" + id + "]", Kind: "duplicate_identity", Detail: side + " collection contains a duplicate identity"})
		}
	}
	check("candidate.contacts", idsOf(value.Contacts))
	check("candidate.external_references", idsOf(value.ExternalReferences))
	check("candidate.education", idsOf(value.Education))
	check("candidate.languages", idsOf(value.Languages))
	check("candidate.experience", idsOf(value.Experience))
	check("candidate.skills", idsOf(value.Skills))
	check("candidate.projects", idsOf(value.Projects))
	check("candidate.achievements", idsOf(value.Achievements))
	check("candidate.preferences", idsOf(value.Preferences))
	check("candidate.constraints", idsOf(value.Constraints))
	check("candidate.stories", idsOf(value.Stories))
	check("candidate.claims", idsOf(value.Claims))
	check("candidate.unknowns", idsOf(value.Unknowns))
	check("candidate.proposals", idsOf(value.Proposals))
	for _, skill := range value.Skills {
		check("candidate.skills["+skill.ID+"].capabilities", idsOf(skill.Capabilities))
		check("candidate.skills["+skill.ID+"].uses", idsOf(skill.Uses))
		check("candidate.skills["+skill.ID+"].source_assertions", idsOf(skill.SourceAssertions))
		for _, assertion := range skill.SourceAssertions {
			check("candidate.skills["+skill.ID+"].source_assertions["+assertion.ID+"].uses", idsOf(assertion.Uses))
		}
	}
	for _, project := range value.Projects {
		check("candidate.projects["+project.ID+"].skill_uses", idsOf(project.SkillUses))
	}
	for _, experience := range value.Experience {
		check("candidate.experience["+experience.ID+"].skills_used", idsOf(experience.SkillsUsed))
	}
	return differences
}

func idsOf[T any](values []T) []string {
	ids := make([]string, 0, len(values))
	for _, value := range values {
		ids = append(ids, identityOf(value))
	}
	return ids
}

func identityOf[T any](value T) string {
	switch value := any(value).(type) {
	case candidate.CanonicalCandidateContact:
		return value.ID
	case candidate.CanonicalExternalReference:
		return value.ID
	case candidate.CanonicalCandidateEducation:
		return value.ID
	case candidate.CanonicalCandidateLanguage:
		return value.ID
	case candidate.CanonicalCandidateExperience:
		return value.ID
	case candidate.CanonicalCandidateSkill:
		return value.ID
	case candidate.CanonicalSkillCapability:
		return value.ID
	case candidate.CanonicalSkillUse:
		return value.ID
	case candidate.CanonicalCandidateSkillAssertion:
		return value.ID
	case candidate.CanonicalCandidateProject:
		return value.ID
	case candidate.CandidateAchievement:
		return value.ID
	case candidate.CanonicalCandidatePreference:
		return value.ID
	case candidate.CanonicalCandidateConstraint:
		return value.ID
	case candidate.CanonicalCandidateStory:
		return value.ID
	case candidate.CanonicalCandidateClaim:
		return value.ID
	case candidate.CandidateUnknown:
		return value.ID
	case candidate.KnowledgeProposal:
		return value.ID
	default:
		return ""
	}
}

func diffJSON(path string, left, right any, differences *[]Difference) {
	if scalarEqual(left, right) {
		return
	}
	leftMap, leftIsMap := left.(map[string]any)
	rightMap, rightIsMap := right.(map[string]any)
	if leftIsMap && rightIsMap {
		keys := make(map[string]bool, len(leftMap)+len(rightMap))
		for key := range leftMap {
			keys[key] = true
		}
		for key := range rightMap {
			keys[key] = true
		}
		ordered := make([]string, 0, len(keys))
		for key := range keys {
			ordered = append(ordered, key)
		}
		sort.Strings(ordered)
		for _, key := range ordered {
			leftValue, leftOK := leftMap[key]
			rightValue, rightOK := rightMap[key]
			childPath := path + "." + key
			if !leftOK {
				*differences = append(*differences, Difference{Path: childPath, Kind: "extra_entity", Detail: "persisted value is not present in source"})
				continue
			}
			if !rightOK {
				*differences = append(*differences, Difference{Path: childPath, Kind: "missing_entity", Detail: "persisted value is missing"})
				continue
			}
			diffJSON(childPath, leftValue, rightValue, differences)
		}
		return
	}
	leftSlice, leftIsSlice := left.([]any)
	rightSlice, rightIsSlice := right.([]any)
	if leftIsSlice && rightIsSlice {
		if len(leftSlice) != len(rightSlice) {
			*differences = append(*differences, Difference{Path: path, Kind: "ordered_length_mismatch", Detail: "ordered collection length differs"})
		}
		limit := len(leftSlice)
		if len(rightSlice) < limit {
			limit = len(rightSlice)
		}
		for index := 0; index < limit; index++ {
			diffJSON(path+"["+strconv.Itoa(index)+"]", leftSlice[index], rightSlice[index], differences)
		}
		return
	}
	*differences = append(*differences, Difference{Path: path, Kind: "value_mismatch", Detail: "semantic value differs"})
}

func scalarEqual(left, right any) bool {
	switch left := left.(type) {
	case nil:
		return right == nil
	case bool:
		value, ok := right.(bool)
		return ok && left == value
	case string:
		value, ok := right.(string)
		return ok && left == value
	case float64:
		value, ok := right.(float64)
		return ok && left == value
	default:
		return false
	}
}
