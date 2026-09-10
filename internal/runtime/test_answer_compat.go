package runtime

import (
	"encoding/json"
	"fmt"

	testanswer "hh-ai-responder/internal/usecase/testanswer"
)

// These aliases and adapters preserve the legacy root API for callers and
// tests. The implementation lives in internal/usecase/testanswer.
type TestSolutionsResponse = testanswer.Response
type TestSolution = testanswer.AIAnswer

func buildTestSystemPrompt(contacts, githubURL, extraPrompt string) string {
	system, _, err := testanswer.BuildPrompt(testanswer.Input{Contacts: contacts, GitHubURL: githubURL, ExtraPrompt: extraPrompt})
	if err != nil {
		return ""
	}
	return system
}

func testSolutionsJSONSchema() *ChatJSONSchema {
	format := testanswer.ResponseFormat()
	var schema map[string]any
	if err := json.Unmarshal(format.JSONSchema.Schema, &schema); err != nil {
		return nil
	}
	return &ChatJSONSchema{Name: format.JSONSchema.Name, Schema: schema, Strict: format.JSONSchema.Strict}
}

func legacyTestTasks(tasks []Task) []testanswer.Task {
	converted := make([]testanswer.Task, len(tasks))
	for i, task := range tasks {
		converted[i] = testanswer.Task{ID: task.ID, Description: task.Description, Multiple: task.Multiple, Open: task.Open, CandidateSolutions: make([]testanswer.Option, len(task.CandidateSolutions))}
		for j, option := range task.CandidateSolutions {
			converted[i].CandidateSolutions[j] = testanswer.Option{ID: option.ID, Text: option.Text, Title: option.Title, Value: option.Value}
		}
	}
	return converted
}

func validateTestSolutions(tasks []Task, response TestSolutionsResponse) (map[int]SolutionFields, error) {
	result, err := testanswer.Validate(legacyTestTasks(tasks), response)
	if err != nil {
		return nil, err
	}
	answers := make(map[int]SolutionFields, len(result.Answers))
	for _, answer := range result.Answers {
		answers[answer.TaskID] = SolutionFields{SolutionID: answer.SolutionID, TextSolution: answer.TextSolution, HasChoice: answer.HasChoice}
	}
	return answers, nil
}

func (c *AIClient) SolveTests(tasks []Task, contacts, githubURL, extraPrompt string) (map[int]SolutionFields, error) {
	if c == nil {
		return nil, fmt.Errorf("AI completion provider is not configured")
	}
	service := testanswer.NewService(testanswer.Dependencies{Completion: c.provider}, testanswer.Options{
		Model: c.model, Attempts: c.attempts, Temperature: 0.2, SemanticRetryDelay: aiRetryDelay,
	})
	result, err := service.Generate(c.ctx, testanswer.Input{Tasks: legacyTestTasks(tasks), Contacts: contacts, GitHubURL: githubURL, ExtraPrompt: extraPrompt})
	if err != nil {
		return nil, err
	}
	answers := make(map[int]SolutionFields, len(result.Answers))
	for _, answer := range result.Answers {
		answers[answer.TaskID] = SolutionFields{SolutionID: answer.SolutionID, TextSolution: answer.TextSolution, HasChoice: answer.HasChoice}
	}
	if len(result.Answers) == 0 {
		return nil, nil
	}
	return answers, nil
}
