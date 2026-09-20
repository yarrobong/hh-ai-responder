package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	hhwrite "hh-ai-responder/internal/ports/hhwrite"
	hhwritegateway "hh-ai-responder/internal/usecase/hhwritegateway"
)

func validAPIApplicationApproval(now time.Time) APIApplicationApproval {
	value := APIApplicationApproval{
		Version:          1,
		VacancyID:        42,
		ProviderResumeID: "resume-provider-7",
		CoverLetter:      "Здравствуйте! Готов обсудить интеграции и поддержку API.",
		Nonce:            "nonce-7",
		Status:           "READY_FOR_EXPLICIT_SEND",
		FinalDecision:    "MATCH",
		PreviewFreshAt:   now,
	}
	value.ContentHash = contentHash(value.CoverLetter)
	return value
}

type controlledApplicationWriter struct{ calls int }

func (w *controlledApplicationWriter) SubmitVacancyResponse(context.Context, hhwrite.VacancyResponseRequest) (hhwrite.WriteResult, error) {
	w.calls++
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeAccepted, Class: hhwrite.ApplicationResultSuccess}, nil
}

func TestControlledApplicationGatewayCapsMutationsAtOnePerInvocation(t *testing.T) {
	writer := &controlledApplicationWriter{}
	service := hhwritegateway.NewService(hhwritegateway.Dependencies{VacancyResponseWriter: writer}, hhwritegateway.Options{WriteEnabled: true, MaxWritesPerRun: 1})
	if _, err := service.SubmitVacancyResponse(context.Background(), hhwritegateway.VacancyResponseRequest{VacancyID: 42, ProviderResumeID: "resume-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitVacancyResponse(context.Background(), hhwritegateway.VacancyResponseRequest{VacancyID: 42, ProviderResumeID: "resume-2"}); err == nil {
		t.Fatal("second mutation was not blocked by invocation cap")
	}
	if writer.calls != 1 {
		t.Fatalf("writer calls=%d, want 1", writer.calls)
	}
}

func TestValidateAPIApplicationApprovalRequiresExactFreshIdentity(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	base := validAPIApplicationApproval(now)
	tests := []struct {
		name  string
		value APIApplicationApproval
		err   error
	}{
		{name: "valid at freshness boundary", value: func() APIApplicationApproval {
			value := base
			value.PreviewFreshAt = now.Add(-apiApplicationApprovalMaxAge)
			return value
		}()},
		{name: "stale", value: func() APIApplicationApproval {
			value := base
			value.PreviewFreshAt = now.Add(-apiApplicationApprovalMaxAge - time.Nanosecond)
			return value
		}(), err: errAPIApplicationApprovalStale},
		{name: "wrong vacancy", value: func() APIApplicationApproval { value := base; value.VacancyID = 43; return value }(), err: errAPIApplicationApprovalIdentity},
		{name: "wrong resume", value: func() APIApplicationApproval { value := base; value.ProviderResumeID = "other"; return value }(), err: errAPIApplicationApprovalIdentity},
		{name: "wrong status", value: func() APIApplicationApproval { value := base; value.Status = "BLOCKED"; return value }(), err: errAPIApplicationApprovalState},
		{name: "wrong decision", value: func() APIApplicationApproval { value := base; value.FinalDecision = "REVIEW_REQUIRED"; return value }(), err: errAPIApplicationApprovalState},
		{name: "content mismatch", value: func() APIApplicationApproval { value := base; value.CoverLetter += " changed"; return value }(), err: errAPIApplicationApprovalContent},
		{name: "nonce missing", value: func() APIApplicationApproval { value := base; value.Nonce = ""; return value }(), err: errAPIApplicationApprovalNonce},
		{name: "nonce used", value: func() APIApplicationApproval { value := base; used := now; value.NonceUsedAt = &used; return value }(), err: errAPIApplicationApprovalNonce},
		{name: "invalid letter", value: func() APIApplicationApproval {
			value := base
			value.CoverLetter = "```json\n{}\n```"
			value.ContentHash = contentHash(value.CoverLetter)
			return value
		}(), err: errAPIApplicationApprovalContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateAPIApplicationApproval(test.value, 42, "resume-provider-7", now)
			if test.err == nil {
				if err != nil {
					t.Fatalf("validation error=%v", err)
				}
				return
			}
			if !errors.Is(err, test.err) {
				t.Fatalf("error=%v, want %v", err, test.err)
			}
		})
	}
}
