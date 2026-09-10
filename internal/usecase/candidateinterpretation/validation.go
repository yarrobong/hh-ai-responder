package candidateinterpretation

import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"hh-ai-responder/internal/usecase/candidateacquisition"
)

func Parse(raw string, input Input) (Interpretation, error) {
	var value Interpretation
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return Interpretation{}, errors.New("invalid candidate knowledge interpretation JSON")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Interpretation{}, errors.New("candidate knowledge interpretation has trailing data")
	}
	if value.Proposals == nil || len(value.Proposals) > 8 {
		return Interpretation{}, errors.New("candidate interpretation proposals are invalid")
	}
	if err := Validate(value, input); err != nil {
		return Interpretation{}, err
	}
	return value, nil
}

func Validate(value Interpretation, input Input) error {
	if err := candidateacquisition.ValidateInterpretation(value, candidateacquisition.CandidateClarificationRequest{Topic: input.Gap.Subject, Status: candidateacquisition.ClarificationPending}); err != nil {
		for _, proposal := range value.Proposals {
			if status := strings.TrimSpace(proposal.TruthStatus); status != "" && status != "hypothesis" {
				return errors.New("AI cannot confirm candidate knowledge")
			}
		}
		return err
	}
	for _, proposal := range value.Proposals {
		if proposal.Type != "story" || proposal.Story == nil {
			continue
		}
		if proposal.Story.ReferencedExperience != "" && !contains(input.KnownContext.ExperienceIDs, proposal.Story.ReferencedExperience) {
			return errors.New("story references an unknown experience")
		}
		if proposal.Story.ReferencedProject != "" && !contains(input.KnownContext.ProjectIDs, proposal.Story.ReferencedProject) {
			return errors.New("story references an unknown project")
		}
	}
	return nil
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
