package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
)

// MigrationStatus is deliberately data-oriented. A plan is built before any
// PostgreSQL transaction is opened, and only new/compatible items can be
// selected for import.
type MigrationStatus string

const (
	MigrationNew            MigrationStatus = "new"
	MigrationAlreadyPresent MigrationStatus = "already_present"
	MigrationCompatible     MigrationStatus = "compatible"
)

const (
	migrationWarning  = "warning"
	migrationConflict = "conflict"
	migrationCritical = "critical"
)

type MigrationConflict struct {
	Entity      string `json:"entity"`
	ID          string `json:"id"`
	Field       string `json:"field"`
	Severity    string `json:"severity"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination,omitempty"`
}

type MigrationWarning struct {
	Entity string `json:"entity"`
	ID     string `json:"id"`
	Field  string `json:"field"`
	Detail string `json:"detail"`
}

type MigrationEntityReport struct {
	Source         int `json:"source"`
	New            int `json:"new"`
	AlreadyPresent int `json:"already_present"`
	Compatible     int `json:"compatible"`
	Warnings       int `json:"warnings"`
	Conflicts      int `json:"conflicts"`
	Critical       int `json:"critical"`
}

type MigrationVerification struct {
	Vacancies         int      `json:"vacancies"`
	Applications      int      `json:"applications"`
	ApplicationEvents int      `json:"application_events"`
	Conversations     int      `json:"conversations"`
	Messages          int      `json:"messages"`
	RelationErrors    []string `json:"relation_errors"`
}

type CareerMigrationReport struct {
	Mode                  string                 `json:"mode"`
	SourceFingerprint     string                 `json:"source_fingerprint"`
	SourceFilesUnmodified bool                   `json:"source_files_unmodified"`
	SafeToApply           bool                   `json:"safe_to_apply"`
	Applied               bool                   `json:"applied"`
	Vacancies             MigrationEntityReport  `json:"vacancies"`
	Applications          MigrationEntityReport  `json:"applications"`
	ApplicationEvents     MigrationEntityReport  `json:"application_events"`
	Conversations         MigrationEntityReport  `json:"conversations"`
	Messages              MigrationEntityReport  `json:"messages"`
	Conflicts             []MigrationConflict    `json:"conflicts"`
	Warnings              []MigrationWarning     `json:"warnings"`
	RelationErrors        []string               `json:"relation_errors"`
	Verification          *MigrationVerification `json:"verification,omitempty"`
	Error                 string                 `json:"error,omitempty"`
	StartedAt             time.Time              `json:"started_at"`
	CompletedAt           time.Time              `json:"completed_at,omitempty"`
}

// CareerMigrationSource is populated through the existing JSON stores, never
// through a second JSON parser.
type CareerMigrationSource struct {
	Vacancies         []Vacancy
	Applications      []JobApplication
	ApplicationEvents []ApplicationEvent
	Conversations     []EmployerConversation
}

type CareerMigrationDestination struct {
	Vacancies         []Vacancy
	Applications      []JobApplication
	ApplicationEvents []ApplicationEvent
	Conversations     []EmployerConversation
}

type migrationMessageRef struct {
	ConversationID string
	Message        ConversationMessage
}

type CareerMigrationPlan struct {
	Source              CareerMigrationSource
	VacancyIndexes      []int
	ApplicationIndexes  []int
	EventIndexes        []int
	ConversationIndexes []int
	MessageRefs         []migrationMessageRef
	Conflicts           []MigrationConflict
	Warnings            []MigrationWarning
	RelationErrors      []string
	Report              CareerMigrationReport
}

// LoadCareerMigrationSource uses the same stores and validation contracts as
// the running JSON backend. Missing legacy files are treated as empty stores,
// matching their normal Load behavior.
func LoadCareerMigrationSource(dir string) (CareerMigrationSource, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = "."
	}
	vacancies := NewVacancyStore(filepath.Join(dir, VacanciesFilename))
	applications := NewApplicationStore(filepath.Join(dir, JobApplicationsFilename))
	conversations := NewConversationStore(filepath.Join(dir, EmployerConversationsFilename))
	if err := vacancies.Load(); err != nil {
		return CareerMigrationSource{}, fmt.Errorf("load %s: %w", VacanciesFilename, err)
	}
	if err := applications.Load(); err != nil {
		return CareerMigrationSource{}, fmt.Errorf("load %s: %w", JobApplicationsFilename, err)
	}
	if err := conversations.Load(); err != nil {
		return CareerMigrationSource{}, fmt.Errorf("load %s: %w", EmployerConversationsFilename, err)
	}
	v, err := vacancies.List()
	if err != nil {
		return CareerMigrationSource{}, err
	}
	a, err := applications.ListApplications()
	if err != nil {
		return CareerMigrationSource{}, err
	}
	e, err := applications.ListEvents()
	if err != nil {
		return CareerMigrationSource{}, err
	}
	c, err := conversations.ListConversations()
	if err != nil {
		return CareerMigrationSource{}, err
	}
	return CareerMigrationSource{Vacancies: v, Applications: a, ApplicationEvents: e, Conversations: c}, nil
}

func migrationFingerprint(source CareerMigrationSource) (string, error) {
	raw, err := json.Marshal(source)
	if err != nil {
		return "", fmt.Errorf("encode migration source fingerprint: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// BuildCareerMigrationPlan performs all validation, identity matching and
// relation checks without mutating either source or destination.
func BuildCareerMigrationPlan(source CareerMigrationSource, destination CareerMigrationDestination) (CareerMigrationPlan, error) {
	plan := CareerMigrationPlan{Source: source, VacancyIndexes: []int{}, ApplicationIndexes: []int{}, EventIndexes: []int{}, ConversationIndexes: []int{}, MessageRefs: []migrationMessageRef{}, Conflicts: []MigrationConflict{}, Warnings: []MigrationWarning{}, RelationErrors: []string{}}
	if err := jsonstorage.ValidateVacancies(source.Vacancies); err != nil {
		return plan, fmt.Errorf("validate source vacancies: %w", err)
	}
	if err := jsonstorage.ValidateApplicationCollections(source.Applications, source.ApplicationEvents); err != nil {
		return plan, fmt.Errorf("validate source applications: %w", err)
	}
	if err := validateConversations(source.Conversations); err != nil {
		return plan, fmt.Errorf("validate source conversations: %w", err)
	}
	fingerprint, err := migrationFingerprint(source)
	if err != nil {
		return plan, err
	}
	plan.Report = CareerMigrationReport{Mode: "dry_run", SourceFingerprint: fingerprint, SourceFilesUnmodified: true, Conflicts: []MigrationConflict{}, Warnings: []MigrationWarning{}, RelationErrors: []string{}}
	plan.Report.Vacancies.Source = len(source.Vacancies)
	plan.Report.Applications.Source = len(source.Applications)
	plan.Report.ApplicationEvents.Source = len(source.ApplicationEvents)
	plan.Report.Conversations.Source = len(source.Conversations)
	for _, conversation := range source.Conversations {
		plan.Report.Messages.Source += len(conversation.Messages)
	}

	vacancyByID := map[int]Vacancy{}
	vacancyByExternal := map[string]Vacancy{}
	for _, value := range destination.Vacancies {
		vacancyByID[value.ID] = value
		if strings.TrimSpace(value.ExternalID) != "" {
			vacancyByExternal[value.ExternalID] = value
		}
	}
	for i, value := range source.Vacancies {
		if value.ID == 0 && strings.TrimSpace(value.ExternalID) == "" {
			plan.addCritical("vacancy", "0", "identity", "local ID and external ID are both empty", "")
			continue
		}
		old, found, identityConflict := findVacancyIdentity(value, vacancyByID, vacancyByExternal)
		if identityConflict != nil {
			plan.Conflicts = append(plan.Conflicts, *identityConflict)
			continue
		}
		if !found {
			plan.VacancyIndexes = append(plan.VacancyIndexes, i)
			plan.Report.Vacancies.New++
			continue
		}
		if vacancyEquivalent(value, old) {
			plan.Report.Vacancies.AlreadyPresent++
		} else {
			plan.addConflictForRecord("vacancy", migrationVacancyID(value), "record", "vacancy data differs", "destination differs")
		}
	}

	appByID := map[string]JobApplication{}
	appByExternal := map[string]JobApplication{}
	for _, value := range destination.Applications {
		appByID[value.ID] = value
		if strings.TrimSpace(value.ExternalID) != "" {
			appByExternal[value.ExternalID] = value
		}
	}
	for i, value := range source.Applications {
		old, found, identityConflict := findApplicationIdentity(value, appByID, appByExternal)
		if identityConflict != nil {
			plan.Conflicts = append(plan.Conflicts, *identityConflict)
			continue
		}
		if !found {
			plan.ApplicationIndexes = append(plan.ApplicationIndexes, i)
			plan.Report.Applications.New++
		} else if applicationEquivalent(value, old) {
			plan.Report.Applications.AlreadyPresent++
		} else {
			field, src, dst := applicationDifference(value, old)
			plan.addConflictForRecord("application", value.ID, field, src, dst)
		}
	}

	conversationByID := map[string]EmployerConversation{}
	conversationByExternal := map[string]EmployerConversation{}
	for _, value := range destination.Conversations {
		conversationByID[value.ID] = value
		if strings.TrimSpace(value.HHConversationID) != "" {
			conversationByExternal[value.HHConversationID] = value
		}
	}
	for i, value := range source.Conversations {
		old, found, identityConflict := findConversationIdentity(value, conversationByID, conversationByExternal)
		if identityConflict != nil {
			plan.Conflicts = append(plan.Conflicts, *identityConflict)
			continue
		}
		if !found {
			plan.ConversationIndexes = append(plan.ConversationIndexes, i)
			plan.Report.Conversations.New++
		} else {
			messageStatus := plan.planMessages(value, old)
			switch {
			case migrationConversationEquivalent(value, old) && messageStatus == "exact":
				plan.Report.Conversations.AlreadyPresent++
			case conversationCompatible(value, old):
				plan.Report.Conversations.Compatible++
			default:
				plan.addConflictForRecord("conversation", value.ID, "record", "conversation data differs", "destination differs")
			}
		}
	}
	// New conversation messages are counted after parent matching so a fresh
	// conversation has all messages classified as new.
	for _, i := range plan.ConversationIndexes {
		for _, message := range source.Conversations[i].Messages {
			plan.MessageRefs = append(plan.MessageRefs, migrationMessageRef{ConversationID: source.Conversations[i].ID, Message: message})
			plan.Report.Messages.New++
		}
	}

	eventByID := map[string]ApplicationEvent{}
	for _, event := range destination.ApplicationEvents {
		eventByID[event.ID] = event
	}
	for i, event := range source.ApplicationEvents {
		old, ok := eventByID[event.ID]
		if !ok {
			plan.EventIndexes = append(plan.EventIndexes, i)
			plan.Report.ApplicationEvents.New++
			continue
		}
		if applicationEventEquivalent(event, old) {
			plan.Report.ApplicationEvents.AlreadyPresent++
		} else {
			plan.addCritical("application_event", event.ID, "payload", migrationEventSummary(event), migrationEventSummary(old))
		}
	}

	plan.validateRelations(source, destination)
	plan.finalizeReport()
	return plan, nil
}

func (p *CareerMigrationPlan) addCritical(entity, id, field, source, destination string) {
	p.Conflicts = append(p.Conflicts, MigrationConflict{Entity: entity, ID: id, Field: field, Severity: migrationCritical, Source: source, Destination: destination})
}

func (p *CareerMigrationPlan) addConflictForRecord(entity, id, field, source, destination string) {
	p.Conflicts = append(p.Conflicts, MigrationConflict{Entity: entity, ID: id, Field: field, Severity: migrationConflict, Source: source, Destination: destination})
}

func (p *CareerMigrationPlan) planMessages(source, destination EmployerConversation) string {
	destByID := map[string]ConversationMessage{}
	destByExternal := map[string]ConversationMessage{}
	for _, message := range destination.Messages {
		destByID[message.ID] = message
		if message.ExternalID != "" {
			destByExternal[string(message.Source)+"\x00"+message.ExternalID] = message
		}
	}
	status := "exact"
	for _, message := range source.Messages {
		old, ok := destByID[message.ID]
		if !ok && message.ExternalID != "" {
			old, ok = destByExternal[string(message.Source)+"\x00"+message.ExternalID]
			if ok && old.ID != message.ID {
				p.addCritical("message", message.ID, "identity", "external identity maps to a different message ID", old.ID)
				status = "conflict"
				continue
			}
		}
		if !ok {
			p.MessageRefs = append(p.MessageRefs, migrationMessageRef{ConversationID: source.ID, Message: message})
			p.Report.Messages.New++
			status = "subset"
			continue
		}
		if conversationMessageEquivalent(message, old) {
			p.Report.Messages.AlreadyPresent++
		} else {
			p.addCritical("message", message.ID, "payload", messageDiagnostic(message), messageDiagnostic(old))
			status = "conflict"
		}
	}
	if len(destination.Messages) != len(source.Messages) && status == "exact" {
		status = "subset"
	}
	if len(destination.Messages) > len(source.Messages) {
		p.Warnings = append(p.Warnings, MigrationWarning{Entity: "conversation", ID: source.ID, Field: "messages", Detail: "destination contains messages absent from the legacy snapshot; destination was preserved"})
	}
	return status
}

func (p *CareerMigrationPlan) validateRelations(source CareerMigrationSource, destination CareerMigrationDestination) {
	vacancies := map[int]bool{}
	for _, value := range append(append([]Vacancy{}, source.Vacancies...), destination.Vacancies...) {
		vacancies[value.ID] = true
	}
	applications := map[string]JobApplication{}
	for _, value := range append(append([]JobApplication{}, source.Applications...), destination.Applications...) {
		applications[value.ID] = value
	}
	conversations := map[string]EmployerConversation{}
	for _, value := range append(append([]EmployerConversation{}, source.Conversations...), destination.Conversations...) {
		conversations[value.ID] = value
	}
	for _, value := range source.Applications {
		if !vacancies[value.VacancyID] {
			p.relationError(fmt.Sprintf("application %s references missing vacancy %d", value.ID, value.VacancyID))
		}
		if value.ConversationID != "" {
			conversation, ok := conversations[value.ConversationID]
			if !ok {
				p.relationError(fmt.Sprintf("application %s references missing conversation %s", value.ID, value.ConversationID))
			} else if conversation.VacancyID != value.VacancyID {
				p.relationError(fmt.Sprintf("application %s and conversation %s reference different vacancies", value.ID, value.ConversationID))
			} else if conversation.ApplicationID != "" && conversation.ApplicationID != value.ID {
				p.relationError(fmt.Sprintf("conversation %s belongs to application %s, not %s", conversation.ID, conversation.ApplicationID, value.ID))
			}
		}
	}
	for _, value := range source.Conversations {
		if !vacancies[value.VacancyID] {
			p.relationError(fmt.Sprintf("conversation %s references missing vacancy %d", value.ID, value.VacancyID))
		}
		if value.ApplicationID != "" {
			application, ok := applications[value.ApplicationID]
			if !ok {
				p.relationError(fmt.Sprintf("conversation %s references missing application %s", value.ID, value.ApplicationID))
			} else if application.VacancyID != value.VacancyID {
				p.relationError(fmt.Sprintf("conversation %s and application %s reference different vacancies", value.ID, value.ApplicationID))
			} else if application.ConversationID != "" && application.ConversationID != value.ID {
				p.relationError(fmt.Sprintf("application %s points to conversation %s, not %s", application.ID, application.ConversationID, value.ID))
			}
		}
	}
	for _, event := range source.ApplicationEvents {
		if _, ok := applications[event.ApplicationID]; !ok {
			p.relationError(fmt.Sprintf("application event %s references missing application %s", event.ID, event.ApplicationID))
		}
	}
}

func (p *CareerMigrationPlan) relationError(detail string) {
	p.RelationErrors = append(p.RelationErrors, detail)
	p.addCritical("relation", detail, "foreign_key", detail, "missing or incompatible relation")
}

func (p *CareerMigrationPlan) finalizeReport() {
	sort.Slice(p.Conflicts, func(i, j int) bool {
		return p.Conflicts[i].Entity+"\x00"+p.Conflicts[i].ID < p.Conflicts[j].Entity+"\x00"+p.Conflicts[j].ID
	})
	p.Report.Conflicts = append([]MigrationConflict{}, p.Conflicts...)
	p.Report.Warnings = append([]MigrationWarning{}, p.Warnings...)
	p.Report.RelationErrors = append([]string{}, p.RelationErrors...)
	for _, warning := range p.Warnings {
		report := migrationReportForEntity(&p.Report, warning.Entity)
		report.Warnings++
	}
	for _, conflict := range p.Conflicts {
		report := migrationReportForEntity(&p.Report, conflict.Entity)
		report.Conflicts++
		if conflict.Severity == migrationCritical {
			report.Critical++
		}
	}
	p.Report.SafeToApply = len(p.Conflicts) == 0
}

func migrationReportForEntity(report *CareerMigrationReport, entity string) *MigrationEntityReport {
	switch entity {
	case "vacancy":
		return &report.Vacancies
	case "application":
		return &report.Applications
	case "application_event":
		return &report.ApplicationEvents
	case "conversation":
		return &report.Conversations
	case "message":
		return &report.Messages
	default:
		return &report.Conversations
	}
}

func findVacancyIdentity(value Vacancy, byID map[int]Vacancy, byExternal map[string]Vacancy) (Vacancy, bool, *MigrationConflict) {
	byLocal, localOK := byID[value.ID]
	byHH, externalOK := Vacancy{}, false
	if value.ExternalID != "" {
		byHH, externalOK = byExternal[value.ExternalID]
	}
	if localOK && byLocal.ExternalID != value.ExternalID && (byLocal.ExternalID != "" || value.ExternalID != "") {
		return Vacancy{}, false, &MigrationConflict{Entity: "vacancy", ID: fmt.Sprint(value.ID), Field: "external_id", Severity: migrationCritical, Source: value.ExternalID, Destination: byLocal.ExternalID}
	}
	if externalOK && value.ID != 0 && byHH.ID != value.ID {
		return Vacancy{}, false, &MigrationConflict{Entity: "vacancy", ID: value.ExternalID, Field: "id", Severity: migrationCritical, Source: fmt.Sprint(value.ID), Destination: fmt.Sprint(byHH.ID)}
	}
	if localOK {
		return byLocal, true, nil
	}
	if externalOK {
		return byHH, true, nil
	}
	return Vacancy{}, false, nil
}

func findApplicationIdentity(value JobApplication, byID map[string]JobApplication, byExternal map[string]JobApplication) (JobApplication, bool, *MigrationConflict) {
	byLocal, localOK := byID[value.ID]
	byHH, externalOK := JobApplication{}, false
	if value.ExternalID != "" {
		byHH, externalOK = byExternal[value.ExternalID]
	}
	if localOK && byLocal.ExternalID != value.ExternalID && (byLocal.ExternalID != "" || value.ExternalID != "") {
		return JobApplication{}, false, &MigrationConflict{Entity: "application", ID: value.ID, Field: "external_id", Severity: migrationCritical, Source: value.ExternalID, Destination: byLocal.ExternalID}
	}
	if externalOK && byHH.ID != value.ID {
		return JobApplication{}, false, &MigrationConflict{Entity: "application", ID: value.ExternalID, Field: "id", Severity: migrationCritical, Source: value.ID, Destination: byHH.ID}
	}
	if localOK {
		return byLocal, true, nil
	}
	if externalOK {
		return byHH, true, nil
	}
	return JobApplication{}, false, nil
}

func findConversationIdentity(value EmployerConversation, byID map[string]EmployerConversation, byExternal map[string]EmployerConversation) (EmployerConversation, bool, *MigrationConflict) {
	byLocal, localOK := byID[value.ID]
	byHH, externalOK := EmployerConversation{}, false
	if value.HHConversationID != "" {
		byHH, externalOK = byExternal[value.HHConversationID]
	}
	if localOK && byLocal.HHConversationID != value.HHConversationID && (byLocal.HHConversationID != "" || value.HHConversationID != "") {
		return EmployerConversation{}, false, &MigrationConflict{Entity: "conversation", ID: value.ID, Field: "external_id", Severity: migrationCritical, Source: value.HHConversationID, Destination: byLocal.HHConversationID}
	}
	if externalOK && byHH.ID != value.ID {
		return EmployerConversation{}, false, &MigrationConflict{Entity: "conversation", ID: value.HHConversationID, Field: "id", Severity: migrationCritical, Source: value.ID, Destination: byHH.ID}
	}
	if localOK {
		return byLocal, true, nil
	}
	if externalOK {
		return byHH, true, nil
	}
	return EmployerConversation{}, false, nil
}

func vacancyEquivalent(a, b Vacancy) bool {
	a.PublishedAt, b.PublishedAt = a.PublishedAt.UTC(), b.PublishedAt.UTC()
	a.HHUpdatedAt, b.HHUpdatedAt = a.HHUpdatedAt.UTC(), b.HHUpdatedAt.UTC()
	a.CreatedAt, b.CreatedAt = a.CreatedAt.UTC(), b.CreatedAt.UTC()
	a.UpdatedAt, b.UpdatedAt = a.UpdatedAt.UTC(), b.UpdatedAt.UTC()
	return reflect.DeepEqual(a, b)
}

func applicationDifference(a, b JobApplication) (string, string, string) {
	if a.Status != b.Status {
		return "status", string(a.Status), string(b.Status)
	}
	if a.VacancyID != b.VacancyID {
		return "vacancy_id", fmt.Sprint(a.VacancyID), fmt.Sprint(b.VacancyID)
	}
	return "record", "source application differs", "destination application differs"
}

func applicationEventEquivalent(a, b ApplicationEvent) bool {
	return a.ID == b.ID && a.ApplicationID == b.ApplicationID && a.Type == b.Type && a.Description == b.Description && a.Timestamp.Equal(b.Timestamp)
}

func conversationMessageEquivalent(a, b ConversationMessage) bool {
	return sameConversationMessage(a, b) && a.HHSystemEvent == b.HHSystemEvent
}

func conversationParentEquivalent(a, b EmployerConversation) bool {
	a.Messages, b.Messages = nil, nil
	return a.ID == b.ID && a.VacancyID == b.VacancyID && a.ApplicationID == b.ApplicationID && a.HHConversationID == b.HHConversationID &&
		a.CompanyName == b.CompanyName && a.VacancyTitle == b.VacancyTitle && a.VacancyDescription == b.VacancyDescription &&
		a.Status == b.Status && a.CreatedAt.Equal(b.CreatedAt) && a.UpdatedAt.Equal(b.UpdatedAt) && a.HHUpdatedAt.Equal(b.HHUpdatedAt) &&
		sameConversationTime(a.LastEmployerMessageAt, b.LastEmployerMessageAt) && sameConversationTime(a.LastCandidateMessageAt, b.LastCandidateMessageAt) &&
		reflect.DeepEqual(a.Summary, b.Summary) && a.NextAction == b.NextAction && sameConversationTime(a.WaitingSince, b.WaitingSince) &&
		sameConversationTime(a.LastActivityAt, b.LastActivityAt) && a.FollowUpState == b.FollowUpState && a.RawStatus == b.RawStatus && reflect.DeepEqual(a.HHMetadata, b.HHMetadata)
}

func migrationConversationEquivalent(a, b EmployerConversation) bool {
	if !conversationParentEquivalent(a, b) || len(a.Messages) != len(b.Messages) {
		return false
	}
	byID := make(map[string]ConversationMessage, len(b.Messages))
	for _, message := range b.Messages {
		byID[message.ID] = message
	}
	for _, message := range a.Messages {
		if existing, ok := byID[message.ID]; !ok || !conversationMessageEquivalent(message, existing) {
			return false
		}
	}
	return true
}

func conversationCompatible(source, destination EmployerConversation) bool {
	if source.ID != destination.ID || source.VacancyID != destination.VacancyID || source.HHConversationID != destination.HHConversationID ||
		!source.CreatedAt.Equal(destination.CreatedAt) || source.Status != destination.Status || source.ApplicationID != destination.ApplicationID ||
		source.CompanyName != destination.CompanyName || source.VacancyTitle != destination.VacancyTitle || source.VacancyDescription != destination.VacancyDescription ||
		source.NextAction != destination.NextAction || source.FollowUpState != destination.FollowUpState || source.RawStatus != destination.RawStatus ||
		!source.HHUpdatedAt.Equal(destination.HHUpdatedAt) || !reflect.DeepEqual(source.Summary, destination.Summary) || !reflect.DeepEqual(source.HHMetadata, destination.HHMetadata) ||
		destination.UpdatedAt.Before(source.UpdatedAt) {
		return false
	}
	for _, message := range source.Messages {
		found := false
		for _, existing := range destination.Messages {
			if existing.ID == message.ID {
				if !conversationMessageEquivalent(message, existing) {
					return false
				}
				found = true
				break
			}
		}
		if !found {
			// A source message absent from an otherwise newer destination is a
			// safe additive import, so it is intentionally allowed here.
			continue
		}
	}
	return true
}

func migrationVacancyID(v Vacancy) string {
	if v.ID != 0 {
		return fmt.Sprint(v.ID)
	}
	return v.ExternalID
}

func migrationEventSummary(e ApplicationEvent) string {
	return fmt.Sprintf("%s/%s/%s/description_hash:%s", e.ApplicationID, e.Type, e.Timestamp.UTC().Format(time.RFC3339Nano), shortHash(e.Description))
}

func messageDiagnostic(m ConversationMessage) string {
	raw, _ := json.Marshal(struct {
		ID   string `json:"id"`
		Hash string `json:"hash"`
	}{ID: m.ID, Hash: shortHash(m.Text)})
	return string(raw)
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

// ApplyCareerMigration executes only the plan's additive actions in one
// PostgresCareerStore.WithTx transaction. It requires concrete Postgres
// repositories with import-only methods; JSON repositories cannot be used as
// a destination accidentally.
func ApplyCareerMigration(ctx context.Context, store *PostgresCareerStore, plan CareerMigrationPlan) error {
	if store == nil {
		return errors.New("postgres career store is required")
	}
	if !plan.Report.SafeToApply {
		return errors.New("migration has unresolved conflicts; apply blocked")
	}
	return store.WithTx(ctx, func(tx CareerTx) error {
		vacancies, ok := tx.Vacancies().(*PostgresVacancyRepository)
		if !ok {
			return errors.New("migration requires postgres vacancy repository")
		}
		applications, ok := tx.Applications().(*PostgresApplicationRepository)
		if !ok {
			return errors.New("migration requires postgres application repository")
		}
		conversations, ok := tx.Conversations().(*PostgresConversationRepository)
		if !ok {
			return errors.New("migration requires postgres conversation repository")
		}
		for _, index := range plan.VacancyIndexes {
			if err := vacancies.Import(ctx, plan.Source.Vacancies[index]); err != nil {
				return err
			}
		}
		for _, index := range plan.ApplicationIndexes {
			if err := applications.Import(ctx, plan.Source.Applications[index]); err != nil {
				return err
			}
		}
		for _, index := range plan.ConversationIndexes {
			if err := conversations.Import(ctx, plan.Source.Conversations[index]); err != nil {
				return err
			}
		}
		for _, index := range plan.EventIndexes {
			if err := applications.ImportEvent(ctx, plan.Source.ApplicationEvents[index]); err != nil {
				return err
			}
		}
		for _, ref := range plan.MessageRefs {
			if err := conversations.ImportMessage(ctx, ref.ConversationID, ref.Message); err != nil {
				return err
			}
		}
		return nil
	})
}

func readCareerMigrationDestination(ctx context.Context, career CareerRepositories) (CareerMigrationDestination, error) {
	vacancies, err := career.Vacancies.List(ctx, VacancyQuery{})
	if err != nil {
		return CareerMigrationDestination{}, err
	}
	applications, err := career.Applications.List(ctx)
	if err != nil {
		return CareerMigrationDestination{}, err
	}
	postgresApplications, ok := career.Applications.(*PostgresApplicationRepository)
	if !ok {
		return CareerMigrationDestination{}, errors.New("migration destination is not postgres")
	}
	events, err := postgresApplications.ListEvents(ctx)
	if err != nil {
		return CareerMigrationDestination{}, err
	}
	conversations, err := career.Conversations.List(ctx)
	if err != nil {
		return CareerMigrationDestination{}, err
	}
	return CareerMigrationDestination{Vacancies: vacancies, Applications: applications, ApplicationEvents: events, Conversations: conversations}, nil
}

func verifyCareerMigration(ctx context.Context, career CareerRepositories, source CareerMigrationSource) (MigrationVerification, error) {
	destination, err := readCareerMigrationDestination(ctx, career)
	if err != nil {
		return MigrationVerification{}, err
	}
	plan, err := BuildCareerMigrationPlan(source, destination)
	if err != nil {
		return MigrationVerification{}, err
	}
	verification := MigrationVerification{Vacancies: len(source.Vacancies), Applications: len(source.Applications), ApplicationEvents: len(source.ApplicationEvents), Conversations: len(source.Conversations), RelationErrors: append([]string{}, plan.RelationErrors...)}
	for _, c := range source.Conversations {
		verification.Messages += len(c.Messages)
	}
	if len(plan.Conflicts) > 0 {
		return verification, fmt.Errorf("post-migration verification found %d conflict(s)", len(plan.Conflicts))
	}
	return verification, nil
}

func writeCareerMigrationReport(path string, report CareerMigrationReport) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write migration report: %w", err)
	}
	return nil
}

func printCareerMigrationReport(out io.Writer, report CareerMigrationReport) error {
	if _, err := fmt.Fprintf(out, "Migration plan created.\nSource fingerprint: %s\n", report.SourceFingerprint); err != nil {
		return err
	}
	printEntity := func(name string, value MigrationEntityReport) error {
		_, err := fmt.Fprintf(out, "%s: source=%d new=%d already_present=%d compatible=%d conflicts=%d critical=%d\n", name, value.Source, value.New, value.AlreadyPresent, value.Compatible, value.Conflicts, value.Critical)
		return err
	}
	for _, item := range []struct {
		name  string
		value MigrationEntityReport
	}{
		{"Vacancies", report.Vacancies}, {"Applications", report.Applications}, {"Application events", report.ApplicationEvents}, {"Conversations", report.Conversations}, {"Messages", report.Messages},
	} {
		if err := printEntity(item.name, item.value); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "Relation errors: %d\nConflicts: %d\nSource JSON files were not modified.\n", len(report.RelationErrors), len(report.Conflicts)); err != nil {
		return err
	}
	if report.SafeToApply {
		if report.Applied {
			if _, err := io.WriteString(out, "Migration applied successfully.\n"); err != nil {
				return err
			}
			if report.Verification != nil {
				v := report.Verification
				_, err := fmt.Fprintf(out, "Vacancies: %d / %d verified\nApplications: %d / %d verified\nApplication events: %d / %d verified\nConversations: %d / %d verified\nMessages: %d / %d verified\nRelation errors: %d\nConflicts: %d\n", v.Vacancies, report.Vacancies.Source, v.Applications, report.Applications.Source, v.ApplicationEvents, report.ApplicationEvents.Source, v.Conversations, report.Conversations.Source, v.Messages, report.Messages.Source, len(v.RelationErrors), len(report.Conflicts))
				return err
			}
			return nil
		}
		_, err := io.WriteString(out, "Safe to apply. Use --apply for the explicit PostgreSQL write.\n")
		return err
	}
	_, err := io.WriteString(out, "Apply blocked: unresolved critical/conflicting data requires review.\n")
	return err
}
