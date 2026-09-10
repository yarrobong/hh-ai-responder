package runtime

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	autochatattempt "hh-ai-responder/internal/autochatattempt"
	applicationreconciliation "hh-ai-responder/internal/usecase/applicationreconciliation"
	autochatreconciliation "hh-ai-responder/internal/usecase/autochatreconciliation"
	reliabilityinspection "hh-ai-responder/internal/usecase/reliabilityinspection"
)

func runReliabilityCommand(args []string, cfg Config, stdout io.Writer) error {
	if len(args) == 0 || isCLIHelpFlag(args[0]) {
		if len(args) > 0 {
			_, _ = io.WriteString(stdout, "usage: hh reliability applications|autochat [--limit N] [--state STATE] [--all] [--json] | hh reliability applications|autochat reconcile <attempt-id> [--json]\n")
			return nil
		}
		return errors.New("usage: hh reliability applications|autochat [--limit N] [--state STATE] [--all] [--json] | hh reliability applications|autochat reconcile <attempt-id> [--json]")
	}
	target := args[0]
	if target != "applications" && target != "autochat" {
		return fmt.Errorf("unknown reliability target %q", target)
	}
	if len(args) > 1 && args[1] == "reconcile" {
		return runReliabilityReconcileCommand(target, args[2:], cfg, stdout)
	}
	fs := flag.NewFlagSet("hh reliability "+target, flag.ContinueOnError)
	fs.SetOutput(stdout)
	limit := fs.Int("limit", reliabilityinspection.DefaultLimit, "maximum number of records (1-100)")
	state := fs.String("state", "", "exact technical attempt state")
	vacancyID := fs.Int("vacancy-id", 0, "application vacancy ID")
	conversationID := fs.String("conversation-id", "", "auto-chat conversation ID")
	actionType := fs.String("action-type", "", "REPLY or LEAVE")
	jsonOutput := fs.Bool("json", false, "machine-readable JSON")
	all := fs.Bool("all", false, "bounded recent history instead of needs attention")
	recent := fs.Bool("recent", false, "bounded recent history instead of needs attention")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected reliability arguments")
	}
	if *all && *recent {
		return errors.New("--all and --recent cannot be used together")
	}
	if *limit < 1 || *limit > reliabilityinspection.MaxLimit {
		return fmt.Errorf("limit must be between 1 and %d", reliabilityinspection.MaxLimit)
	}

	backend, err := normalizeStorageBackend(cfg.StorageBackend)
	if err != nil {
		return err
	}
	var closePool func()
	var pool *pgxpool.Pool
	if backend == storageBackendPostgres {
		opened, openErr := OpenPostgres(context.Background(), PostgresConfig{DatabaseURL: cfg.DatabaseURL})
		if openErr != nil {
			return openErr
		}
		pool = opened
		closePool = opened.Close
	} else {
		closePool = func() {}
	}
	defer closePool()
	applications, err := buildApplicationAttemptReader(cfg, backend, pool)
	if err != nil {
		return err
	}
	autoChats, err := buildAutoChatAttemptReader(cfg, backend, pool)
	if err != nil {
		return err
	}
	service := reliabilityinspection.NewService(applications, autoChats)
	attention := !*all && !*recent
	if target == "applications" {
		var vacancy *int
		if *vacancyID != 0 {
			if *vacancyID < 1 {
				return errors.New("vacancy-id must be positive")
			}
			vacancy = vacancyID
		}
		items, listErr := service.ListApplications(context.Background(), reliabilityinspection.ApplicationFilter{Limit: *limit, NeedsAttention: attention, State: *state, VacancyID: vacancy})
		if listErr != nil {
			return listErr
		}
		if *jsonOutput {
			return writeJSON(stdout, map[string]any{"items": items, "store": map[string]string{"status": "AVAILABLE", "backend": backend}, "needs_attention": attention, "limit": *limit})
		}
		return writeApplicationReliabilityText(stdout, items, backend, attention)
	}
	var conversation *string
	if strings.TrimSpace(*conversationID) != "" {
		conversation = conversationID
	}
	items, listErr := service.ListAutoChats(context.Background(), reliabilityinspection.AutoChatFilter{Limit: *limit, NeedsAttention: attention, State: *state, ConversationID: conversation, ActionType: *actionType})
	if listErr != nil {
		return listErr
	}
	if *jsonOutput {
		return writeJSON(stdout, map[string]any{"items": items, "store": map[string]string{"status": "AVAILABLE", "backend": backend}, "needs_attention": attention, "limit": *limit})
	}
	return writeAutoChatReliabilityText(stdout, items, backend, attention)
}

func runReliabilityReconcileCommand(target string, args []string, cfg Config, stdout io.Writer) error {
	fs := flag.NewFlagSet("hh reliability "+target+" reconcile", flag.ContinueOnError)
	fs.SetOutput(stdout)
	jsonOutput := fs.Bool("json", false, "machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return errors.New("usage: hh reliability applications|autochat reconcile <attempt-id> [--json]")
	}
	attemptID := strings.TrimSpace(fs.Arg(0))
	backend, err := normalizeStorageBackend(cfg.StorageBackend)
	if err != nil {
		return err
	}
	var closePool func()
	var pool *pgxpool.Pool
	if backend == storageBackendPostgres {
		opened, openErr := OpenPostgres(context.Background(), PostgresConfig{DatabaseURL: cfg.DatabaseURL})
		if openErr != nil {
			return openErr
		}
		pool, closePool = opened, opened.Close
	} else {
		closePool = func() {}
	}
	defer closePool()
	readCfg := dashboardReadConfig(cfg)
	readCfg.StorageBackend = storageBackendJSON
	readCfg.CandidateProfilePath, readCfg.CandidateStoriesPath, readCfg.OutputPath = "", "", ""
	responder, err := NewHHAIResponder(context.Background(), readCfg)
	if err != nil {
		return err
	}
	readClient := NewHHAIResponderReadClient(responder)
	if target == "applications" {
		store, storeErr := buildApplicationAttemptReconciliationStore(cfg, backend, pool)
		if storeErr != nil {
			return storeErr
		}
		attemptStore, ok := store.(applicationreconciliation.AttemptStore)
		if !ok {
			return errors.New("application reconciliation store is unavailable")
		}
		service := applicationreconciliation.NewService(applicationreconciliation.Dependencies{Attempts: attemptStore, Reader: operatorApplicationEvidenceReader{source: readClient}, Notifications: responder.reliabilityNotifications})
		result, reconcileErr := service.Reconcile(context.Background(), attemptID)
		if err := writeReliabilityReconcileOutput(stdout, reliabilityReconcileOutput{Workflow: "application", AttemptID: result.Attempt.AttemptID, PreviousState: result.PreviousState, NewState: result.Attempt.State, Status: string(result.Status), EvidenceKind: string(result.Evidence.Kind), EvidenceSource: result.Evidence.Source, ProviderApplicationID: result.Evidence.ProviderApplicationID, ProviderNegotiationID: result.Evidence.ProviderNegotiationID, ObservedAt: result.Evidence.ObservedAt, Reason: result.Reason, CausalityNote: "Отклик подтверждён на HH; принадлежность именно этой попытке не установлена автоматически."}, *jsonOutput); err != nil {
			return err
		}
		return reconcileErr
	}
	store, storeErr := buildAutoChatAttemptReconciliationStore(cfg, backend, pool)
	if storeErr != nil {
		return storeErr
	}
	reader, ok := store.(interface {
		GetByID(context.Context, string) (autochatattempt.Attempt, error)
	})
	writer, writerOK := store.(autochatreconciliation.ReconciliationWriter)
	if !ok || !writerOK {
		return errors.New("auto-chat reconciliation store is unavailable")
	}
	service := autochatreconciliation.NewService(autochatreconciliation.Dependencies{Attempts: reader, Reader: operatorAutoChatHistoryReader{source: readClient}, Writer: writer, Notifications: responder.reliabilityNotifications})
	result, reconcileErr := service.Reconcile(context.Background(), attemptID)
	var observedAt time.Time
	var evidenceKind, evidenceSource, providerMessageID, causality string
	if result.Evidence.Kind != "" {
		evidenceKind, evidenceSource, providerMessageID, observedAt, causality = string(result.Evidence.Kind), result.Evidence.Source, result.Evidence.ProviderMessageID, result.Evidence.ObservedAt, result.Evidence.CausalityNote
	}
	if err := writeReliabilityReconcileOutput(stdout, reliabilityReconcileOutput{Workflow: "autochat", AttemptID: result.Attempt.AttemptID, PreviousState: result.PreviousState, NewState: result.NewState, Status: string(result.Status), EvidenceKind: evidenceKind, EvidenceSource: evidenceSource, ProviderMessageID: providerMessageID, ProviderOutgoingMessageID: result.Attempt.ProviderOutgoingMessageID, ObservedAt: observedAt, Reason: result.Reason, CausalityNote: causality}, *jsonOutput); err != nil {
		return err
	}
	return reconcileErr
}

type reliabilityReconcileOutput struct {
	Workflow                  string    `json:"workflow"`
	AttemptID                 string    `json:"attempt_id"`
	PreviousState             any       `json:"previous_state"`
	NewState                  any       `json:"new_state"`
	Status                    string    `json:"status"`
	EvidenceKind              string    `json:"evidence_kind,omitempty"`
	EvidenceSource            string    `json:"evidence_source,omitempty"`
	ProviderApplicationID     string    `json:"provider_application_id,omitempty"`
	ProviderNegotiationID     string    `json:"provider_negotiation_id,omitempty"`
	ProviderMessageID         string    `json:"provider_message_id,omitempty"`
	ProviderOutgoingMessageID string    `json:"provider_outgoing_message_id,omitempty"`
	ObservedAt                time.Time `json:"observed_at,omitempty"`
	Reason                    string    `json:"reason,omitempty"`
	CausalityNote             string    `json:"causality_note,omitempty"`
}

func writeReliabilityReconcileOutput(out io.Writer, value reliabilityReconcileOutput, jsonOutput bool) error {
	if jsonOutput {
		return writeJSON(out, value)
	}
	_, err := fmt.Fprintf(out, "workflow=%s attempt=%s previous=%v new=%v status=%s evidence=%s source=%s observed=%s\nreason=%s\n", value.Workflow, value.AttemptID, value.PreviousState, value.NewState, value.Status, value.EvidenceKind, value.EvidenceSource, value.ObservedAt.Format(time.RFC3339), value.Reason)
	if value.ProviderApplicationID != "" || value.ProviderNegotiationID != "" || value.ProviderMessageID != "" || value.ProviderOutgoingMessageID != "" {
		_, _ = fmt.Fprintf(out, "provider_application=%s provider_negotiation=%s provider_message=%s provider_outgoing=%s\n", value.ProviderApplicationID, value.ProviderNegotiationID, value.ProviderMessageID, value.ProviderOutgoingMessageID)
	}
	if value.CausalityNote != "" {
		_, _ = fmt.Fprintf(out, "causality=%s\n", value.CausalityNote)
	}
	return err
}

func writeApplicationReliabilityText(out io.Writer, items []reliabilityinspection.ApplicationAttemptReadModel, backend string, attention bool) error {
	_, _ = fmt.Fprintf(out, "Хранилище: %s · %s\n", backend, attentionTitle(attention))
	for _, item := range items {
		_, _ = fmt.Fprintf(out, "[%s] %s · vacancy=%d · attempt=%s · updated=%s\n", item.State, item.DisplayLabel, item.VacancyID, item.AttemptID, item.UpdatedAt.Format("2006-01-02 15:04:05Z07:00"))
		if item.ProviderApplicationID != "" || item.ProviderNegotiationID != "" {
			_, _ = fmt.Fprintf(out, "  provider_application=%s provider_negotiation=%s\n", item.ProviderApplicationID, item.ProviderNegotiationID)
		}
	}
	if len(items) == 0 {
		_, _ = io.WriteString(out, "Записей нет.\n")
	}
	return nil
}

func writeAutoChatReliabilityText(out io.Writer, items []reliabilityinspection.AutoChatAttemptReadModel, backend string, attention bool) error {
	_, _ = fmt.Fprintf(out, "Хранилище: %s · %s\n", backend, attentionTitle(attention))
	for _, item := range items {
		_, _ = fmt.Fprintf(out, "[%s] %s · conversation=%s · trigger=%s · action=%s · attempt=%s · updated=%s\n", item.State, item.DisplayLabel, item.ConversationID, item.TriggerMessageID, item.ActionType, item.AttemptID, item.UpdatedAt.Format("2006-01-02 15:04:05Z07:00"))
		if item.ProviderOutgoingMessageID != "" {
			_, _ = fmt.Fprintf(out, "  outgoing_provider_id=%s\n", item.ProviderOutgoingMessageID)
		}
	}
	if len(items) == 0 {
		_, _ = io.WriteString(out, "Записей нет.\n")
	}
	return nil
}

func attentionTitle(attention bool) string {
	if attention {
		return "требуют внимания"
	}
	return "bounded recent history"
}
