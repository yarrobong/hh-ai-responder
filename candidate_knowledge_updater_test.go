package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func pipelineTestUpdate(source KnowledgeSource) KnowledgeUpdate {
	return KnowledgeUpdate{Source: KnowledgeSourceRecord{Type: source, Evidence: []string{"fixture assertion"}}, Reason: "Review fixture assertion"}
}
func pipelineTestUpdater(kb *CandidateKnowledgeBase, actor KnowledgeActor) *CandidateKnowledgeUpdater {
	return NewCandidateKnowledgeUpdater(kb, KnowledgeUpdaterOptions{Actor: actor})
}
func pipelineSnapshot(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	requireKnowledgeOK(t, err)
	return raw
}
func pipelineSafe(t *testing.T, kb *CandidateKnowledgeBase) EmployerSafeKnowledge {
	t.Helper()
	view, err := kb.GetEmployerSafeKnowledge()
	requireKnowledgeOK(t, err)
	return view
}

func TestPipelineSourceTrustAndConfirmation(t *testing.T) {
	for _, source := range []KnowledgeSource{KnowledgeSourceUserConfirmed, KnowledgeSourceHHResume,
		KnowledgeSourceGithubVerified, KnowledgeSourceCandidateInterview, KnowledgeSourceProjectAnalysis, KnowledgeSourceDerived, KnowledgeSourceUnknown} {
		t.Run(string(source), func(t *testing.T) {
			kb := knowledgeTestBase(t)
			actor := KnowledgeActorAI
			if source == KnowledgeSourceUserConfirmed {
				actor = KnowledgeActorUser
			}
			if source == KnowledgeSourceHHResume {
				actor = KnowledgeActorImporter
			}
			options := KnowledgeUpdaterOptions{Actor: actor, VerifyGitHub: func(record KnowledgeSourceRecord, kind string, raw json.RawMessage) error {
				if kind != "skill" || !bytes.Contains(raw, []byte("Docker")) {
					t.Fatal("verifier did not receive full assertion")
				}
				return nil // Deterministic fixture adapter; no live GitHub/HH calls.
			}}
			update := pipelineTestUpdate(source)
			if source == KnowledgeSourceGithubVerified {
				update.Source.Reference = "https://github.com/fixture/repo/blob/abc/Dockerfile"
			}
			result, err := NewCandidateKnowledgeUpdater(kb, options).UpdateSkill(CandidateSkillDetailed{Name: "Docker"}, update)
			requireKnowledgeOK(t, err)
			view := pipelineSafe(t, kb)
			switch source {
			case KnowledgeSourceUserConfirmed, KnowledgeSourceHHResume:
				if len(view.Skills) != 1 || result.ProposalID != "" {
					t.Fatal("trusted fact was not added directly")
				}
			case KnowledgeSourceUnknown:
				if len(kb.Skills) != 0 || len(kb.Proposals) != 0 || len(kb.Unknowns) != 1 || result.QuestionID == "" {
					t.Fatal("unknown source must produce only a question")
				}
			default:
				if len(view.Skills) != 0 || result.ProposalID == "" {
					t.Fatal("unconfirmed assertion reached employer view")
				}
				if source == KnowledgeSourceDerived || source == KnowledgeSourceProjectAnalysis {
					if len(kb.Skills) != 1 || kb.Skills[0].TruthStatus != TruthStatusHypothesis {
						t.Fatal("analysis must remain hypothesis")
					}
				}
				requireKnowledgeOK(t, kb.Save())
				requireKnowledgeOK(t, kb.Load())
				requireKnowledgeOK(t, pipelineTestUpdater(kb, KnowledgeActorUser).ConfirmKnowledge(result.ProposalID))
				view = pipelineSafe(t, kb)
				want := TruthStatusConfirmed
				if source == KnowledgeSourceGithubVerified {
					want = TruthStatusVerified
				}
				if len(view.Skills) != 1 || view.Skills[0].TruthStatus != want || kb.Proposals[0].Status != KnowledgeProposalConfirmed {
					t.Fatal("confirmation did not apply the source trust policy")
				}
				if view.Skills[0].Sources[0].Type != source || view.Skills[0].ConfirmedAt == nil {
					t.Fatal("confirmation lost provenance")
				}
				events := kb.Events[len(kb.Events)-2:]
				if events[0].Action != "confirm" || events[0].EntityType != "skill" || events[1].Action != "confirm" || events[1].EntityType != "proposal" ||
					events[0].Actor != "user" || events[0].Source != KnowledgeSourceUserConfirmed {
					t.Fatal("confirmation did not record fact and proposal audit events")
				}
				requireKnowledgeOK(t, kb.Save())
				requireKnowledgeOK(t, kb.Load())
			}
		})
	}
}

func TestPipelineGitHubRequiresRealVerifier(t *testing.T) {
	for _, scenario := range []string{"no_verifier", "no_evidence", "bad_reference", "verification_failed", "bare_label"} {
		t.Run(scenario, func(t *testing.T) {
			kb := knowledgeTestBase(t)
			update := pipelineTestUpdate(KnowledgeSourceGithubVerified)
			update.Source.Reference = "https://github.com/fixture/repo/blob/abc/Dockerfile"
			options := KnowledgeUpdaterOptions{Actor: KnowledgeActorAI, VerifyGitHub: func(KnowledgeSourceRecord, string, json.RawMessage) error { return nil }}
			switch scenario {
			case "no_verifier":
				options.VerifyGitHub = nil
			case "no_evidence":
				update.Source.Evidence = nil
			case "bad_reference":
				update.Source.Reference = "https://github.com.evil.example/repo"
			case "verification_failed":
				options.VerifyGitHub = func(KnowledgeSourceRecord, string, json.RawMessage) error { return errors.New("unproven assertion") }
			case "bare_label":
				update.Source = KnowledgeSourceRecord{Type: KnowledgeSourceGithubVerified}
			}
			before := pipelineSnapshot(t, kb)
			if _, err := NewCandidateKnowledgeUpdater(kb, options).UpdateSkill(CandidateSkillDetailed{Name: "Docker", Level: SkillLevelWorking}, update); err == nil {
				t.Fatal("unverified GitHub claim accepted")
			}
			if !bytes.Equal(before, pipelineSnapshot(t, kb)) {
				t.Fatal("failed verification changed knowledge")
			}
		})
	}
}

func TestPipelineAIAndImporterCannotForgeConfirmation(t *testing.T) {
	for _, actor := range []KnowledgeActor{KnowledgeActorAI, KnowledgeActorImporter, ""} {
		t.Run(string(actor), func(t *testing.T) {
			kb := knowledgeTestBase(t)
			updater := pipelineTestUpdater(kb, actor)
			before := pipelineSnapshot(t, kb)
			_, err := updater.UpdateSkill(CandidateSkillDetailed{Name: "Python"}, pipelineTestUpdate(KnowledgeSourceUserConfirmed))
			if err == nil {
				t.Fatal("non-user created user_confirmed")
			}
			_, err = updater.UpdateSkill(CandidateSkillDetailed{Name: "Python", KnowledgeMetadata: knowledgeTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed)}, pipelineTestUpdate(KnowledgeSourceDerived))
			if err == nil {
				t.Fatal("non-user supplied confirmation metadata")
			}
			if !bytes.Equal(before, pipelineSnapshot(t, kb)) {
				t.Fatal("failed update changed state")
			}
		})
	}
	kb := knowledgeTestBase(t)
	ai := pipelineTestUpdater(kb, KnowledgeActorAI)
	result, err := ai.UpdateSkill(CandidateSkillDetailed{Name: "Docker"}, pipelineTestUpdate(KnowledgeSourceDerived))
	requireKnowledgeOK(t, err)
	before := pipelineSnapshot(t, kb)
	if ai.ConfirmKnowledge(result.ProposalID) == nil || ai.RejectKnowledge(result.ProposalID) == nil {
		t.Fatal("AI resolved a proposal")
	}
	if !bytes.Equal(before, pipelineSnapshot(t, kb)) {
		t.Fatal("AI resolution changed state")
	}
	if _, err := ai.UpdateSkill(CandidateSkillDetailed{Name: "Docker"}, pipelineTestUpdate(KnowledgeSourceHHResume)); err == nil {
		t.Fatal("AI forged HH provenance")
	}
}

func TestPipelineSkillLevelRequiresConfirmationIncludingAliases(t *testing.T) {
	for _, source := range []KnowledgeSource{KnowledgeSourceHHResume, KnowledgeSourceDerived, KnowledgeSourceProjectAnalysis} {
		t.Run(string(source), func(t *testing.T) {
			kb := knowledgeTestBase(t)
			user := pipelineTestUpdater(kb, KnowledgeActorUser)
			result, err := user.UpdateSkill(CandidateSkillDetailed{Name: "Go", Level: SkillLevelBasic}, pipelineTestUpdate(KnowledgeSourceUserConfirmed))
			requireKnowledgeOK(t, err)
			old := pipelineSnapshot(t, kb.Skills)
			updater := pipelineTestUpdater(kb, KnowledgeActorImporter)
			proposal, err := updater.UpdateSkill(CandidateSkillDetailed{Name: "golang", Level: SkillLevelAdvanced}, pipelineTestUpdate(source))
			requireKnowledgeOK(t, err)
			if proposal.EntityID != result.EntityID || proposal.ProposalID == "" || !bytes.Equal(old, pipelineSnapshot(t, kb.Skills)) {
				t.Fatal("skill was promoted or duplicated without confirmation")
			}
			if pipelineSafe(t, kb).Skills[0].Level != SkillLevelBasic {
				t.Fatal("pending level reached employer")
			}
			if _, err := updater.UpdateSkill(CandidateSkillDetailed{ID: "bypass", Name: "Go", Level: SkillLevelAdvanced}, pipelineTestUpdate(source)); err == nil {
				t.Fatal("explicit new ID bypassed identity guard")
			}
			requireKnowledgeOK(t, user.ConfirmKnowledge(proposal.ProposalID))
			if pipelineSafe(t, kb).Skills[0].Level != SkillLevelAdvanced {
				t.Fatal("explicitly confirmed level not applied")
			}
		})
	}
	kb := knowledgeTestBase(t)
	importer := pipelineTestUpdater(kb, KnowledgeActorImporter)
	result, err := importer.UpdateSkill(CandidateSkillDetailed{Name: "Python", Level: SkillLevelWorking}, pipelineTestUpdate(KnowledgeSourceHHResume))
	requireKnowledgeOK(t, err)
	if len(kb.Skills) != 0 || result.ProposalID == "" {
		t.Fatal("new HH level bypassed confirmation")
	}
}

func TestPipelineRejectDoesNotChangeProfileOrFacts(t *testing.T) {
	kb := knowledgeTestBase(t)
	updater := pipelineTestUpdater(kb, KnowledgeActorAI)
	result, err := updater.UpdateSkill(CandidateSkillDetailed{Name: "Docker", Level: SkillLevelWorking}, pipelineTestUpdate(KnowledgeSourceDerived))
	requireKnowledgeOK(t, err)
	requireKnowledgeOK(t, SaveCandidateProfile(kb.ProfilePath, kb.Profile))
	requireKnowledgeOK(t, kb.Save())
	profileBefore := readKnowledgeTestFile(t, kb.ProfilePath)
	skillsBefore := pipelineSnapshot(t, kb.Skills)
	user := pipelineTestUpdater(kb, KnowledgeActorUser)
	requireKnowledgeOK(t, user.RejectKnowledge(result.ProposalID))
	if !bytes.Equal(skillsBefore, pipelineSnapshot(t, kb.Skills)) || len(pipelineSafe(t, kb).Skills) != 0 {
		t.Fatal("rejection modified the profile")
	}
	if kb.Proposals[0].Status != KnowledgeProposalRejected || kb.Events[len(kb.Events)-1].Action != "reject" {
		t.Fatal("rejection not audited")
	}
	requireKnowledgeOK(t, kb.Save())
	if !bytes.Equal(profileBefore, readKnowledgeTestFile(t, kb.ProfilePath)) {
		t.Fatal("rejection rewrote legacy profile")
	}
	before := pipelineSnapshot(t, kb)
	if user.ConfirmKnowledge(result.ProposalID) == nil || user.RejectKnowledge(result.ProposalID) == nil || user.ConfirmKnowledge("missing") == nil {
		t.Fatal("invalid transition succeeded")
	}
	if !bytes.Equal(before, pipelineSnapshot(t, kb)) {
		t.Fatal("invalid transition changed state")
	}
}

func TestPipelineStaleProposalCannotOverwriteLaterUserUpdate(t *testing.T) {
	kb := knowledgeTestBase(t)
	ai := pipelineTestUpdater(kb, KnowledgeActorAI)
	proposal, err := ai.UpdateSkill(CandidateSkillDetailed{Name: "Python", Level: SkillLevelWorking}, pipelineTestUpdate(KnowledgeSourceDerived))
	requireKnowledgeOK(t, err)
	user := pipelineTestUpdater(kb, KnowledgeActorUser)
	_, err = user.UpdateSkill(CandidateSkillDetailed{Name: "Python", Level: SkillLevelBasic}, pipelineTestUpdate(KnowledgeSourceUserConfirmed))
	requireKnowledgeOK(t, err)
	before := pipelineSnapshot(t, kb)
	if user.ConfirmKnowledge(proposal.ProposalID) == nil {
		t.Fatal("stale proposal overwrote user fact")
	}
	if !bytes.Equal(before, pipelineSnapshot(t, kb)) {
		t.Fatal("failed confirmation changed state")
	}
	requireKnowledgeOK(t, user.RejectKnowledge(proposal.ProposalID))
}

func TestPipelineProjectsAchievementsAndQuestions(t *testing.T) {
	kb := knowledgeTestBase(t)
	ai := pipelineTestUpdater(kb, KnowledgeActorAI)
	project, err := ai.UpdateProject(CandidateProject{Name: "API tool", Description: "Tool description"}, pipelineTestUpdate(KnowledgeSourceProjectAnalysis))
	requireKnowledgeOK(t, err)
	achievement, err := ai.UpdateAchievement(CandidateAchievement{Title: "API integration", ProjectID: project.EntityID}, pipelineTestUpdate(KnowledgeSourceCandidateInterview))
	requireKnowledgeOK(t, err)
	question, err := ai.UpdateUnknown(CandidateUnknown{Question: "What was your role?", RelatedEntity: project.EntityID}, pipelineTestUpdate(KnowledgeSourceUnknown))
	requireKnowledgeOK(t, err)
	if question.QuestionID == "" || len(pipelineSafe(t, kb).Projects) != 0 || len(pipelineSafe(t, kb).Achievements) != 0 {
		t.Fatal("unconfirmed knowledge exposed")
	}
	user := pipelineTestUpdater(kb, KnowledgeActorUser)
	requireKnowledgeOK(t, user.ConfirmKnowledge(project.ProposalID))
	requireKnowledgeOK(t, user.ConfirmKnowledge(achievement.ProposalID))
	_, err = user.UpdateProject(CandidateProject{ID: project.EntityID, Name: "API tool", Description: "Updated description"}, pipelineTestUpdate(KnowledgeSourceUserConfirmed))
	requireKnowledgeOK(t, err)
	_, err = user.UpdateAchievement(CandidateAchievement{ID: achievement.EntityID, Title: "API integration", Result: []string{"Explicit result"}}, pipelineTestUpdate(KnowledgeSourceUserConfirmed))
	requireKnowledgeOK(t, err)
	_, err = ai.UpdateUnknown(CandidateUnknown{ID: question.EntityID, Question: "Which API did you integrate?"}, pipelineTestUpdate(KnowledgeSourceUnknown))
	requireKnowledgeOK(t, err)
	view := pipelineSafe(t, kb)
	if len(view.Projects) != 1 || len(view.Achievements) != 1 || view.Projects[0].Description != "Updated description" || len(kb.Unknowns) != 1 {
		t.Fatal("entity updates failed")
	}
	view.Achievements[0].Result[0] = "tampered"
	if kb.Achievements[0].Result[0] != "Explicit result" {
		t.Fatal("view aliases storage")
	}
	raw := pipelineSnapshot(t, view)
	if bytes.Contains(raw, []byte("Which API")) || bytes.Contains(raw, []byte("proposed_value")) {
		t.Fatal("questions or proposals exposed")
	}
	kb.Projects[0].Sources = nil
	if _, err := kb.GetEmployerSafeKnowledge(); err == nil {
		t.Fatal("invalid verified data exposed")
	}
}

func TestPipelineMigrationRoundTrip(t *testing.T) {
	kb := knowledgeTestBase(t)
	now := time.Now().UTC()
	kb.Profile.Skills = []CandidateSkill{{Name: "Go", Level: SkillLevelBasic, ProfileFact: confirmedProfileFact(CandidateSourceHHResume, now)}}
	requireKnowledgeOK(t, SaveCandidateProfile(kb.ProfilePath, kb.Profile))
	before := readKnowledgeTestFile(t, kb.ProfilePath)
	requireKnowledgeOK(t, kb.MigrateLegacyProfile())
	id := kb.Skills[0].ID
	result, err := pipelineTestUpdater(kb, KnowledgeActorImporter).UpdateSkill(CandidateSkillDetailed{Name: "golang", Level: SkillLevelWorking}, pipelineTestUpdate(KnowledgeSourceHHResume))
	requireKnowledgeOK(t, err)
	if result.EntityID != id || len(kb.Skills) != 1 {
		t.Fatal("pipeline did not reuse migrated identity")
	}
	requireKnowledgeOK(t, kb.Save())
	requireKnowledgeOK(t, kb.Load())
	requireKnowledgeOK(t, pipelineTestUpdater(kb, KnowledgeActorUser).ConfirmKnowledge(result.ProposalID))
	eventCount := len(kb.Events)
	requireKnowledgeOK(t, kb.MigrateLegacyProfile())
	if len(kb.Events) != eventCount || kb.Skills[0].ID != id || kb.Skills[0].Level != SkillLevelWorking {
		t.Fatal("migration overwrote enrichment or duplicated events")
	}
	requireKnowledgeOK(t, kb.Save())
	requireKnowledgeOK(t, kb.Load())
	if pipelineSafe(t, kb).Skills[0].Level != SkillLevelWorking || !bytes.Equal(before, readKnowledgeTestFile(t, kb.ProfilePath)) {
		t.Fatal("migration/pipeline round trip failed")
	}
}

func TestPipelineCLIProposalsConfirmReject(t *testing.T) {
	kb := knowledgeTestBase(t)
	ai := pipelineTestUpdater(kb, KnowledgeActorAI)
	first, err := ai.UpdateSkill(CandidateSkillDetailed{Name: "Python"}, pipelineTestUpdate(KnowledgeSourceDerived))
	requireKnowledgeOK(t, err)
	second, err := ai.UpdateSkill(CandidateSkillDetailed{Name: "Go"}, pipelineTestUpdate(KnowledgeSourceDerived))
	requireKnowledgeOK(t, err)
	requireKnowledgeOK(t, kb.Save())
	t.Setenv("HH_CANDIDATE_PROFILE", filepath.Join(t.TempDir(), "ignored.json"))
	var out bytes.Buffer
	requireKnowledgeOK(t, runProfileCommand([]string{"knowledge", "proposals", "-candidate-profile", kb.ProfilePath}, strings.NewReader(""), &out))
	var pending []KnowledgeProposal
	requireKnowledgeOK(t, json.Unmarshal(out.Bytes(), &pending))
	if len(pending) != 2 {
		t.Fatal("CLI did not show pending values")
	}
	out.Reset()
	requireKnowledgeOK(t, runProfileCommand([]string{"-candidate-profile", kb.ProfilePath, "knowledge", "confirm", first.ProposalID}, strings.NewReader(""), &out))
	requireKnowledgeOK(t, runProfileCommand([]string{"knowledge", "reject", "-candidate-profile", kb.ProfilePath, second.ProposalID}, strings.NewReader(""), &out))
	requireKnowledgeOK(t, kb.Load())
	if kb.Proposals[0].Status != KnowledgeProposalConfirmed || kb.Proposals[1].Status != KnowledgeProposalRejected {
		t.Fatal("CLI did not persist resolutions")
	}
	if fileMode(t, filepath.Join(filepath.Dir(kb.ProfilePath), "candidate_proposals.json")).Perm() != 0o600 {
		t.Fatal("proposals are not private")
	}
	out.Reset()
	requireKnowledgeOK(t, runProfileCommand([]string{"knowledge", "proposals", "-candidate-profile", kb.ProfilePath}, strings.NewReader(""), &out))
	if strings.TrimSpace(out.String()) != "[]" {
		t.Fatal("resolved proposals listed as pending")
	}
	for _, args := range [][]string{{"knowledge"}, {"knowledge", "confirm"}, {"knowledge", "reject", "missing"}, {"knowledge", "proposals", "extra"}, {"knowledge", "unknown"}, {"knowledge", "confirm", first.ProposalID}} {
		if err := runProfileCommand(append(args, "-candidate-profile", kb.ProfilePath), strings.NewReader(""), &out); err == nil {
			t.Fatalf("invalid command accepted: %v", args)
		}
	}
	empty := knowledgeTestBase(t)
	requireKnowledgeOK(t, runProfileCommand([]string{"knowledge", "proposals", "-candidate-profile", empty.ProfilePath}, strings.NewReader(""), &out))
	entries, err := os.ReadDir(filepath.Dir(empty.ProfilePath))
	requireKnowledgeOK(t, err)
	if len(entries) != 0 {
		t.Fatal("listing proposals wrote files")
	}
}

func TestPipelineInvalidInputsAndProposalStorageFailClosed(t *testing.T) {
	for _, invalid := range []float64{-1, 2, math.NaN()} {
		kb := knowledgeTestBase(t)
		update := pipelineTestUpdate(KnowledgeSourceDerived)
		update.Confidence = &invalid
		if _, err := pipelineTestUpdater(kb, KnowledgeActorAI).UpdateSkill(CandidateSkillDetailed{Name: "Go"}, update); err == nil {
			t.Fatal("invalid confidence accepted")
		}
		if len(kb.Skills)+len(kb.Events)+len(kb.Proposals) != 0 {
			t.Fatal("failed update partially committed")
		}
	}
	kb := knowledgeTestBase(t)
	confidence := 0.8
	update := pipelineTestUpdate(KnowledgeSourceDerived)
	update.Confidence = &confidence
	_, err := pipelineTestUpdater(kb, KnowledgeActorAI).UpdateSkill(CandidateSkillDetailed{Name: "Go"}, update)
	requireKnowledgeOK(t, err)
	confidence = 1
	update.Source.Evidence[0] = "changed"
	if *kb.Proposals[0].Confidence != 0.8 || *kb.Skills[0].Confidence != 0.8 || kb.Skills[0].Sources[0].Evidence[0] == "changed" {
		t.Fatal("caller mutated stored values")
	}
	requireKnowledgeOK(t, kb.Save())
	for _, mutate := range []func(*KnowledgeProposal){
		func(p *KnowledgeProposal) { p.Status = "invalid" },
		func(p *KnowledgeProposal) { p.Source = KnowledgeSourceUserConfirmed },
		func(p *KnowledgeProposal) { p.ProposedValue = json.RawMessage(`{"confirmed":true}`) },
		func(p *KnowledgeProposal) { p.EntityID = "wrong" },
		func(p *KnowledgeProposal) { p.Reason = "api_key=must-not-persist" },
		func(p *KnowledgeProposal) { p.BaseValue = json.RawMessage(`{}`) },
	} {
		copy, err := cloneKnowledge(kb.Proposals[0])
		requireKnowledgeOK(t, err)
		mutate(&copy)
		raw := pipelineSnapshot(t, map[string]any{"version": 1, "proposals": []KnowledgeProposal{copy}})
		requireKnowledgeOK(t, os.WriteFile(filepath.Join(filepath.Dir(kb.ProfilePath), "candidate_proposals.json"), raw, 0o600))
		before, err := cloneKnowledge(*kb)
		requireKnowledgeOK(t, err)
		if kb.Load() == nil {
			t.Fatal("invalid proposal loaded")
		}
		if !reflect.DeepEqual(before, *kb) {
			t.Fatal("failed Load changed state")
		}
	}
}

func TestPipelineHHCannotUseHypothesisAsConfirmedLevel(t *testing.T) {
	for _, status := range []TruthStatus{TruthStatusHypothesis, TruthStatusVerified} {
		t.Run(string(status), func(t *testing.T) {
			kb := knowledgeTestBase(t)
			source, previous := KnowledgeSourceDerived, SkillLevelAdvanced
			if status == TruthStatusVerified {
				source, previous = KnowledgeSourceHHResume, SkillLevelBasic
			}
			requireKnowledgeOK(t, kb.AddSkill(CandidateSkillDetailed{Name: "Python", Level: previous,
				KnowledgeMetadata: knowledgeTestMetadata(source, status)}))
			before := pipelineSnapshot(t, kb.Skills)
			result, err := pipelineTestUpdater(kb, KnowledgeActorImporter).UpdateSkill(CandidateSkillDetailed{Name: "Python", Level: SkillLevelAdvanced}, pipelineTestUpdate(KnowledgeSourceHHResume))
			requireKnowledgeOK(t, err)
			if result.ProposalID == "" || !bytes.Equal(before, pipelineSnapshot(t, kb.Skills)) {
				t.Fatal("HH promoted an unconfirmed skill level")
			}
		})
	}
}

func TestPipelinePendingDuplicateIdentityCannotBeConfirmedTwice(t *testing.T) {
	kb := knowledgeTestBase(t)
	ai := pipelineTestUpdater(kb, KnowledgeActorAI)
	first, err := ai.UpdateProject(CandidateProject{Name: "API project"}, pipelineTestUpdate(KnowledgeSourceCandidateInterview))
	requireKnowledgeOK(t, err)
	second, err := ai.UpdateProject(CandidateProject{Name: "API project", Role: "developer"}, pipelineTestUpdate(KnowledgeSourceCandidateInterview))
	requireKnowledgeOK(t, err)
	user := pipelineTestUpdater(kb, KnowledgeActorUser)
	requireKnowledgeOK(t, user.ConfirmKnowledge(first.ProposalID))
	before := pipelineSnapshot(t, kb)
	if user.ConfirmKnowledge(second.ProposalID) == nil || user.ConfirmKnowledge(first.ProposalID) == nil {
		t.Fatal("duplicate or repeated confirmation accepted")
	}
	if !bytes.Equal(before, pipelineSnapshot(t, kb)) {
		t.Fatal("invalid confirmation changed knowledge")
	}
}
