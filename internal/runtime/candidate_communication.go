package runtime

import (
	_ "embed"

	"hh-ai-responder/internal/usecase/autochatreply"
)

// Embedded so the same communication rules are available in standalone binaries.
//
//go:embed candidate_communication.md
var candidateCommunicationProfile string

func buildChatSystemPrompt(chatToReply ChatToReply) string {
	systemPrompt, _ := autochatreply.BuildPrompt(autochatreply.Input{
		Candidate: autochatreply.Candidate{
			FirstName: chatToReply.FirstName, LastName: chatToReply.LastName,
			ResumeTitle: chatToReply.ResumeTitle, Salary: chatToReply.Salary,
			Skills: chatToReply.Skills, Experience: chatToReply.ResumeExperience,
			AlwaysEmphasize: chatToReply.AlwaysEmphasize, AvoidClaiming: chatToReply.AvoidClaiming,
			Context: chatToReply.CandidateContext,
		},
		CommunicationProfile: candidateCommunicationProfile,
	})
	return systemPrompt
}
