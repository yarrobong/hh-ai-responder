package applicationpilot

import (
	"errors"
	"testing"
)

func approvalFixture() Approval {
	return Approval{VacancyID: 137428040, ResumeID: "resume-python", ContentHash: "hash", Nonce: "nonce"}
}

func currentFixture() CurrentIdentity {
	return CurrentIdentity{VacancyID: 137428040, ResumeID: "resume-python", ContentHash: "hash"}
}

func TestVerifyApprovalBlocksChangedIdentityAndReplay(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Approval, *CurrentIdentity)
		want error
	}{
		{name: "changed vacancy", edit: func(_ *Approval, current *CurrentIdentity) { current.VacancyID++ }, want: ErrVacancyChanged},
		{name: "changed resume", edit: func(_ *Approval, current *CurrentIdentity) { current.ResumeID = "other" }, want: ErrResumeChanged},
		{name: "changed cover letter", edit: func(_ *Approval, current *CurrentIdentity) { current.ContentHash = "other" }, want: ErrContentChanged},
		{name: "used nonce", edit: func(approval *Approval, _ *CurrentIdentity) { approval.NonceUsed = true }, want: ErrNonceConsumed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			approval, current := approvalFixture(), currentFixture()
			test.edit(&approval, &current)
			if err := VerifyApproval(approval, current); !errors.Is(err, test.want) {
				t.Fatalf("VerifyApproval() error=%v, want %v", err, test.want)
			}
		})
	}
}

func TestPilotBudgetAllowsOneApplicationOnly(t *testing.T) {
	var budget Budget
	if err := budget.ReserveApplication(); err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveApplication(); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("second application error=%v, want %v", err, ErrLimitReached)
	}
}

func TestReconciliationContract(t *testing.T) {
	tests := []struct {
		name     string
		evidence ReconciliationEvidence
		want     string
	}{
		{name: "confirmed submission", evidence: ReconciliationEvidence{TransportAccepted: true, ProviderRead: true, ProviderConfirmed: true}, want: OutcomeConfirmed},
		{name: "ambiguous transport never retries", evidence: ReconciliationEvidence{TransportAmbiguous: true, ProviderRead: true, ProviderConfirmed: true}, want: OutcomeUnknown},
		{name: "rejected submission", evidence: ReconciliationEvidence{TransportRejected: true, ProviderRead: true}, want: OutcomeFailed},
		{name: "missing provider read", evidence: ReconciliationEvidence{TransportAccepted: true}, want: OutcomeUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ClassifyReconciliation(test.evidence); got != test.want {
				t.Fatalf("ClassifyReconciliation()=%q, want %q", got, test.want)
			}
		})
	}
}
