package runtime

import (
	"errors"
	"testing"

	hhwriteport "hh-ai-responder/internal/ports/hhwrite"
)

func TestHHWritePortOutcomeCompatibilityPreservesAmbiguity(t *testing.T) {
	sentinel := errors.New("server response after dispatch")
	source := &hhwriteport.TransportError{
		Category: hhwriteport.ErrorServer,
		Outcome:  hhwriteport.OutcomeAmbiguous,
		Status:   500,
		Err:      sentinel,
	}
	converted := hhWriteErrorFromPort(source)
	var transportErr *HHWriteTransportError
	if !errors.As(converted, &transportErr) || transportErr.Outcome != hhwriteport.OutcomeAmbiguous || !transportErr.DeliveryUncertain || !errors.Is(converted, sentinel) {
		t.Fatalf("ambiguous transport error was not preserved: %#v", converted)
	}

	result := hhWriteResultFromPort(hhwriteport.WriteResult{
		Outcome:        hhwriteport.OutcomeAmbiguous,
		ProviderStatus: 500,
	})
	if result.Outcome != hhwriteport.OutcomeAmbiguous {
		t.Fatalf("ambiguous write result was not preserved: %+v", result)
	}
}
