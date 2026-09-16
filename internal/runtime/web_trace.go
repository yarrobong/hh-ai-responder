package runtime

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
)

// WebPageClass is intentionally small. Every value is backed by a concrete
// page marker; an unrecognised or ambiguous HTML page remains UNKNOWN_PAGE.
type WebPageClass string

const (
	WebPageVacancyActive    WebPageClass = "VACANCY_ACTIVE"
	WebPageVacancyResponded WebPageClass = "VACANCY_RESPONDED"
	WebPageApplicationForm  WebPageClass = "APPLICATION_FORM"
	WebPageApplicationTest  WebPageClass = "APPLICATION_TEST"
	WebPageVacancyArchived  WebPageClass = "VACANCY_ARCHIVED"
	WebPageLoginRequired    WebPageClass = "LOGIN_REQUIRED"
	WebPageChallenge        WebPageClass = "CHALLENGE"
	WebPageUnknown          WebPageClass = "UNKNOWN_PAGE"
)

const (
	webTraceVacancy     = "vacancy_page"
	webTraceResponse    = "response_application_page"
	webTraceNegotiation = "negotiation_history_page"
)

// WebPageInspection contains only bounded marker results. It deliberately
// excludes response bodies, form values and hidden fields.
type WebPageInspection struct {
	Class                        WebPageClass
	LoginOrChallenge             bool
	ExplicitRespondedMarker      bool
	ExplicitApplyAction          bool
	DisabledApplyAction          bool
	ApplicationFormPresent       bool
	FormVacancyIDMatches         bool
	FormMethod                   string
	TestMarkerPresent            bool
	TestMarkerVacancyScoped      bool
	NegotiationVacancyScoped     bool
	LetterFieldPresent           bool
	ResumeSelectorPresent        bool
	StateVacancyID               int
	StateVacancyIDMatches        bool
	StateResponseMarkerPresent   bool
	StateCanApplyPresent         bool
	StateCanApply                bool
	StateTestMarkerPresent       bool
	StateTestMarkerVacancyScoped bool
}

// WebTraceRecord is the safe per-GET output contract for the diagnostic.
type WebTraceRecord struct {
	VacancyID                int
	RequestKind              string
	InitialURLPath           string
	FinalURLPath             string
	Status                   int
	ContentType              string
	PageClassification       WebPageClass
	LoginOrChallengeDetected bool
	ExplicitRespondedMarker  bool
	ExplicitApplyAction      bool
	DisabledApplyAction      bool
	ApplicationFormPresent   bool
	FormVacancyIDMatches     bool
	FormMethod               string
	TestMarkerPresent        bool
	LetterFieldPresent       bool
	ResumeSelectorPresent    bool
	NegotiationVacancyScoped bool
}

func runCareerAgentWebTraceCommand(args []string, cfg Config, stdout, stderr io.Writer) error {
	fs := newFlagSet("career-agent web-trace", stderr)
	known := ""
	unknown := ""
	fs.StringVar(&known, "known", "", "comma-separated locally confirmed responded vacancy IDs")
	fs.StringVar(&unknown, "unknown", "", "comma-separated current UNKNOWN vacancy IDs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: career-agent web-trace --known <id,id> --unknown <id,id,...>")
	}
	knownIDs, err := parseWebTraceIDs(known)
	if err != nil {
		return fmt.Errorf("known IDs: %w", err)
	}
	unknownIDs, err := parseWebTraceIDs(unknown)
	if err != nil {
		return fmt.Errorf("unknown IDs: %w", err)
	}
	if len(knownIDs) == 0 || len(unknownIDs) == 0 {
		return errors.New("both --known and --unknown must contain at least one vacancy ID")
	}
	configureCareerAgentPilotPreview(&cfg)
	if logger == nil {
		logger = NewLogger(stderr, LevelError)
	}
	r, err := NewHHAIResponder(context.Background(), cfg)
	if err != nil {
		return err
	}
	defer r.closeResources()
	for _, id := range append(append([]int(nil), knownIDs...), unknownIDs...) {
		records, traceErr := r.traceVacancyWeb(context.Background(), id)
		if traceErr != nil {
			return traceErr
		}
		for _, record := range records {
			if _, err := fmt.Fprintln(stdout, renderWebTraceRecord(record)); err != nil {
				return err
			}
		}
	}
	return nil
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func parseWebTraceIDs(value string) ([]int, error) {
	var result []int
	seen := map[int]struct{}{}
	for _, token := range strings.Split(value, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		id, err := strconv.Atoi(token)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid vacancy ID %q", token)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result, nil
}

func (r *HHAIResponder) traceVacancyWeb(ctx context.Context, vacancyID int) ([]WebTraceRecord, error) {
	if r == nil || r.requester == nil || r.baseURL == nil {
		return nil, errors.New("HH web read client is not configured")
	}
	if vacancyID <= 0 {
		return nil, errors.New("vacancy ID is required")
	}
	requests := []struct {
		kind  string
		path  string
		query url.Values
	}{
		{kind: webTraceVacancy, path: fmt.Sprintf("/vacancy/%d", vacancyID), query: url.Values{"hhtmFrom": {"web_trace"}}},
		{kind: webTraceResponse, path: "/applicant/vacancy_response", query: url.Values{"vacancyId": {strconv.Itoa(vacancyID)}, "startedWithQuestion": {"false"}, "hhtmFrom": {"vacancy"}}},
		{kind: webTraceNegotiation, path: "/applicant/negotiations", query: url.Values{"page": {"0"}}},
	}
	result := make([]WebTraceRecord, 0, len(requests))
	for _, item := range requests {
		endpoint := r.baseURL.ResolveReference(&url.URL{Path: item.path, RawQuery: item.query.Encode()})
		req, err := r.buildRequest(http.MethodGet, endpoint.String(), nil, nil)
		if err != nil {
			return nil, err
		}
		response, err := r.requester.Do(req.WithContext(ctx))
		if err != nil {
			return nil, fmt.Errorf("web trace %d %s: %w", vacancyID, item.kind, err)
		}
		inspection := inspectHHWebPage(response.Body, vacancyID, item.kind)
		finalPath := ""
		if response.FinalURL != "" {
			if parsed, parseErr := url.Parse(response.FinalURL); parseErr == nil {
				finalPath = parsed.EscapedPath()
			}
		}
		result = append(result, WebTraceRecord{
			VacancyID:                vacancyID,
			RequestKind:              item.kind,
			InitialURLPath:           endpoint.EscapedPath(),
			FinalURLPath:             finalPath,
			Status:                   response.Status,
			ContentType:              strings.TrimSpace(strings.Split(response.ContentType, ";")[0]),
			PageClassification:       inspection.Class,
			LoginOrChallengeDetected: inspection.LoginOrChallenge,
			ExplicitRespondedMarker:  inspection.ExplicitRespondedMarker,
			ExplicitApplyAction:      inspection.ExplicitApplyAction,
			DisabledApplyAction:      inspection.DisabledApplyAction,
			ApplicationFormPresent:   inspection.ApplicationFormPresent,
			FormVacancyIDMatches:     inspection.FormVacancyIDMatches,
			FormMethod:               inspection.FormMethod,
			TestMarkerPresent:        inspection.TestMarkerPresent,
			LetterFieldPresent:       inspection.LetterFieldPresent,
			ResumeSelectorPresent:    inspection.ResumeSelectorPresent,
			NegotiationVacancyScoped: inspection.NegotiationVacancyScoped,
		})
	}
	return result, nil
}

func renderWebTraceRecord(value WebTraceRecord) string {
	return fmt.Sprintf("WEB_TRACE vacancy_id=%d request_kind=%s initial_url_path=%s final_url_path=%s status=%d content_type=%s page_classification=%s login_challenge=%s explicit_responded=%s explicit_apply=%s disabled_apply=%s application_form=%s form_vacancy_id_matches=%s form_method=%s test_marker=%s letter_field=%s resume_selector=%s negotiation_vacancy_scoped=%s", value.VacancyID, value.RequestKind, value.InitialURLPath, value.FinalURLPath, value.Status, firstNonEmpty(value.ContentType, "unknown"), value.PageClassification, yesNo(value.LoginOrChallengeDetected), yesNo(value.ExplicitRespondedMarker), yesNo(value.ExplicitApplyAction), yesNo(value.DisabledApplyAction), yesNo(value.ApplicationFormPresent), yesNo(value.FormVacancyIDMatches), firstNonEmpty(value.FormMethod, "unknown"), yesNo(value.TestMarkerPresent), yesNo(value.LetterFieldPresent), yesNo(value.ResumeSelectorPresent), yesNo(value.NegotiationVacancyScoped))
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func inspectHHWebPage(data []byte, vacancyID int, requestKind string) WebPageInspection {
	inspection := WebPageInspection{Class: WebPageUnknown}
	if looksLikeAuthFailure(data) {
		inspection.LoginOrChallenge = true
		if containsAny(strings.ToLower(string(data)), "captcha", "cf-chl", "challenge", "ddos-guard") {
			inspection.Class = WebPageChallenge
		} else {
			inspection.Class = WebPageLoginRequired
		}
		return inspection
	}
	document, err := xhtml.Parse(strings.NewReader(string(data)))
	if err != nil {
		return inspection
	}
	if state, stateErr := embeddedVacancyState(data); stateErr == nil {
		if responseState, ok := directResponseState(state, vacancyID); ok {
			inspection.StateVacancyID, _ = directStateInt(responseState, "vacancyId", "vacancy_id")
			inspection.StateVacancyIDMatches = inspection.StateVacancyID == vacancyID
			if value, found := directStateValue(responseState, "alreadyResponded", "responseAlreadySent"); found {
				if parsed, parsedOK := stateBool(value); parsedOK {
					inspection.StateResponseMarkerPresent = true
					inspection.ExplicitRespondedMarker = inspection.ExplicitRespondedMarker || parsed && inspection.StateVacancyIDMatches
				}
			}
			if value, found := directStateValue(responseState, "canApply", "canRespond", "responseAllowed", "isResponseAllowed", "applyAvailable"); found {
				if parsed, parsedOK := stateBool(value); parsedOK {
					inspection.StateCanApplyPresent, inspection.StateCanApply = true, parsed
				}
			}
			if value, found := directStateValue(responseState, "testPresent", "userTestPresent", "hasTest", "testRequired"); found {
				if parsed, parsedOK := stateBool(value); parsedOK && parsed && inspection.StateVacancyIDMatches {
					inspection.StateTestMarkerPresent, inspection.StateTestMarkerVacancyScoped = true, true
				}
			}
		}
	}
	inspection.TestMarkerPresent = inspection.StateTestMarkerPresent
	inspection.TestMarkerVacancyScoped = inspection.StateTestMarkerVacancyScoped
	text := strings.ToLower(normalizeHTMLText(htmlNodeText(document)))
	inspection.ExplicitRespondedMarker = containsAny(text, "вы уже откликались", "отклик отправлен", "отклик уже отправлен")
	if requestKind == webTraceNegotiation {
		inspection.NegotiationVacancyScoped = htmlContainsVacancyID(document, vacancyID)
	}
	for _, node := range findHTMLNodes(document, func(node *xhtml.Node) bool {
		if node.Type != xhtml.ElementNode || (node.Data != "a" && node.Data != "button") {
			return false
		}
		qa := strings.ToLower(htmlAttr(node, "data-qa"))
		role := strings.ToLower(htmlAttr(node, "role"))
		label := strings.ToLower(normalizeHTMLText(htmlNodeText(node)))
		return strings.Contains(qa, "response") || strings.Contains(qa, "apply") || role == "button" && containsAny(label, "откликнуться", "apply")
	}) {
		disabled := hasHTMLAttr(node, "disabled") || strings.EqualFold(strings.TrimSpace(htmlAttr(node, "aria-disabled")), "true")
		if disabled {
			inspection.DisabledApplyAction = true
			continue
		}
		if !htmlNodeBelongsToVacancy(node, vacancyID, requestKind) {
			continue
		}
		inspection.ExplicitApplyAction = true
	}
	for _, form := range findHTMLNodes(document, func(node *xhtml.Node) bool { return node.Type == xhtml.ElementNode && node.Data == "form" }) {
		formID, hasID := formVacancyID(form)
		matches := hasID && formID == vacancyID
		if !matches {
			continue
		}
		inspection.ApplicationFormPresent = true
		inspection.FormVacancyIDMatches = true
		inspection.FormMethod = strings.ToUpper(firstNonEmpty(htmlAttr(form, "method"), http.MethodGet))
		inspection.LetterFieldPresent = formHasField(form, "letter", "cover", "сопровод")
		inspection.ResumeSelectorPresent = formHasField(form, "resume", "резюме")
		inspection.TestMarkerVacancyScoped = formHasTestMarker(form)
		inspection.TestMarkerPresent = inspection.TestMarkerVacancyScoped
	}
	if requestKind == webTraceNegotiation && inspection.NegotiationVacancyScoped {
		inspection.Class = WebPageVacancyResponded
		return inspection
	}
	if inspection.ApplicationFormPresent {
		if inspection.TestMarkerPresent {
			inspection.Class = WebPageApplicationTest
		} else {
			inspection.Class = WebPageApplicationForm
		}
		return inspection
	}
	if inspection.StateTestMarkerPresent {
		inspection.Class = WebPageApplicationTest
		return inspection
	}
	if inspection.ExplicitRespondedMarker {
		inspection.Class = WebPageVacancyResponded
		return inspection
	}
	if inspection.DisabledApplyAction && !inspection.ExplicitApplyAction {
		return inspection
	}
	if inspection.ExplicitApplyAction {
		inspection.Class = WebPageVacancyActive
		return inspection
	}
	if inspection.StateCanApplyPresent && inspection.StateCanApply {
		inspection.Class = WebPageVacancyActive
		return inspection
	}
	if containsAny(text, "вакансия в архиве", "вакансия закрыта") {
		inspection.Class = WebPageVacancyArchived
	}
	return inspection
}

func directStateInt(state map[string]any, keys ...string) (int, bool) {
	value, ok := directStateValue(state, keys...)
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		return int(typed), int(typed) > 0
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		return parsed, err == nil && parsed > 0
	default:
		return 0, false
	}
}

func htmlContainsVacancyID(document *xhtml.Node, vacancyID int) bool {
	needle := strconv.Itoa(vacancyID)
	for _, node := range findHTMLNodes(document, func(node *xhtml.Node) bool { return node.Type == xhtml.ElementNode }) {
		for _, attr := range node.Attr {
			key := strings.ToLower(attr.Key)
			if strings.Contains(key, "vacancy") || key == "href" {
				if strings.Contains(attr.Val, needle) && (strings.Contains(strings.ToLower(attr.Val), "vacancy") || strings.Contains(strings.ToLower(attr.Val), "/vacancy/")) {
					return true
				}
			}
		}
	}
	return false
}

func htmlNodeBelongsToVacancy(node *xhtml.Node, vacancyID int, requestKind string) bool {
	if requestKind == webTraceVacancy {
		return true
	}
	href := htmlAttr(node, "href")
	if href == "" {
		return false
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return false
	}
	if value := parsed.Query().Get("vacancyId"); value != "" {
		return value == strconv.Itoa(vacancyID)
	}
	return vacancyIDFromCanonicalPath(parsed.Path) == vacancyID
}

func formVacancyID(form *xhtml.Node) (int, bool) {
	for _, key := range []string{"action", "data-vacancy-id", "data-vacancyid"} {
		value := htmlAttr(form, key)
		if value == "" {
			continue
		}
		if parsed, err := url.Parse(value); err == nil {
			if id, err := strconv.Atoi(parsed.Query().Get("vacancyId")); err == nil && id > 0 {
				return id, true
			}
		}
	}
	for _, input := range findHTMLNodes(form, func(node *xhtml.Node) bool {
		return node.Type == xhtml.ElementNode && (node.Data == "input" || node.Data == "select")
	}) {
		name := strings.ToLower(htmlAttr(input, "name"))
		if name != "vacancyid" && name != "vacancy_id" && name != "vacancy" {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSpace(htmlAttr(input, "value")))
		if err == nil && id > 0 {
			return id, true
		}
	}
	return 0, false
}

func formHasField(form *xhtml.Node, values ...string) bool {
	for _, node := range findHTMLNodes(form, func(node *xhtml.Node) bool { return node.Type == xhtml.ElementNode }) {
		value := strings.ToLower(strings.Join([]string{htmlAttr(node, "name"), htmlAttr(node, "data-qa"), htmlAttr(node, "aria-label")}, " "))
		if containsAny(value, values...) {
			return true
		}
	}
	return false
}

func formHasTestMarker(form *xhtml.Node) bool {
	for _, node := range findHTMLNodes(form, func(node *xhtml.Node) bool { return node.Type == xhtml.ElementNode }) {
		value := strings.ToLower(strings.Join([]string{htmlAttr(node, "data-qa"), htmlAttr(node, "name"), htmlAttr(node, "class")}, " "))
		if containsAny(value, "test", "questionnaire", "тест", "вопрос") {
			return true
		}
	}
	return false
}

// Keep deterministic ordering available to callers that build a comparative
// table from trace records.
func sortWebTraceRecords(records []WebTraceRecord) {
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].VacancyID != records[j].VacancyID {
			return records[i].VacancyID < records[j].VacancyID
		}
		return records[i].RequestKind < records[j].RequestKind
	})
}
