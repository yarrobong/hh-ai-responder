package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"hh-ai-responder/internal/careeragent"
	"hh-ai-responder/internal/platform"
)

type CareerAgentRunReport struct {
	Version        int                         `json:"version"`
	RunID          string                      `json:"run_id"`
	Mode           string                      `json:"mode"`
	GeneratedAt    time.Time                   `json:"generated_at"`
	ResumeProfiles []careeragent.ResumeProfile `json:"resume_profiles"`
	SearchProfiles []careeragent.SearchProfile `json:"search_profiles"`
	Summary        RunSummaryResult            `json:"summary"`
	Events         []json.RawMessage           `json:"events"`
}

func runCareerAgentCommand(args []string, cfg Config, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "feedback" {
		return runCareerAgentFeedback(args[1:], cfg, stdout)
	}
	if len(args) > 0 && args[0] == "resume" {
		return runCareerAgentResumeCommand(args[1:], cfg, stdout)
	}
	if len(args) > 0 && args[0] == "resumes" {
		return runCareerAgentResumes(cfg, stdout, stderr)
	}
	// Accept both the flag spelling documented for automation and the
	// subcommand spelling used by operators. Keeping both avoids making the
	// new flow a breaking CLI change while the parser remains explicit.
	if len(args) > 0 && (args[0] == "shadow" || args[0] == "canary") {
		args = append([]string{"--" + args[0]}, args[1:]...)
	}
	fs := flag.NewFlagSet("career-agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	shadow, canary := false, false
	fs.BoolVar(&shadow, "shadow", false, "build and evaluate a report without HH writes")
	fs.BoolVar(&canary, "canary", false, "run one strictly capped live application attempt")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: career-agent --shadow | career-agent --canary | career-agent feedback ... | career-agent resumes")
	}
	if !shadow && !canary {
		shadow = true
	}
	mode := "shadow"
	if canary {
		mode = "canary"
	}
	if shadow {
		cfg.DryRun, cfg.HHWriteEnabled = true, false
		cfg.AutoApply, cfg.AutoChat, cfg.AutoTouch, cfg.AutoJobStatus = true, false, false, false
		cfg.ChatMode, cfg.RunOnce, cfg.OutputPath = "off", true, ""
	} else {
		if cfg.DryRun || !cfg.HHWriteEnabled {
			return errors.New("canary requires HH_DRY_RUN=false and HH_WRITE_ENABLED=true")
		}
		cfg.AutoApply, cfg.AutoChat, cfg.AutoTouch, cfg.AutoJobStatus = true, false, false, false
		cfg.ChatMode, cfg.RunOnce, cfg.OutputPath, cfg.AutoApplyMode = "off", true, "", "canary"
		if cfg.HHMaxWritesPerRun == 0 || cfg.HHMaxWritesPerRun > 1 {
			cfg.HHMaxWritesPerRun = 1
		}
		if cfg.HHMaxWritesPerDay == 0 || cfg.HHMaxWritesPerDay > 3 {
			cfg.HHMaxWritesPerDay = 3
		}
		if cfg.MaxApplicationsPerRun == 0 || cfg.MaxApplicationsPerRun > 1 {
			cfg.MaxApplicationsPerRun = 1
		}
	}
	if logger == nil {
		logger = NewLogger(stderr, parseLogLevel(cfg.LogLevel))
	}
	responder, err := NewHHAIResponder(context.Background(), cfg)
	if err != nil {
		return err
	}
	defer responder.closeResources()
	responder.careerAgentMode = mode
	responder.loadCareerAgentRegistry(cfg.ResumeRegistryPath)
	if len(responder.careerAgentProfiles) == 0 && len(responder.searchProfiles) == 0 {
		responder.initializeCareerAgentProfiles(cfg)
	}
	var events bytes.Buffer
	responder.eventWriter = &events
	if err := responder.ApplyVacancies(); err != nil {
		return err
	}
	report := CareerAgentRunReport{Version: 1, RunID: fmt.Sprintf("career-agent-%d", time.Now().UTC().UnixNano()), Mode: mode, GeneratedAt: time.Now().UTC(), ResumeProfiles: responder.careerAgentResumes, SearchProfiles: responder.careerAgentProfiles, Events: []json.RawMessage{}}
	if len(report.SearchProfiles) == 0 {
		report.SearchProfiles = manualCareerAgentSearchProfiles(responder.searchProfiles)
	}
	for _, line := range strings.Split(strings.TrimSpace(events.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		raw := json.RawMessage(line)
		report.Events = append(report.Events, raw)
		var kind struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &kind) == nil && kind.Type == "run_summary" {
			_ = json.Unmarshal(raw, &report.Summary)
		}
	}
	if report.Summary.Type == "" {
		report.Summary = RunSummaryResult{Type: "run_summary", Errors: 1}
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if cfg.CareerAgentResultPath != "" {
		if err := platform.WritePrivateFileAtomic(cfg.CareerAgentResultPath, append(raw, '\n'), ".career-agent-report-*.tmp"); err != nil {
			return err
		}
	}
	_, err = stdout.Write(append(raw, '\n'))
	return err
}

func manualCareerAgentSearchProfiles(values []vacancySearchProfile) []careeragent.SearchProfile {
	result := make([]careeragent.SearchProfile, 0, len(values))
	for index, value := range values {
		result = append(result, careeragent.SearchProfile{
			ID:               fmt.Sprintf("manual-search-%d", index+1),
			ResumeID:         value.Params.Get("resume"),
			ResumeTitle:      "explicit/manual search",
			Query:            value.Params.Get("text"),
			Reason:           "explicit HH_SEARCH_URL or HH_SEARCH_URLS profile",
			SearchPeriodDays: parseSearchPeriod(value.Params.Get("search_period")),
			Params:           cloneValues(value.Params),
		})
	}
	return result
}

func parseSearchPeriod(value string) int {
	period := 0
	if _, err := fmt.Sscanf(value, "%d", &period); err != nil {
		return 0
	}
	return period
}

func (r *HHAIResponder) loadCareerAgentRegistry(path string) {
	profiles := careeragent.NormalizeResumes(r.resumes)
	if path != "" {
		overrides, err := (careeragent.RegistryStore{Path: path}).Load()
		if err == nil {
			profiles = careeragent.ApplyRegistryOverrides(profiles, overrides)
		}
	}
	r.careerAgentResumes = profiles
	if r.resumeFactsByHash == nil {
		r.resumeFactsByHash = map[string]ResumeFacts{}
	}
	// NewHHAIResponder may have already planned searches before this command
	// loads local enable/disable overrides. Rebuild only generated profiles;
	// an explicit HH_SEARCH_URL/HH_SEARCH_URLS must remain untouched.
	if len(r.careerAgentProfiles) > 0 {
		r.rebuildCareerAgentSearchProfiles(profiles)
	}
}

func runCareerAgentResumes(cfg Config, stdout, stderr io.Writer) error {
	responder, err := NewHHAIResponder(context.Background(), cfg)
	if err != nil {
		return err
	}
	defer responder.closeResources()
	responder.loadCareerAgentRegistry(cfg.ResumeRegistryPath)
	return writeJSON(stdout, responder.careerAgentResumes)
}

func runCareerAgentResumeCommand(args []string, cfg Config, stdout io.Writer) error {
	fs := flag.NewFlagSet("career-agent resume", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := ""
	fs.StringVar(&id, "id", "", "stable resume registry id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || (fs.Arg(0) != "enable" && fs.Arg(0) != "disable") || strings.TrimSpace(id) == "" {
		return errors.New("usage: career-agent resume enable|disable --id <resume-id>")
	}
	store := careeragent.RegistryStore{Path: cfg.ResumeRegistryPath}
	overrides, err := store.Load()
	if err != nil {
		return err
	}
	if overrides.Enabled == nil {
		overrides.Enabled = map[string]bool{}
	}
	overrides.Enabled[id] = fs.Arg(0) == "enable"
	if err := store.Save(overrides); err != nil {
		return err
	}
	return writeJSON(stdout, map[string]any{"id": id, "enabled": overrides.Enabled[id]})
}

func runCareerAgentFeedback(args []string, cfg Config, stdout io.Writer) error {
	fs := flag.NewFlagSet("career-agent feedback", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	vacancyID, resumeID, note, kind := 0, "", "", ""
	fs.IntVar(&vacancyID, "vacancy", 0, "HH vacancy id")
	fs.StringVar(&resumeID, "resume-id", "", "selected resume registry id")
	fs.StringVar(&note, "note", "", "compact operator note")
	fs.StringVar(&kind, "type", "", "ACCEPT, REJECT, WRONG_RESUME, GOOD_MATCH, or BAD_MATCH")
	if err := fs.Parse(args); err != nil {
		return err
	}
	feedback := careeragent.Feedback{VacancyID: vacancyID, ResumeID: resumeID, Type: careeragent.FeedbackType(strings.ToUpper(strings.TrimSpace(kind))), Note: note, CreatedAt: time.Now().UTC()}
	if !careeragent.ValidFeedbackType(feedback.Type) {
		return errors.New("unsupported feedback type")
	}
	feedback.ID = careeragent.FeedbackID(feedback)
	store := &careeragent.FeedbackStore{Path: cfg.CareerAgentFeedbackPath}
	if err := store.Add(feedback); err != nil {
		return err
	}
	return writeJSON(stdout, feedback)
}
