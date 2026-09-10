package runtime

import (
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"

	hhwriteadapter "hh-ai-responder/internal/adapters/hh/write"
	hhwriteport "hh-ai-responder/internal/ports/hhwrite"
)

// hhWriteAdapter is composition glue only. It supplies already-resolved
// runtime values; the adapter never reads config, environment, or cookies.
func (r *HHAIResponder) hhWriteAdapter() (*hhwriteadapter.Client, error) {
	if r == nil {
		return nil, errors.New("HH writer is not configured")
	}
	httpClient := r.client
	if httpClient == nil && r.requester != nil {
		httpClient = r.requester.client
	}
	var chatURL, profileURL *url.URL
	if strings.TrimSpace(r.chatURL) != "" {
		var err error
		chatURL, err = url.Parse(r.chatURL)
		if err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(r.resumeProfileFrontURL) != "" {
		var err error
		profileURL, err = url.Parse(r.resumeProfileFrontURL)
		if err != nil {
			return nil, err
		}
	}
	return hhwriteadapter.NewClient(hhwriteadapter.Options{
		BaseURL: r.baseURL, ChatURL: chatURL, ResumeProfileURL: profileURL,
		HTTPClient: httpClient, XSRFToken: r.XSRFToken(),
	})
}

func hhWriteResultFromPort(result hhwriteport.WriteResult) HHWriteTransportResult {
	return HHWriteTransportResult{
		ExternalMessageID: result.ProviderID,
		Outcome:           result.Outcome,
		ProviderStatus:    result.ProviderStatus,
		Timestamp:         result.Timestamp,
		Metadata:          copyStringMap(result.Metadata),
	}
}

func hhWriteErrorFromPort(err error) error {
	if err == nil {
		return nil
	}
	var source *hhwriteport.TransportError
	if !errors.As(err, &source) || source == nil {
		return err
	}
	category := HHWriteErrorBadRequest
	switch source.Category {
	case hhwriteport.ErrorRequestValidation:
		category = HHWriteErrorRequestValidationFailed
	case hhwriteport.ErrorAuthentication:
		category = HHWriteErrorAuthenticationFailed
	case hhwriteport.ErrorPermission:
		category = HHWriteErrorPermissionDenied
	case hhwriteport.ErrorRateLimited:
		category = HHWriteErrorRateLimited
	case hhwriteport.ErrorServer:
		category = HHWriteErrorServerError
	case hhwriteport.ErrorNetworkAmbiguous:
		category = HHWriteErrorNetworkUncertain
	case hhwriteport.ErrorResponseAmbiguous:
		category = HHWriteErrorDeliveryUncertain
	case hhwriteport.ErrorProvider:
		category = HHWriteErrorBadRequest
	}
	return &HHWriteTransportError{
		Category: category, Outcome: source.Outcome, Status: source.Status,
		ResponseContentType: source.ResponseContentType, ResponseBody: source.ResponseBody,
		HHErrorFields: copyStringMap(source.ErrorFields), CorrelationIDs: copyStringMap(source.CorrelationIDs),
		Err: source.Err, DeliveryUncertain: source.Outcome == hhwriteport.OutcomeAmbiguous,
		Retryable: false,
	}
}

func vacancyResponseRequestFromValues(payload url.Values, refererURL string) (hhwriteport.VacancyResponseRequest, error) {
	vacancyID, err := strconv.Atoi(payload.Get("vacancy_id"))
	if err != nil || vacancyID <= 0 {
		return hhwriteport.VacancyResponseRequest{}, errors.New("vacancy preflight requires a valid vacancy_id")
	}
	request := hhwriteport.VacancyResponseRequest{
		VacancyID: vacancyID, ResumeHash: payload.Get("resume_hash"), Letter: payload.Get("letter"),
		RefererURL: refererURL, IgnorePostponed: payload.Get("ignore_postponed"),
	}
	if payload.Get("uidPk") == "" && payload.Get("guid") == "" && len(taskAnswerKeys(payload)) == 0 {
		return request, nil
	}
	test := &hhwriteport.VacancyTestSubmission{
		UIDPK: payload.Get("uidPk"), GUID: payload.Get("guid"), StartTime: payload.Get("startTime"),
		Required: payload.Get("testRequired"), Incomplete: payload.Get("incomplete"), Lux: payload.Get("lux"),
		WithoutTest: payload.Get("withoutTest"), CountryIDs: payload.Get("country_ids"),
		VisibleInVacancyCountry: payload.Get("mark_applicant_visible_in_vacancy_country"),
	}
	keys := taskAnswerKeys(payload)
	for _, key := range keys {
		taskID, parseErr := strconv.Atoi(strings.TrimPrefix(key, "task_"))
		if parseErr != nil || taskID <= 0 {
			continue
		}
		textKey := "task_" + strconv.Itoa(taskID) + "_text"
		answer := hhwriteport.VacancyTestAnswer{TaskID: taskID, ChoiceID: payload.Get(key)}
		if strings.HasSuffix(key, "_text") {
			answer.ChoiceID = ""
			answer.Text = payload.Get(key)
		} else if payload.Get(textKey) != "" {
			answer.Text = payload.Get(textKey)
		}
		test.Answers = append(test.Answers, answer)
	}
	request.Test = test
	return request, nil
}

func taskAnswerKeys(payload url.Values) []string {
	ids := map[int]struct{}{}
	for key := range payload {
		if !strings.HasPrefix(key, "task_") {
			continue
		}
		idText := strings.TrimPrefix(strings.TrimSuffix(key, "_text"), "task_")
		if id, err := strconv.Atoi(idText); err == nil && id > 0 {
			ids[id] = struct{}{}
		}
	}
	keys := make([]string, 0, len(ids))
	for id := range ids {
		keys = append(keys, "task_"+strconv.Itoa(id))
	}
	sort.Slice(keys, func(i, j int) bool {
		left, _ := strconv.Atoi(strings.TrimPrefix(keys[i], "task_"))
		right, _ := strconv.Atoi(strings.TrimPrefix(keys[j], "task_"))
		return left < right
	})
	return keys
}
