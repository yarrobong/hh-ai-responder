package testanswer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func (a *AIAnswer) UnmarshalJSON(data []byte) error {
	var wire struct {
		TaskID       *int    `json:"task_id"`
		SolutionID   *int    `json:"solution_id"`
		TextSolution *string `json:"text_solution"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("test solution contains trailing data")
	}
	if wire.TaskID == nil {
		return errors.New("test solution is missing task_id")
	}

	a.TaskID = *wire.TaskID
	a.SolutionID = wire.SolutionID
	a.SolutionIDPresent = hasJSONField(data, "solution_id")
	a.TextSolutionPresent = hasJSONField(data, "text_solution")
	if wire.TextSolution != nil {
		a.TextSolution = *wire.TextSolution
	}
	return nil
}

func hasJSONField(data []byte, field string) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return false
	}
	_, ok := fields[field]
	return ok
}

func ParseResponse(raw string) (Response, error) {
	var response Response
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return Response{}, fmt.Errorf("ai returned invalid JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Response{}, errors.New("ai returned invalid JSON: trailing data")
		}
		return Response{}, fmt.Errorf("ai returned invalid JSON: trailing data: %w", err)
	}
	return response, nil
}

// Validate converts untrusted model output into source-ordered proposed
// answers. No answer is returned when any task or option check fails.
func Validate(tasks []Task, response Response) (Result, error) {
	expected := make(map[int]Task, len(tasks))
	for _, task := range tasks {
		if _, exists := expected[task.ID]; exists {
			return Result{}, fmt.Errorf("source test contains duplicate task_id %d", task.ID)
		}
		expected[task.ID] = task
	}
	if len(response.Solutions) != len(tasks) {
		return Result{}, fmt.Errorf("ai returned %d answers, expected %d", len(response.Solutions), len(tasks))
	}

	byID := make(map[int]ProposedAnswer, len(tasks))
	for _, item := range response.Solutions {
		task, exists := expected[item.TaskID]
		if !exists {
			return Result{}, fmt.Errorf("ai returned unknown task_id %d", item.TaskID)
		}
		if _, duplicate := byID[item.TaskID]; duplicate {
			return Result{}, fmt.Errorf("ai returned duplicate task_id %d", item.TaskID)
		}
		if item.SolutionIDPresent && item.TextSolutionPresent {
			return Result{}, fmt.Errorf("task %d has conflicting solution_id and text_solution", item.TaskID)
		}

		if len(task.CandidateSolutions) > 0 {
			if !item.SolutionIDPresent || item.SolutionID == nil {
				return Result{}, fmt.Errorf("task %d requires solution_id", item.TaskID)
			}
			if !candidateSolutionExists(task, *item.SolutionID) {
				return Result{}, fmt.Errorf("task %d has unknown solution_id %d", item.TaskID, *item.SolutionID)
			}
			byID[item.TaskID] = ProposedAnswer{TaskID: item.TaskID, SolutionID: *item.SolutionID, HasChoice: true}
			continue
		}

		if item.SolutionIDPresent {
			return Result{}, fmt.Errorf("open task %d must not use solution_id", item.TaskID)
		}
		if !item.TextSolutionPresent || strings.TrimSpace(item.TextSolution) == "" {
			return Result{}, fmt.Errorf("open task %d requires a non-empty text_solution", item.TaskID)
		}
		byID[item.TaskID] = ProposedAnswer{TaskID: item.TaskID, TextSolution: strings.TrimSpace(item.TextSolution)}
	}

	result := Result{Answers: make([]ProposedAnswer, 0, len(tasks))}
	for _, task := range tasks {
		answer, ok := byID[task.ID]
		if !ok {
			return Result{}, fmt.Errorf("ai returned no answer for task %d", task.ID)
		}
		result.Answers = append(result.Answers, answer)
	}
	return result, nil
}

func candidateSolutionExists(task Task, solutionID int) bool {
	for _, solution := range task.CandidateSolutions {
		if id, err := strconv.Atoi(strings.TrimSpace(solution.ID)); err == nil && id == solutionID {
			return true
		}
	}
	return false
}
