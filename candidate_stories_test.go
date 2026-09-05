package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCandidateStoriesSupportsArrayAndEnvelope(t *testing.T) {
	for name, content := range map[string]string{
		"array":    `[{"id":"api","title":"API integration","task":"Connect services","action":"Built integration","result":"Reduced manual work","keywords":["API"]}]`,
		"envelope": `{"version":1,"stories":[{"title":"Django tool","context":"Internal tool","action":"Implemented it","outcome":"Delivered result","technologies":["Django"]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "candidate_stories.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadCandidateStories(path)
			if err != nil || len(loaded.Stories) != 1 {
				t.Fatalf("stories: %+v, err=%v", loaded, err)
			}
		})
	}
}

func TestLoadCandidateStoriesRejectsSecretsAndIncompleteStories(t *testing.T) {
	tests := []string{
		`[{"title":"Story","task":"Use api_key=secret"}]`,
		`[{"title":""}]`,
		`[{"title":"Story"}]`,
	}
	for _, content := range tests {
		path := filepath.Join(t.TempDir(), "stories.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadCandidateStories(path); err == nil {
			t.Fatalf("invalid stories were accepted: %s", content)
		}
	}
}

func TestSelectRelevantCandidateStoriesLimitsToTwo(t *testing.T) {
	stories := []CandidateStory{
		{Title: "Python API", Task: "one", Keywords: []string{"Python", "API"}},
		{Title: "Django backend", Task: "two", Technologies: []string{"Django"}},
		{Title: "Support workflow", Task: "three", Roles: []string{"support"}},
		{Title: "Unrelated story", Task: "four", Keywords: []string{"accounting"}},
	}
	selected := selectRelevantCandidateStories(stories, Vacancy{Name: "Python/Django backend developer"}, "Build API integrations")
	if len(selected) != 2 {
		t.Fatalf("selected %d stories, want 2: %+v", len(selected), selected)
	}
	if selected[0].Title != "Python API" || selected[1].Title != "Django backend" {
		t.Fatalf("unexpected stories: %+v", selected)
	}
	if strings.Contains(candidateStoriesPrompt(selected), "Unrelated story") {
		t.Fatal("irrelevant story leaked into prompt")
	}
}

func TestCandidateStoriesAreNotAddedToChatPrompt(t *testing.T) {
	chat := buildChatSystemPrompt(ChatToReply{FirstName: "Test", LastName: "Candidate"})
	if strings.Contains(chat, "Релевантные примеры опыта") {
		t.Fatal("stories were added to chat system prompt")
	}
	if !strings.Contains(chat, "1–3 предложения") || !strings.Contains(chat, "400 символов") {
		t.Fatal("short HR chat instruction is missing")
	}
}

func TestLongHRChatReplyRequiresReview(t *testing.T) {
	longReply := strings.Repeat("длинный ответ ", 40)
	if reason := chatReplyReviewReasonForContext("auto", false, "Вопрос", "", nil, longReply); !strings.Contains(reason, "too long") {
		t.Fatalf("long reply was not routed to review: %q", reason)
	}
}

func TestProfileCommandsReadCommunicationAndStoriesWithoutCandidateProfile(t *testing.T) {
	oldStoriesPath := os.Getenv("HH_CANDIDATE_STORIES")
	t.Cleanup(func() { _ = os.Setenv("HH_CANDIDATE_STORIES", oldStoriesPath) })
	storiesPath := filepath.Join(t.TempDir(), "stories.json")
	if err := os.WriteFile(storiesPath, []byte(`[{"title":"API case","action":"Integrated API","result":"Delivered"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Setenv("HH_CANDIDATE_STORIES", storiesPath)

	var storiesOut bytes.Buffer
	if err := runProfileCommand([]string{"stories"}, strings.NewReader(""), &storiesOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(storiesOut.String(), "API case") {
		t.Fatalf("stories command output: %s", storiesOut.String())
	}

	var communicationOut bytes.Buffer
	if err := runProfileCommand([]string{"communication"}, strings.NewReader(""), &communicationOut); err != nil {
		t.Fatal(err)
	}
	if communicationOut.String() != candidateCommunicationProfile && communicationOut.String() != candidateCommunicationProfile+"\n" {
		t.Fatal("communication command did not print the embedded profile")
	}
}
