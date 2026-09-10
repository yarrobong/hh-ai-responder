package runtime

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	hhreadadapter "hh-ai-responder/internal/adapters/hh/read"
	openaillm "hh-ai-responder/internal/adapters/llm/openai"
	appconfig "hh-ai-responder/internal/config"
	attemptport "hh-ai-responder/internal/ports/applicationattempt"
	autochatattemptport "hh-ai-responder/internal/ports/autochatattempt"
	hhwriteport "hh-ai-responder/internal/ports/hhwrite"
	llmport "hh-ai-responder/internal/ports/llm"
	autochatorchestration "hh-ai-responder/internal/usecase/autochatorchestration"
	hhwritegateway "hh-ai-responder/internal/usecase/hhwritegateway"
	hhwritepreflight "hh-ai-responder/internal/usecase/hhwritepreflight"
	reliabilitynotifications "hh-ai-responder/internal/usecase/reliabilitynotifications"
)

const (
	acceptHeader                  = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"
	acceptLanguageHeader          = "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7"
	botRecruiterAnswer            = "Спасибо!\nВаши ответы отправлены работодателю. Если ваш отклик его заинтересует, он напишет в этом же чате или позвонит по номеру, который вы указали."
	defaultAIAttempts             = appconfig.DefaultAIAttempts
	defaultAIBaseURL              = appconfig.DefaultAIBaseURL
	defaultAIConnectTimeout       = appconfig.DefaultAIConnectTimeout
	defaultAIModel                = appconfig.DefaultAIModel
	defaultAITimeout              = appconfig.DefaultAITimeout
	defaultHost                   = "hh.ru"
	defaultRequestInterval        = appconfig.DefaultRequestInterval
	defaultConversationDisplayTTL = appconfig.DefaultConversationDisplayTTL
	defaultWorkers                = 2
	secCHUAHeader                 = `"Chromium";v="151", "Google Chrome";v="151", "Not-A.Brand";v="99"`
	userAgent                     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36"
)

var aiRetryDelay = 3 * time.Second

type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

var (
	logger                            *Logger
	latesteResumeHashRegexp           = regexp.MustCompile(`"latestResumeHash":"([a-f0-9]{30,})"`)
	userIdRegexp                      = regexp.MustCompile(`"userId":(\d+)`)
	experienceDurationClaimPattern    = regexp.MustCompile(`(?i)(?:[0-9]+(?:[.,][0-9]+)?\s*)?(?:год(?:а|ов)?|лет|year(?:s)?)\s*(?:опыта|experience)?`)
	approximateExperienceClaimPattern = regexp.MustCompile(`(?i)(?:около|примерно|приблизительно|about|approximately)\s+(?:[0-9]+(?:[.,][0-9]+)?\s*)?(?:год(?:а|ов)?|лет|year(?:s)?)`)
)

type Config struct {
	HHReadOnly                bool // Internal capability restriction for sync/dashboard only.
	StorageBackend            string
	DatabaseURL               string
	CandidateID               string
	FollowUpPolicy            FollowUpPolicy
	SearchURL                 string
	SearchURLs                []string
	CookiesPath               string
	LogLevel                  string
	Resume                    string
	MaxResponses              int
	AIBaseURL                 string
	AIModel                   string
	AIAPIKey                  string
	EmbeddingProvider         string
	EmbeddingBaseURL          string
	EmbeddingAPIKey           string
	EmbeddingModel            string
	EmbeddingDimensions       int
	AITimeout                 time.Duration
	AIConnectTimeout          time.Duration
	AIAttempts                int
	ExtraLetterPrompt         string
	ExtraTestSolutionPrompt   string
	HHReadConcurrency         int
	RequestInterval           time.Duration
	OutputPath                string
	Contacts                  string
	ListResumes               bool
	ForceLetter               bool
	ExtraChatReplyPrompt      string
	GithubURL                 string
	DryRun                    bool
	HHWriteEnabled            bool
	HHChatURL                 string
	HHMaxWritesPerRun         int
	HHMaxWritesPerDay         int
	AutoApply                 bool
	AutoChat                  bool
	AutoTouch                 bool
	AutoJobStatus             bool
	ChatMode                  string
	MinSalary                 int
	MinSalaryCurrency         string
	IncludeKeywords           []string
	ExcludeKeywords           []string
	MinMatchScore             int
	RunOnce                   bool
	MaxVacanciesPerRun        int
	MaxApplicationsPerRun     int
	MaxConversationsPerRun    int
	AlreadyRespondedStatePath string
	CandidateProfilePath      string
	CandidateStoriesPath      string
	HHSyncStatePath           string
	MonitorInterval           time.Duration
	MonitorQuietHours         string
	NotificationCooldown      time.Duration
	ConversationDisplayTTL    time.Duration
	BackgroundInboxRefresh    bool
}

type LegacyCandidateContext struct {
	FullName                   string
	ResumeTitle                string
	Salary                     string
	Experience                 string
	Skills                     string
	Location                   string
	Contacts                   string
	EducationKnown             bool
	EducationLevel             string
	EducationDetails           string
	TotalExperienceMonthsKnown bool
	TotalExperienceMonths      int
	Profile                    CandidateProfile               `json:"candidate_profile,omitempty"`
	Stories                    []CandidateStory               `json:"stories,omitempty"`
	SafeContext                CandidateContext               `json:"-"`
	SafeKnowledge              EmployerSafeCandidateKnowledge `json:"-"`
}

// type Compensation struct {
// 	From         int    `json:"from"`
// 	To           int    `json:"to"`
// 	CurrencyCode string `json:"currencyCode"`
// }

type VacancyTest struct {
	UIDPk       string `json:"uidPk"`
	GUID        string `json:"guid"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    string `json:"required"`
	StartTime   string `json:"startTime"`
	Tasks       []Task `json:"tasks"`
}

type Task struct {
	ID                 int        `json:"id"`
	Description        string     `json:"description"`
	Multiple           string     `json:"multiple"`
	Open               string     `json:"open"`
	CandidateSolutions []Solution `json:"candidateSolutions"`
}

type Solution struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Title string `json:"title"`
	Value string `json:"value"`
}

type SolutionFields struct {
	SolutionID   int
	TextSolution string
	HasChoice    bool
}

type ApplyResult struct {
	Type           string    `json:"type"`
	Resume         string    `json:"resume"`
	ResumeTitle    string    `json:"resume_title"`
	VacancyID      int       `json:"vacancy_id"`
	URL            string    `json:"url"`
	Name           string    `json:"name"`
	Letter         string    `json:"letter"`
	AppliedAt      time.Time `json:"applied_at"`
	ResponsesCount *int      `json:"responses_count,omitempty"`
	TestSolutions  []QAPair  `json:"test_solutions,omitempty"`
}

type ChatResult struct {
	Type             string    `json:"type"`
	Resume           string    `json:"resume"`
	ResumeTitle      string    `json:"resume_title"`
	ChatId           int64     `json:"chat_id"`
	AttemptID        string    `json:"attempt_id,omitempty"`
	ConversationID   string    `json:"conversation_id,omitempty"`
	TriggerMessageID string    `json:"trigger_message_id,omitempty"`
	EmployerMsg      string    `json:"employer_message"`
	Reply            string    `json:"reply"`
	ReviewReason     string    `json:"review_reason,omitempty"`
	SentAt           time.Time `json:"sent_at"`
}

type ResumeTouchResult struct {
	Type        string    `json:"type"`
	Resume      string    `json:"resume"`
	ResumeTitle string    `json:"resume_title"`
	Updated     bool      `json:"updated"`
	Time        time.Time `json:"time"`
}

type ErrorResult struct {
	Type    string         `json:"type"`
	Context map[string]any `json:"context"`
	Error   string         `json:"error"`
	Time    time.Time      `json:"time"`
}

type QAPair struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// ===== Chat API Types =====
type ChatsResponse struct {
	Chats            ChatsList                  `json:"chats"`
	ChatsDisplayInfo map[string]ChatDisplayInfo `json:"chatsDisplayInfo"`
	Resources        ChatsResources             `json:"resources"`
}

type ChatsList struct {
	NextFrom string         `json:"nextFrom"`
	Page     int            `json:"page"`
	PerPage  int            `json:"per_page"`
	Pages    int            `json:"pages"`
	Items    []ChatListItem `json:"items"`
}

type ChatListItem struct {
	Id                               int64             `json:"id"`
	Type                             string            `json:"type"`
	SubType                          interface{}       `json:"subType"`
	UnreadCount                      int               `json:"unreadCount"`
	Resources                        ChatItemResources `json:"resources"`
	Pinned                           bool              `json:"pinned"`
	NotificationEnabled              bool              `json:"notificationEnabled"`
	OwnerViolatesRules               bool              `json:"ownerViolatesRules"`
	CurrentParticipantID             string            `json:"currentParticipantId"`
	LastMessage                      *ChatMessage      `json:"lastMessage,omitempty"`
	LastViewedByOpponentMessageID    int64             `json:"lastViewedByOpponentMessageId"`
	LastViewedByCurrentUserMessageID *int64            `json:"lastViewedByCurrentUserMessageId"`
	ParticipantsIDs                  []string          `json:"participantsIds"`
	OnlineUntilTime                  *time.Time        `json:"onlineUntilTime"`
	LastActivityTime                 time.Time         `json:"lastActivityTime"`
}

type ChatDataResponse struct {
	Chat             ChatDetail        `json:"chat"`
	Resources        ExtendedResources `json:"resources"`
	MissingResources MissingResources  `json:"missingResources"`
	Display          ChatDisplay       `json:"display"`
	ChatStates       ChatStates        `json:"chatStates"`
	Suggestions      Suggestions       `json:"suggestions"`
	HasButtons       bool              `json:"hasMessagesWithTextButtons"`
	CallAvailable    bool              `json:"callAvailable"`
	TopicStates      interface{}       `json:"negotiationTopicsAvailableStates"`
}

type ChatDetail struct {
	ID                               int64             `json:"id"`
	Type                             string            `json:"type"`
	SubType                          interface{}       `json:"subType"`
	BlockInfo                        interface{}       `json:"blockInfo"`
	UnreadCount                      int               `json:"unreadCount"`
	Resources                        ChatItemResources `json:"resources"`
	Pinned                           bool              `json:"pinned"`
	NotificationEnabled              bool              `json:"notificationEnabled"`
	WritePossibility                 WritePossibility  `json:"writePossibility"`
	Operations                       Operations        `json:"operations"`
	OwnerViolatesRules               bool              `json:"ownerViolatesRules"`
	Messages                         ChatMessages      `json:"messages"`
	CurrentParticipantID             string            `json:"currentParticipantId"`
	LastViewedByOpponentMessageID    int64             `json:"lastViewedByOpponentMessageId"`
	LastViewedByCurrentUserMessageID *int64            `json:"lastViewedByCurrentUserMessageId"`
	ParticipantsIDs                  []string          `json:"participantsIds"`
	OnlineUntilTime                  *time.Time        `json:"onlineUntilTime"`
	LastActivityTime                 time.Time         `json:"lastActivityTime"`
}

type WritePossibility struct {
	Name                 string   `json:"name"`
	WriteDisabledReasons []string `json:"writeDisabledReasons"`
}

type Operations struct {
	Enabled []string `json:"enabled"`
}

type ChatItemResources struct {
	Vacancy          []string `json:"VACANCY"`
	NegotiationTopic []string `json:"NEGOTIATION_TOPIC"`
	Resume           []string `json:"RESUME"`
	Unknown          []string `json:"UNKNOWN"`
}

type ChatMessages struct {
	Items   []ChatMessage `json:"items"`
	HasMore bool          `json:"hasMore"`
}

type ChatMessage struct {
	ID                   int64               `json:"id"`
	ChatID               int64               `json:"chatId"`
	CreationTime         time.Time           `json:"creationTime"`
	Text                 string              `json:"text"`
	Resources            ChatItemResources   `json:"resources,omitempty"`
	Type                 string              `json:"type"`
	CanEdit              bool                `json:"canEdit"`
	CanDelete            bool                `json:"canDelete"`
	WorkflowTransitionID int64               `json:"workflowTransitionId"`
	OnlyVisibleForMyType bool                `json:"onlyVisibleForMyType"`
	Flags                MessageFlags        `json:"flags"`
	HasContent           bool                `json:"hasContent"`
	Hidden               bool                `json:"hidden"`
	WorkflowTransition   *WorkflowTransition `json:"workflowTransition"`
	ParticipantDisplay   ParticipantDisplay  `json:"participantDisplay"`
	ParticipantID        string              `json:"participantId"`
	Actions              *MessageActions     `json:"actions,omitempty"`
}

type MessageActions struct {
	TextButtons []TextButton `json:"text_buttons"`
}

type TextButton struct {
	Size string `json:"size"`
	Text string `json:"text"`
}

type MessageFlags struct {
	ShouldCheckLinks bool `json:"shouldCheckLinks"`
}

type WorkflowTransition struct {
	ID                  int64  `json:"id"`
	TopicID             int64  `json:"topicId"`
	ApplicantState      string `json:"applicantState"`
	DeclinedByApplicant bool   `json:"declinedByApplicant"`
}

type ParticipantDisplay struct {
	Name   string `json:"name"`
	IsBot  bool   `json:"isBot"`
	Avatar string `json:"avatar,omitempty"`
}

type ExtendedResources struct {
	Vacancies         map[string]ChatDetailVacancy `json:"vacancies"`
	Employers         map[string]interface{}       `json:"employers"`
	Resumes           map[string]Resume            `json:"resumes"`
	ResumeHashById    map[string]string            `json:"resumeHashById"`
	Participants      map[string]ParticipantDetail `json:"participants"`
	NegotiationTopics map[string]NegotiationTopic  `json:"negotiation_topics"`
	Addresses         map[string]interface{}       `json:"addresses"`
	TestSolutions     map[string]interface{}       `json:"test_solutions"`
	FileInfoByUpload  map[string]interface{}       `json:"file_info_by_upload_ids"`
	EmployerAssistant map[string]interface{}       `json:"employer_assistant"`
}

type ChatDetailVacancy struct {
	WorkSchedule            string             `json:"@workSchedule"`
	ShowContact             bool               `json:"@showContact"`
	ResponseLetterRequired  bool               `json:"@responseLetterRequired"`
	VacancyID               int64              `json:"vacancyId"`
	Name                    string             `json:"name"`
	Company                 ChatDetailCompany  `json:"company"`
	Compensation            Compensation       `json:"compensation"`
	PublicationTime         CustomTime         `json:"publicationTime"`
	Area                    Area               `json:"area"`
	AcceptTemporary         bool               `json:"acceptTemporary"`
	CreationSite            string             `json:"creationSite"`
	CreationSiteID          int                `json:"creationSiteId"`
	DisplayHost             string             `json:"displayHost"`
	LastChangeTime          CustomTime         `json:"lastChangeTime"`
	CreationTime            time.Time          `json:"creationTime"`
	CanBeShared             bool               `json:"canBeShared"`
	EmployerManager         interface{}        `json:"employerManager"`
	InboxPossibility        bool               `json:"inboxPossibility"`
	ChatWritePossibility    string             `json:"chatWritePossibility"`
	Notify                  bool               `json:"notify"`
	Links                   Links              `json:"links"`
	AcceptIncompleteResumes bool               `json:"acceptIncompleteResumes"`
	DriverLicenseTypes      []interface{}      `json:"driverLicenseTypes"`
	Languages               []interface{}      `json:"languages"`
	WorkingDays             []interface{}      `json:"workingDays"`
	WorkingTimeIntervals    []interface{}      `json:"workingTimeIntervals"`
	WorkingTimeModes        []interface{}      `json:"workingTimeModes"`
	VacancyProperties       VacancyProperties  `json:"vacancyProperties"`
	VacancyPlatforms        []string           `json:"vacancyPlatforms"`
	ProfessionalRoleIds     []ProfessionalRole `json:"professionalRoleIds"`
	WorkExperience          string             `json:"workExperience"`
	Employment              Employment         `json:"employment"`
	ClosedForApplicants     bool               `json:"closedForApplicants"`
	UserTestPresent         bool               `json:"userTestPresent"`
	EmploymentForm          string             `json:"employmentForm"`
	FlyInFlyOutDurations    []interface{}      `json:"flyInFlyOutDurations"`
	Internship              bool               `json:"internship"`
	NightShifts             bool               `json:"nightShifts"`
	WorkFormats             []WorkFormat       `json:"workFormats"`
	WorkScheduleByDays      []WorkScheduleDays `json:"workScheduleByDays"`
	WorkingHours            []WorkingHours     `json:"workingHours"`
	ExperimentalModes       []ExperimentalMode `json:"experimentalModes"`
	AcceptLaborContract     bool               `json:"acceptLaborContract"`
	CivilLawContracts       []interface{}      `json:"civilLawContracts"`
	AutoResponse            AutoResponse       `json:"autoResponse"`
	InclusivenessTypes      []interface{}      `json:"inclusivenessTypes"`
}

type ChatDetailCompany struct {
	ShowSimilarVacancies      bool   `json:"@showSimilarVacancies"`
	Trusted                   bool   `json:"@trusted"`
	Category                  string `json:"@category"`
	CountryID                 int    `json:"@countryId"`
	State                     string `json:"@state"`
	ID                        int64  `json:"id"`
	Name                      string `json:"name"`
	VisibleName               string `json:"visibleName"`
	Logos                     Logos  `json:"logos"`
	EmployerOrganizationForm  int    `json:"employerOrganizationFormId"`
	ShowOrganizationForm      bool   `json:"showOrganizationForm"`
	Badges                    Badges `json:"badges"`
	CompanySiteURL            string `json:"companySiteUrl"`
	AccreditedITEmployer      bool   `json:"accreditedITEmployer"`
	EmployerOnAdditionalCheck bool   `json:"employerOnAdditionalCheck"`
}

type Logos struct {
	Logo []LogoItem `json:"logo"`
}

type LogoItem struct {
	Type string `json:"@type"`
	URL  string `json:"@url"`
}

type Badges struct {
	Badge []BadgeItem `json:"badge"`
}

type BadgeItem struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	URL         string `json:"url"`
}

// type Compensation struct {
// 	NoCompensation interface{} `json:"noCompensation"`
// }

type CustomTime struct {
	Timestamp int64     `json:"@timestamp"`
	Value     time.Time `json:"$"`
}

type Area struct {
	ID   int64  `json:"@id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type Links struct {
	Desktop string `json:"desktop"`
	Mobile  string `json:"mobile"`
}

type VacancyProperties struct {
	Properties       []PropertyBlock  `json:"properties"`
	CalculatedStates CalculatedStates `json:"calculatedStates"`
}

type PropertyBlock struct {
	Property []PropertyItem `json:"property"`
}

type PropertyItem struct {
	ID             int64        `json:"id"`
	PropertyType   string       `json:"propertyType"`
	Defining       bool         `json:"defining,omitempty"`
	Classifying    bool         `json:"classifying,omitempty"`
	Bundle         string       `json:"bundle"`
	PropertyWeight int          `json:"propertyWeight"`
	Parameters     []ParamBlock `json:"parameters"`
	StartTimeIso   time.Time    `json:"startTimeIso"`
	EndTimeIso     time.Time    `json:"endTimeIso"`
}

type ParamBlock struct {
	Parameter []ParamItem `json:"parameter"`
}

type ParamItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type CalculatedStates struct {
	HH StateDetail `json:"HH"`
	ZP StateDetail `json:"ZP"`
}

type StateDetail struct {
	Advertising           bool     `json:"advertising"`
	Anonymous             bool     `json:"anonymous"`
	CrosspostedTo         []string `json:"crosspostedTo,omitempty"`
	CrosspostedFrom       string   `json:"crosspostedFrom,omitempty"`
	FilteredPropertyNames []string `json:"filteredPropertyNames"`
	Free                  bool     `json:"free"`
	Optimum               bool     `json:"optimum"`
	OptionPremium         bool     `json:"optionPremium"`
	PayForPerformance     bool     `json:"payForPerformance"`
	Premium               bool     `json:"premium"`
	Standard              bool     `json:"standard"`
	StandardPlus          bool     `json:"standardPlus"`
	TranslationKeys       []string `json:"translationKeys"`
}

type ProfessionalRole struct {
	ProfessionalRoleId []int `json:"professionalRoleId"`
}

type Employment struct {
	Type string `json:"@type"`
}

type WorkFormat struct {
	WorkFormatsElement []string `json:"workFormatsElement"`
}

type WorkScheduleDays struct {
	WorkScheduleByDaysElement []string `json:"workScheduleByDaysElement"`
}

type WorkingHours struct {
	WorkingHoursElement []string `json:"workingHoursElement"`
}

type ExperimentalMode struct {
	ExperimentalMode []string `json:"experimentalMode"`
}

type AutoResponse struct {
	AcceptAutoResponse bool `json:"acceptAutoResponse"`
}

type Resume struct {
	Hash         string      `json:"hash"`
	ID           int64       `json:"id"`
	UserID       int64       `json:"userId"`
	Title        string      `json:"title"`
	HiddenFields []string    `json:"hiddenFields"`
	Gender       string      `json:"gender"`
	Phone        []PhoneItem `json:"phone"`
}

type PhoneItem struct {
	Type             string      `json:"type"`
	Country          string      `json:"country"`
	City             string      `json:"city"`
	Number           string      `json:"number"`
	Formatted        string      `json:"formatted"`
	Raw              string      `json:"raw"`
	Verified         bool        `json:"verified"`
	NeedVerification bool        `json:"needVerification"`
	Comment          interface{} `json:"comment"`
}

type ParticipantDetail struct {
	ID               int64         `json:"id"`
	ExternalID       string        `json:"externalId"`
	Type             string        `json:"type"`
	IsCurrentUser    bool          `json:"isCurrentUser"`
	Key              string        `json:"key"`
	Display          UserDisplay   `json:"display"`
	EmployerManager  int64         `json:"employerManagerId,omitempty"`
	LastActivityTime DateTimeBlock `json:"lastActivityTime"`
	OnlineUntilTime  DateTimeBlock `json:"onlineUntilTime"`
	EntityID         int64         `json:"entityId"`
	Role             UserRole      `json:"role"`
}

type UserDisplay struct {
	Name   string `json:"name"`
	Avatar string `json:"avatar,omitempty"`
}

type DateTimeBlock struct {
	DateTime time.Time `json:"dt"`
}

type UserRole struct {
	Name    string `json:"name"`
	Display string `json:"display"`
}

type NegotiationTopic struct {
	TopicID               int64  `json:"topicId"`
	VacancyID             int64  `json:"vacancyId"`
	ResumeID              int64  `json:"resumeId"`
	InitialTopicType      string `json:"initialTopicType"`
	CurrentTopicType      string `json:"currentTopicType"`
	InitialApplicantState string `json:"initialApplicantState"`
	CurrentApplicantState string `json:"currentApplicantState"`
}

type MissingResources struct {
	Vacancies         interface{} `json:"vacancies"`
	Employers         interface{} `json:"employers"`
	Resumes           interface{} `json:"resumes"`
	Participants      interface{} `json:"participants"`
	NegotiationTopics interface{} `json:"negotiation_topics"`
	Addresses         interface{} `json:"addresses"`
	TestSolutions     interface{} `json:"test_solutions"`
	FileUploadIDs     interface{} `json:"file_upload_ids"`
}

type ChatDisplay struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Icon     string `json:"icon"`
}

type ChatStates struct {
	WriteMessageState     StateAllowed `json:"writeMessageState"`
	ResponseReminderState StateAllowed `json:"responseReminderState"`
	SendFileState         StateAllowed `json:"sendFileState"`
}

type StateAllowed struct {
	Allowed bool        `json:"allowed"`
	Reasons []string    `json:"reasons,omitempty"`
	Reason  interface{} `json:"reason,omitempty"`
}

type Suggestions struct {
	UUID              string            `json:"uuid"`
	SuggestionOptions SuggestionOptions `json:"suggestionOptions"`
}

type SuggestionOptions struct {
	Options []interface{} `json:"options"`
}

type ChatDisplayInfo struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Icon     string `json:"icon,omitempty"`
}

type ChatsResources struct {
	Vacancies           map[string]ChatVacancyResource      `json:"vacancies"`
	Employers           map[string]json.RawMessage          `json:"employers"`
	Resumes             map[string]ChatResumeResource       `json:"resumes"`
	ResumeHashById      map[string]string                   `json:"resumeHashById"`
	Participants        map[string]json.RawMessage          `json:"participants"`
	NegotiationTopics   map[string]ChatNegotiationTopic     `json:"negotiation_topics"`
	Addresses           map[string]json.RawMessage          `json:"addresses"`
	TestSolutions       map[string]ChatTestSolutionResource `json:"test_solutions"`
	FileInfoByUploadIds map[string]json.RawMessage          `json:"file_info_by_upload_ids"`
	EmployerAssistant   map[string]json.RawMessage          `json:"employer_assistant"`
}

type ChatResumeResource struct {
	Area             int           `json:"area"`
	LastName         string        `json:"lastName"`
	FieldsViewStatus *string       `json:"fieldsViewStatus"`
	PhotoUrls        ChatPhotoUrls `json:"photoUrls"`
	Gender           string        `json:"gender"`
	Permission       string        `json:"permission"`
	Salary           *ChatSalary   `json:"salary"`
	Title            string        `json:"title"`
	UserId           int64         `json:"userId"`
	AccessType       string        `json:"accessType"`
	FirstName        string        `json:"firstName"`
	HiddenFields     []string      `json:"hiddenFields"`
	Phone            []ChatPhone   `json:"phone"`
	MiddleName       string        `json:"middleName"`
	Id               int64         `json:"id"`
	Hash             string        `json:"hash"`
	TopicVacancyId   int64         `json:"topic_vacancy_id"`
}

type ChatPhotoUrls struct {
	Id      int64   `json:"id"`
	State   string  `json:"state"`
	Title   *string `json:"title"`
	Big     string  `json:"big"`
	Large   string  `json:"large"`
	Preview string  `json:"preview"`
	Avatar  string  `json:"avatar"`
}

type ChatSalary struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type ChatPhone struct {
	Type             string  `json:"type"`
	Country          string  `json:"country"`
	City             string  `json:"city"`
	Number           string  `json:"number"`
	Formatted        string  `json:"formatted"`
	Raw              string  `json:"raw"`
	Verified         bool    `json:"verified"`
	NeedVerification bool    `json:"needVerification"`
	Comment          *string `json:"comment"`
}

type ChatNegotiationTopic struct {
	TopicId               int64  `json:"topicId"`
	VacancyId             int64  `json:"vacancyId"`
	ResumeId              int64  `json:"resumeId"`
	InitialTopicType      string `json:"initialTopicType"`
	CurrentTopicType      string `json:"currentTopicType"`
	InitialApplicantState string `json:"initialApplicantState"`
	CurrentApplicantState string `json:"currentApplicantState"`
}

type ChatTestSolutionResource struct {
	UidPk    int64  `json:"uidPk"`
	Score    int    `json:"score"`
	Mark     string `json:"mark"`
	Examined bool   `json:"examined"`
}

type ChatVacancyResource struct {
	VacancyID int64  `json:"vacancyId"`
	Name      string `json:"name"`

	Company struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		SiteURL string `json:"companySiteUrl,omitempty"`
		Trusted bool   `json:"trusted,omitempty"`
	} `json:"company"`
	Links        VacancyLinks  `json:"links"`
	Compensation *Compensation `json:"compensation,omitempty"`
}

type VacancyLinks struct {
	Desktop string `json:"desktop"`
	Mobile  string `json:"mobile"`
}

// ===== Chat API Methods =====
// TODO: там есть вебсокеты для получения новых сообщений в реальном времени
func (r *HHAIResponder) GetChats(page int) (*ChatsResponse, error) {
	return r.getChatsThroughReadAdapter(page)
}

func (r *HHAIResponder) GetChatData(chatID int64, applicantID int64) (*ChatDataResponse, error) {
	return r.getChatDataThroughReadAdapter(chatID, applicantID)
}

func generateUUIDv4() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4],
		b[4:6],
		b[6:8],
		b[8:10],
		b[10:16],
	), nil
}

func (r *HHAIResponder) SendChatMessage(chatID int64, text string) (map[string]any, error) {
	if r.dryRun {
		return map[string]any{"dry_run": true}, nil
	}
	if !r.chatSendingAllowed() {
		return map[string]any{"disabled": true}, nil
	}
	uuid, err := generateUUIDv4()
	if err != nil {
		return nil, err
	}
	service, err := r.newLegacyWriteService()
	if err != nil {
		return nil, err
	}
	result, err := service.SendChatMessage(ctxOrBackground(r.ctx), hhwritegateway.ChatMessageRequest{ConversationID: strconv.FormatInt(chatID, 10), Text: text, IdempotencyKey: uuid})
	if err != nil {
		return nil, hhWriteErrorFromPort(err)
	}
	return map[string]any{"messageId": result.ProviderID, "metadata": result.Metadata}, nil
}

func (r *HHAIResponder) LeaveChat(chatId int64) (map[string]any, error) {
	if r.dryRun {
		return map[string]any{"dry_run": true}, nil
	}
	if !r.chatSendingAllowed() {
		return map[string]any{"disabled": true}, nil
	}
	service, err := r.newLegacyWriteService()
	if err != nil {
		return nil, err
	}
	result, err := service.LeaveChat(ctxOrBackground(r.ctx), hhwriteport.ChatLeaveRequest{ConversationID: strconv.FormatInt(chatId, 10)})
	if err != nil {
		return nil, hhWriteErrorFromPort(err)
	}
	return map[string]any{"status": result.ProviderStatus, "metadata": result.Metadata}, nil
}

type ChatToReply struct {
	ChatId              int64
	TriggerMessageID    string
	ContactName         string
	ReplyToMessage      string
	VacancyName         string
	VacancyURL          string
	CompanyName         string
	VacancyCompensation string
	ReplyOptions        []string
	ResumeID            int64
	ResumeHash          string
	ResumeTitle         string
	ResumeExperience    string
	ApplicantId         int64
	FirstName           string
	LastName            string
	Salary              string
	Skills              string
	IsDiscard           bool
	AlwaysEmphasize     string
	AvoidClaiming       string
	CandidateContext    CandidateContext `json:"-"`
}

func (r *HHAIResponder) getChatsAwaitingReply(ctx context.Context, maxPages int) ([]ChatToReply, error) {
	resume := r.GetCurrentResume()
	if resume == nil {
		return nil, errors.New("resume not found")
	}
	baseCandidate, canonicalResolver, err := r.canonicalCandidateContextContext(ctxOrBackground(ctx), *resume)
	if err != nil {
		return nil, fmt.Errorf("canonical candidate read failed: %w", err)
	}

	pages := 1
	var results []ChatToReply

	// ЭТАП 1: Загрузка и первичная фильтрация чатов
	for page := 0; page < pages; page++ {
		chatsResponse, err := r.getChatsThroughReadAdapterContext(ctxOrBackground(ctx), page)
		if err != nil {
			return nil, err
		}

		chats := chatsResponse.Chats

		if len(chats.Items) == 0 {
			logger.Warn("Empty chat list!")
			break
		}

		// var resume ChatResumeResource
		var resumeExists bool
		// resume, exists = chatsResponse.Resources.Resumes[chat.Resources.Resume[0]]
		// if !exists {
		// Фолбечное резюме, если то, с которого был отклик, удалено
		_, resumeExists = chatsResponse.Resources.Resumes[fmt.Sprint(resume.Id)]
		if !resumeExists {
			//return nil, fmt.Errorf("Resume doesn't exists: %s", resumeId)
			continue
		}
		// }

		pages = min(maxPages, chats.Pages)

		for _, chat := range chats.Items {

			// Общение со всем резюме пусть
			// Последнее сообщение свое
			// if len(chat.Resources.Resume) == 0 || !slices.Contains(chat.Resources.Resume, resumeId) {
			// 	continue
			// }

			last := chat.LastMessage

			if last == nil {
				continue
			}
			if r.isIgnoredChatTrigger(chat.Id, messageExternalID(last.ID)) {
				continue
			}

			// На чаты старше 3-х дней не отвечаем
			if time.Since(last.CreationTime) > 72*time.Hour {
				return results, nil
			}

			// Пропускаем чаты, где соискатель писал последним
			participantId, _ := strconv.ParseInt(last.ParticipantID, 10, 64)
			if r.userId == participantId {
				logger.Debug("Skip chat #%d without response", chat.Id)
				continue
			}

			if last.Text == botRecruiterAnswer {
				continue
			}

			if len(chat.Resources.Vacancy) == 0 || len(chat.Resources.Resume) == 0 {
				continue
			}

			if !slices.Contains(chat.Resources.Resume, strconv.FormatInt(resume.Id, 10)) {
				continue
			}
			candidate := baseCandidate
			candidateContext, err := canonicalResolver.ResolveForEmployerMessage(last.Text, nil)
			if err != nil {
				logger.Warn("Skip chat %d: employer-safe candidate context unavailable: %v", chat.Id, err)
				continue
			}
			alwaysEmphasize, avoidClaiming := candidate.Profile.TrustedCommunicationRules()

			vacancy, vacancyExists := chatsResponse.Resources.Vacancies[chat.Resources.Vacancy[0]]
			if !vacancyExists {
				continue
			}

			var options []string
			if chat.LastMessage.Actions != nil {
				for _, button := range chat.LastMessage.Actions.TextButtons {
					options = append(options, button.Text)
				}
			}

			// В принципе можно сделать общение во всех чатах, но сейчас под резюме
			// сделано
			chatInfo := ChatToReply{
				ChatId:              chat.Id,
				TriggerMessageID:    messageExternalID(last.ID),
				ContactName:         last.ParticipantDisplay.Name,
				ReplyToMessage:      last.Text,
				ReplyOptions:        options,
				VacancyName:         vacancy.Name,
				VacancyURL:          vacancy.Links.Desktop,
				CompanyName:         vacancy.Company.Name,
				VacancyCompensation: strings.Replace(FormatCompensation(vacancy.Compensation), "RUR", "руб", 1),
				ApplicantId:         r.userId,
				FirstName:           r.firstName,
				LastName:            r.lastName,
				ResumeExperience:    candidate.Experience,
				ResumeID:            resume.Id,
				ResumeHash:          resume.Hash,
				ResumeTitle:         resume.Title,
				Skills:              candidate.Skills,
				Salary:              candidate.Salary,
				AlwaysEmphasize:     alwaysEmphasize,
				AvoidClaiming:       avoidClaiming,
				CandidateContext:    candidateContext,
			}

			if last.WorkflowTransition != nil && last.WorkflowTransition.ApplicantState == "DISCARD" {
				chatInfo.IsDiscard = true
			}

			//logger.Debug("append chat #%d", chat.ID)
			results = append(results, chatInfo)
		}
	}

	return results, nil
}

// ===== Auto Chat Responder =====
func JoinChatMessages(response *ChatDataResponse) string {
	var sb strings.Builder
	items := response.Chat.Messages.Items

	for _, msg := range items {
		timeStr := msg.CreationTime.Format("2006-01-02 15:04:05")
		author := msg.ParticipantDisplay.Name

		sb.WriteString(fmt.Sprintf("[%s] %s\n", timeStr, author))
		if msg.Text != "" {
			sb.WriteString(strings.TrimSpace(msg.Text))
			sb.WriteString("\n")
		}

		sb.WriteString("---\n")
	}

	return sb.String()
}

func (r *HHAIResponder) AutoRespondChats() error {
	if r.effectiveChatMode() == "off" {
		logger.Info("Chat replies disabled by configuration")
		return nil
	}
	_, err := r.newAutoChatOrchestrationService().Run(ctxOrBackground(r.ctx), autochatorchestration.Input{})
	if err != nil {
		return err
	}
	return nil
}

func (r *HHAIResponder) effectiveChatMode() string {
	if r.chatMode != "" {
		return r.chatMode
	}
	if r.autoChat {
		return "auto"
	}
	return "off"
}

func (r *HHAIResponder) chatSendingAllowed() bool {
	return r.autoChat && r.effectiveChatMode() == "auto"
}

// buildReadableTestSolutions converts test tasks and AI answers to human-readable question/answer pairs
func buildReadableTestSolutions(tasks []Task, answers map[int]SolutionFields) []QAPair {
	var result []QAPair
	for _, task := range tasks {
		ans, ok := answers[task.ID]
		if !ok {
			continue
		}

		var answerText string
		if ans.HasChoice {
			for _, sol := range task.CandidateSolutions {
				if id, err := strconv.Atoi(sol.ID); err == nil && id == ans.SolutionID {
					answerText = sol.Text
					break
				}
			}
		} else {
			answerText = ans.TextSolution
		}

		result = append(result, QAPair{
			Question: task.Description,
			Answer:   answerText,
		})
	}
	return result
}

type HHResponse struct {
	RetryAfter     string
	Status         int
	URL            *url.URL
	ContentType    string
	Body           []byte
	CorrelationIDs map[string]string
}

type HHAIResponder struct {
	ctx                         context.Context
	baseURL                     *url.URL
	searchParams                url.Values
	searchProfiles              []vacancySearchProfile
	cookiesPath                 string
	maxResponses                int
	client                      *http.Client
	jar                         *MemoryPersistentJar
	requester                   *HHRequester
	resumeHash                  string
	resumeExperience            string
	resumeFacts                 ResumeFacts
	candidateProfile            CandidateProfile
	candidateProfilePath        string
	candidateStoriesPath        string
	candidateStories            []CandidateStory
	candidateRepository         CandidateRepository
	candidateMutations          *CandidateMutationService
	semanticRetriever           CandidateSemanticRetriever
	candidateClose              func()
	careerRepositories          CareerRepositories
	careerClose                 func()
	applicationAttempts         attemptport.Store
	attemptStoreInitErr         error
	autoChatAttempts            autochatattemptport.Store
	autoChatAttemptStoreInitErr error
	notifications               *NotificationStore
	reliabilityNotifications    reliabilitynotifications.Sink
	latestResumeHash            string
	resumes                     []ResumeItem
	userId                      int64
	firstName                   string
	middleName                  string
	lastName                    string
	email                       string
	ai                          *AIClient
	extraLetterPrompt           string
	extraTestSolutionPrompt     string
	contacts                    string
	outputPath                  string
	forceLetter                 bool
	extraChatReplyPrompt        string
	githubURL                   string
	dryRun                      bool
	hhWriteEnabled              bool
	autoApply                   bool
	autoChat                    bool
	autoTouch                   bool
	autoJobStatus               bool
	chatMode                    string
	minSalary                   int
	minSalaryCurrency           string
	includeKeywords             []string
	excludeKeywords             []string
	minMatchScore               int
	runOnce                     bool
	maxVacanciesPerRun          int
	maxApplicationsPerRun       int
	alreadyRespondedStatePath   string
	alreadyResponded            map[int]struct{}
	chatURL                     string
	resumeProfileFrontURL       string
	ignoredChats                []int64
	ignoredChatTriggers         map[string]struct{}
	preflightCache              map[int]VacancyPreflight
	readClient                  *HHAIResponderReadClient

	eventWriter        io.Writer
	eventMu            sync.Mutex
	preflightMu        sync.Mutex
	alreadyRespondedMu sync.Mutex
	ignoredChatsMu     sync.Mutex
}

type vacancySearchProfile struct {
	Name    string
	URL     string
	BaseURL *url.URL
	Params  url.Values
}

type HHRequester struct {
	readOnly        bool
	ctx             context.Context
	client          *http.Client
	interval        time.Duration
	mu              sync.Mutex
	lastStart       time.Time
	readNotBefore   time.Time
	readConcurrency int
	readQueue       []*hhReadWaiter
	readWake        chan struct{}
	readScheduler   bool
	readActive      int
}

type hhReadWaiter struct {
	priority hhReadPriority
	granted  chan struct{}
	ctx      context.Context
}

func NewHHRequester(ctx context.Context, client *http.Client, interval time.Duration) *HHRequester {
	return &HHRequester{
		ctx:      ctx,
		client:   client,
		interval: interval,
	}
}

func cookieNames(req *http.Request) string {
	names := make([]string, 0, len(req.Cookies()))
	for _, c := range req.Cookies() {
		names = append(names, c.Name)
	}
	return strings.Join(names, ",")
}

func (r *HHRequester) doOnce(req *http.Request) (*HHResponse, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	if req.Method == http.MethodGet || req.Method == http.MethodHead {
		if err := r.acquireRead(req.Context()); err != nil {
			return nil, err
		}
		defer r.releaseRead()
	}

	if r.readOnly && req.Method != http.MethodGet && req.Method != http.MethodHead {
		return nil, errors.New("HH read-only client blocks state-changing methods")
	}
	waitStart := time.Now()
	perfRecord("hh.rate_wait", waitStart, 1)
	meterRecord(req.Context(), "wait", time.Since(waitStart))
	start := time.Now()
	defer perfRecord("hh.http", start, 1)
	defer func() { meterRecord(req.Context(), "network", time.Since(start)) }()

	// Execute request
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if logger != nil {
		logger.Debug("REQ  %s %s cookies=[%s]", req.Method, req.URL.String(), cookieNames(req))
		logger.Debug("RESP %d %s final_url=%s cookies=[%s]", resp.StatusCode, req.Method, resp.Request.URL.String(), cookieNames(resp.Request))
	}
	return &HHResponse{
		Status:         resp.StatusCode,
		URL:            req.URL,
		ContentType:    resp.Header.Get("Content-Type"),
		Body:           body,
		CorrelationIDs: safeHHCorrelationIDs(resp.Header),
		RetryAfter:     resp.Header.Get("Retry-After"),
	}, nil
}

func (r *HHRequester) acquireRead(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	waiter := &hhReadWaiter{priority: hhReadPriorityFromContext(ctx), granted: make(chan struct{}), ctx: ctx}
	r.mu.Lock()
	if r.readWake == nil {
		r.readWake = make(chan struct{}, 1)
	}
	r.readQueue = append(r.readQueue, waiter)
	if !r.readScheduler {
		r.readScheduler = true
		go r.runReadScheduler()
	}
	r.signalReadSchedulerLocked()
	r.mu.Unlock()
	select {
	case <-waiter.granted:
		return nil
	case <-ctx.Done():
		r.signalReadScheduler()
		return ctx.Err()
	case <-r.ctx.Done():
		r.signalReadScheduler()
		return r.ctx.Err()
	}
}

func (r *HHRequester) releaseRead() {
	r.mu.Lock()
	if r.readActive > 0 {
		r.readActive--
	}
	r.signalReadSchedulerLocked()
	r.mu.Unlock()
}

func (r *HHRequester) signalReadScheduler() {
	r.mu.Lock()
	r.signalReadSchedulerLocked()
	r.mu.Unlock()
}

func (r *HHRequester) signalReadSchedulerLocked() {
	if r.readWake == nil {
		return
	}
	select {
	case r.readWake <- struct{}{}:
	default:
	}
}

func (r *HHRequester) runReadScheduler() {
	for {
		r.mu.Lock()
		for i := len(r.readQueue) - 1; i >= 0; i-- {
			select {
			case <-r.readQueue[i].ctx.Done():
				r.readQueue = append(r.readQueue[:i], r.readQueue[i+1:]...)
			default:
			}
		}
		limit := r.readConcurrency
		if limit < 1 {
			limit = 4
		}
		if limit > 8 {
			limit = 8
		}
		if r.readActive >= limit || len(r.readQueue) == 0 {
			wake := r.readWake
			r.mu.Unlock()
			select {
			case <-wake:
			case <-r.ctx.Done():
				return
			}
			continue
		}
		deadline := r.lastStart.Add(r.interval)
		if r.readNotBefore.After(deadline) {
			deadline = r.readNotBefore
		}
		if wait := time.Until(deadline); wait > 0 {
			wake := r.readWake
			r.mu.Unlock()
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-wake:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			case <-r.ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return
			}
			continue
		}
		best := 0
		for i := 1; i < len(r.readQueue); i++ {
			if r.readQueue[i].priority > r.readQueue[best].priority {
				best = i
			}
		}
		waiter := r.readQueue[best]
		r.readQueue = append(r.readQueue[:best], r.readQueue[best+1:]...)
		r.readActive++
		r.lastStart = time.Now()
		close(waiter.granted)
		r.mu.Unlock()
	}
}

func safeHHCorrelationIDs(headers http.Header) map[string]string {
	result := map[string]string{}
	for _, name := range []string{"X-Request-ID", "X-Correlation-ID", "Trace-ID", "X-Amzn-Trace-Id"} {
		value := strings.TrimSpace(headers.Get(name))
		if value == "" || strings.ContainsAny(value, "\r\n") || sensitiveResponseField.MatchString(value) {
			continue
		}
		if len(value) > 256 {
			value = value[:256]
		}
		result[name] = value
	}
	return result
}

type AIClient struct {
	ctx      context.Context
	baseURL  string
	model    string
	apiKey   string
	attempts int
	client   *http.Client
	provider llmport.CompletionProvider
}

type AccountInfo struct {
	FirstName  string `json:"firstName"`
	MiddleName string `json:"middleName"`
	LastName   string `json:"lastName"`
	Email      string `json:"email"`
}

type ResumeTitle struct {
	String string `json:"string"`
}

type Logger struct {
	base  *log.Logger
	level LogLevel
	color bool
}

func NewLogger(output io.Writer, level LogLevel) *Logger {
	useColor := false
	if f, ok := output.(*os.File); ok {
		if fi, err := f.Stat(); err == nil {
			useColor = (fi.Mode() & os.ModeCharDevice) != 0
		}
	}
	return &Logger{
		base:  log.New(output, "", log.LstdFlags),
		level: level,
		color: useColor,
	}
}

func (l *Logger) write(level, color, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if l.color {
		l.base.Printf("%s[%s]\x1b[0m %s", color, level, msg)
		return
	}
	l.base.Printf("[%s] %s", level, msg)
}

func (l *Logger) Debug(format string, args ...any) {
	if l.level <= LevelDebug {
		l.write("DEBUG", "\x1b[34;20m", format, args...)
	}
}

func (l *Logger) Info(format string, args ...any) {
	if l.level <= LevelInfo {
		l.write("INFO", "\x1b[32;20m", format, args...)
	}
}

func (l *Logger) Warn(format string, args ...any) {
	if l.level <= LevelWarn {
		l.write("WARNING", "\x1b[33;20m", format, args...)
	}
}

func (l *Logger) Error(format string, args ...any) {
	if l.level <= LevelError {
		l.write("ERROR", "\x1b[31;20m", format, args...)
	}
}

func (r *HHAIResponder) getBaseHost() string {
	for domain, list := range r.jar.cookies {
		if domain == ".hh.ru" || strings.HasSuffix(domain, ".hh.ru") {
			for _, c := range list {
				if c.Name == "redirect_host" && c.Value != "" {
					return c.Value
				}
			}
		}
	}

	return defaultHost
}

func NewHHAIResponder(ctx context.Context, cfg Config) (*HHAIResponder, error) {
	if logger == nil {
		// Read-only dashboard construction can happen outside the CLI entrypoint.
		// Keep constructor logging safe without changing an already configured
		// application logger.
		logger = NewLogger(io.Discard, LevelError)
	}
	backend, err := normalizeStorageBackend(cfg.StorageBackend)
	if err != nil {
		return nil, err
	}
	var candidatePersistence CandidatePersistence
	var closeCandidate func()
	var careerRepositories CareerRepositories
	var closeCareer func()
	resourcesReady := false
	defer func() {
		if resourcesReady {
			return
		}
		if closeCareer != nil {
			closeCareer()
		}
		if closeCandidate != nil {
			closeCandidate()
		}
	}()
	if backend == storageBackendPostgres {
		if strings.TrimSpace(cfg.CandidateProfilePath) == "" {
			cfg.CandidateProfilePath = "candidate_profile.json"
		}
		candidatePersistence, closeCandidate, err = BuildCandidatePersistence(ctx, cfg)
		if err != nil {
			return nil, err
		}
		careerRepositories, closeCareer, err = BuildCareerRepositories(ctx, cfg, nil, nil, nil)
		if err != nil {
			return nil, err
		}
	}
	var baseURL *url.URL
	var searchParams url.Values
	searchURLs := append([]string(nil), cfg.SearchURLs...)
	if len(searchURLs) == 0 && strings.TrimSpace(cfg.SearchURL) != "" {
		searchURLs = []string{cfg.SearchURL}
	}
	searchProfiles, parsedBaseURL, err := buildVacancySearchProfiles(searchURLs)
	if err != nil {
		return nil, err
	}
	baseURL = parsedBaseURL
	if len(searchProfiles) > 0 {
		searchParams = cloneValues(searchProfiles[0].Params)
	}
	jar, err := NewMemoryPersistentJar(cfg.CookiesPath)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
	}

	responder := &HHAIResponder{
		ctx:                       ctx,
		baseURL:                   baseURL,
		cookiesPath:               cfg.CookiesPath,
		maxResponses:              cfg.MaxResponses,
		client:                    client,
		jar:                       jar,
		resumeHash:                cfg.Resume,
		ai:                        NewAIClient(ctx, cfg.AIBaseURL, cfg.AIModel, cfg.AIAPIKey, cfg.AITimeout, cfg.AIConnectTimeout, cfg.AIAttempts),
		extraLetterPrompt:         cfg.ExtraLetterPrompt,
		extraTestSolutionPrompt:   cfg.ExtraTestSolutionPrompt,
		contacts:                  cfg.Contacts,
		outputPath:                cfg.OutputPath,
		forceLetter:               cfg.ForceLetter,
		extraChatReplyPrompt:      cfg.ExtraChatReplyPrompt,
		githubURL:                 cfg.GithubURL,
		dryRun:                    cfg.DryRun,
		autoApply:                 cfg.AutoApply,
		autoChat:                  cfg.AutoChat,
		autoTouch:                 cfg.AutoTouch,
		autoJobStatus:             cfg.AutoJobStatus,
		chatMode:                  cfg.ChatMode,
		hhWriteEnabled:            cfg.HHWriteEnabled,
		minSalary:                 cfg.MinSalary,
		minSalaryCurrency:         cfg.MinSalaryCurrency,
		includeKeywords:           append([]string(nil), cfg.IncludeKeywords...),
		excludeKeywords:           append([]string(nil), cfg.ExcludeKeywords...),
		minMatchScore:             cfg.MinMatchScore,
		runOnce:                   cfg.RunOnce,
		maxVacanciesPerRun:        cfg.MaxVacanciesPerRun,
		maxApplicationsPerRun:     cfg.MaxApplicationsPerRun,
		alreadyRespondedStatePath: cfg.AlreadyRespondedStatePath,
		candidateProfilePath:      cfg.CandidateProfilePath,
		candidateStoriesPath:      cfg.CandidateStoriesPath,
		candidateClose:            closeCandidate,
		careerRepositories:        careerRepositories,
		careerClose:               closeCareer,
	}
	if backend == storageBackendPostgres {
		responder.candidateRepository = candidatePersistence.Repository
		responder.candidateMutations = candidatePersistence.Mutations
		responder.semanticRetriever = candidatePersistence.SemanticRetriever
	}
	applicationAttempts, attemptStoreErr := buildAutomaticApplicationAttemptStore(ctx, cfg, backend, candidatePersistence.Pool)
	responder.applicationAttempts = applicationAttempts
	responder.attemptStoreInitErr = attemptStoreErr
	autoChatAttempts, autoChatAttemptStoreErr := buildAutoChatAttemptStore(ctx, cfg, backend, candidatePersistence.Pool)
	responder.autoChatAttempts = autoChatAttempts
	responder.autoChatAttemptStoreInitErr = autoChatAttemptStoreErr
	notificationPath, pathErr := os.Getwd()
	if pathErr != nil {
		return nil, pathErr
	}
	responder.notifications = NewNotificationStore(filepath.Join(notificationPath, NotificationEventsFilename))
	if err := responder.notifications.Load(); err != nil {
		return nil, err
	}
	responder.reliabilityNotifications = reliabilitynotifications.NewProjector(responder.notifications)
	if backend == storageBackendJSON && responder.candidateProfilePath != "" {
		responder.candidateProfile, err = LoadCandidateProfile(responder.candidateProfilePath)
		if err != nil {
			return nil, err
		}
	}
	if backend == storageBackendJSON && responder.candidateStoriesPath != "" {
		stories, err := LoadCandidateStories(responder.candidateStoriesPath)
		if err != nil {
			return nil, err
		}
		responder.candidateStories = stories.Stories
	}

	responder.requester = NewHHRequester(ctx, client, cfg.RequestInterval)
	responder.requester.readOnly = cfg.HHReadOnly
	responder.requester.readConcurrency = cfg.HHReadConcurrency

	// initialize event writer once
	var out io.Writer = os.Stdout
	if cfg.OutputPath != "" {
		f, err := os.OpenFile(cfg.OutputPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, err
		}
		out = f
	}

	responder.eventWriter = out
	responder.searchParams = searchParams
	responder.searchProfiles = searchProfiles

	// If baseURL not provided via -u, resolve from redirect_host cookie for .hh.ru
	if responder.baseURL == nil {
		host := responder.getBaseHost()
		responder.baseURL = &url.URL{Scheme: "https", Host: host}
	}
	logger.Debug("baseURL resolved to %s", responder.baseURL.String())

	if err := responder.LoadProfileData(); err != nil {
		return nil, err
	}

	logger.Debug("HH profile loaded")

	if responder.resumeHash == "" {
		responder.resumeHash = responder.latestResumeHash
	}

	resume := responder.GetCurrentResume()

	if resume == nil {
		return nil, errors.New("resume not found")
	}

	logger.Debug("Current resume loaded (title_characters=%d)", len(resume.Title))

	resumeFacts, err := responder.GetResumeFacts()
	if err != nil {
		return nil, errors.New("can't load resume experience")
	}
	responder.resumeFacts = resumeFacts
	responder.resumeExperience = resumeFacts.ExperienceText
	if backend == storageBackendJSON && responder.candidateProfilePath != "" {
		if fullName := strings.TrimSpace(responder.GetFullName()); fullName != "" && sourcePriority(responder.candidateProfile.Identity.FullName.Source) <= sourcePriority(CandidateSourceHHResume) {
			responder.candidateProfile.Identity.FullName = ProfileStringFact{Value: fullName, ProfileFact: ProfileFact{Source: CandidateSourceHHResume, Confirmed: true, ConfirmedAt: time.Now(), Evidence: []string{"HH account profile"}}}
		}
		responder.candidateProfile.MergeHHResumeFacts(*resume, resumeFacts, time.Now())
		if err := SaveCandidateProfile(responder.candidateProfilePath, responder.candidateProfile); err != nil {
			return nil, fmt.Errorf("save merged candidate profile: %w", err)
		}
	}

	// If no search URL was provided, retain the old resume-only search behavior.
	if len(responder.searchProfiles) == 0 {
		responder.searchParams = make(url.Values)
		responder.searchParams.Set("resume", responder.resumeHash)
		responder.searchProfiles = []vacancySearchProfile{{
			Name:    "Default search",
			BaseURL: responder.baseURL,
			Params:  cloneValues(responder.searchParams),
			URL:     searchProfileURL(responder.baseURL, responder.searchParams),
		}}
	}
	resourcesReady = true

	return responder, nil
}

func buildVacancySearchProfiles(searchURLs []string) ([]vacancySearchProfile, *url.URL, error) {
	profiles := make([]vacancySearchProfile, 0, len(searchURLs))
	var firstBaseURL *url.URL

	for index, rawURL := range searchURLs {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			continue
		}
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid search URL: %s: %w", rawURL, err)
		}
		if parsed.Scheme == "" || parsed.Host == "" {
			return nil, nil, fmt.Errorf("invalid search URL: %s", rawURL)
		}

		base := &url.URL{Scheme: parsed.Scheme, Host: parsed.Host}
		if firstBaseURL == nil {
			firstBaseURL = base
		}
		params := parsed.Query()
		params.Del("page")
		params.Set("order_by", "publication_time")
		params.Set("search_period", "7")
		params.Set("items_on_page", "50")

		profiles = append(profiles, vacancySearchProfile{
			Name:    vacancySearchProfileName(index),
			URL:     searchProfileURL(base, params),
			BaseURL: base,
			Params:  params,
		})
	}

	return profiles, firstBaseURL, nil
}

func vacancySearchProfileName(index int) string {
	knownNames := []string{
		"Python / Django / Backend",
		"Automation / Integrations / Implementation",
		"Support / Product Support",
	}
	if index >= 0 && index < len(knownNames) {
		return knownNames[index]
	}
	return fmt.Sprintf("Search profile %d", index+1)
}

func searchProfileURL(baseURL *url.URL, params url.Values) string {
	if baseURL == nil {
		return ""
	}
	profileURL := *baseURL
	profileURL.Path = "/search/vacancy"
	profileURL.RawPath = ""
	profileURL.RawQuery = params.Encode()
	return profileURL.String()
}

func configuredSearchURLs(multiple, fallback string) ([]string, error) {
	return appconfig.ConfiguredSearchURLs(multiple, fallback)
}

func (r *HHAIResponder) writeEvent(v any) {
	if r.eventWriter == nil {
		return
	}
	r.eventMu.Lock()
	defer r.eventMu.Unlock()
	_ = json.NewEncoder(r.eventWriter).Encode(v)
}

func (r *HHAIResponder) ResolveURL(endpoint string) string {
	ref, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	return r.baseURL.ResolveReference(ref).String()
}

// buildRequest creates an HTTP request with standard headers
func (r *HHAIResponder) buildRequest(method, endpoint string, body io.Reader, headers map[string]string) (*http.Request, error) {
	return r.buildRequestAtBase(r.baseURL, method, endpoint, body, headers)
}

func (r *HHAIResponder) buildRequestAtBase(baseURL *url.URL, method, endpoint string, body io.Reader, headers map[string]string) (*http.Request, error) {
	if baseURL == nil {
		return nil, errors.New("base URL is not configured")
	}
	ref, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse request endpoint: %w", err)
	}
	req, err := http.NewRequestWithContext(r.ctx, method, baseURL.ResolveReference(ref).String(), body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Standard headers
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", acceptLanguageHeader)
	req.Header.Set("Accept", acceptHeader)
	req.Header.Set("Sec-CH-UA", secCHUAHeader)
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", `"Windows"`)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")

	// Additional headers
	for key, value := range headers {
		if value != "" {
			req.Header.Set(key, value)
		}
	}

	return req, nil
}

// func (r *HHAIResponder) GetCurrentResumeTitle() string {
// 	for _, resume := range r.resumes {
// 		if resume.Hash == r.resumeHash {
// 			return resume.Title
// 		}
// 	}
// 	return ""
// }
//
// func (r *HHAIResponder) GetCurrentResumeId() int64 {
// 	for _, resume := range r.resumes {
// 		if resume.Hash == r.resumeHash {
// 			return resume.Id
// 		}
// 	}
// 	return -1
// }

func (r *HHAIResponder) GetCurrentResume() *ResumeItem {
	for _, res := range r.resumes {
		if res.Hash == r.resumeHash {
			return &res
		}
	}
	return nil
}

func (r *HHAIResponder) GetFullName() string {
	return fmt.Sprintf("%s %s", r.firstName, r.lastName)
}

func (r *HHAIResponder) XSRFToken() string {
	if r == nil || r.jar == nil || r.baseURL == nil {
		return ""
	}
	for _, cookie := range r.jar.Cookies(r.baseURL) {
		if cookie.Name == "_xsrf" {
			return cookie.Value
		}
	}
	return ""
}

func NewAIClient(ctx context.Context, baseURL, model, apiKey string, timeout, connectTimeout time.Duration, attempts int) *AIClient {
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}

	// connectTimeout ограничивает только установку соединения, timeout весь запрос целиком
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = connectTimeout
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
	debug := func(message string) {
		if logger != nil {
			logger.Debug("%s", message)
		}
	}
	warn := func(message string) {
		if logger != nil {
			logger.Warn("%s", message)
		}
	}

	return &AIClient{
		ctx:      ctx,
		baseURL:  strings.TrimRight(baseURL, "/"),
		model:    model,
		apiKey:   apiKey,
		attempts: attempts,
		client:   client,
		provider: openaillm.New(openaillm.Options{
			BaseURL:    baseURL,
			APIKey:     apiKey,
			Model:      model,
			Attempts:   attempts,
			HTTPClient: client,
			RetryDelay: aiRetryDelay,
			Debug:      debug,
			Warn:       warn,
		}),
	}
}

func (c *AIClient) Chat(systemPrompt, userPrompt string, maxTokens int, temperature float64) (string, error) {
	return c.legacyCompletion(systemPrompt, userPrompt, maxTokens, temperature, nil, nil)
}

// ChatStructured remains only for source compatibility. Runtime workflows use
// their typed usecase, which owns structured parsing and semantic retries.
func (c *AIClient) ChatStructured(systemPrompt, userPrompt string, maxTokens int, temperature float64, validator func(string) error) (string, error) {
	return c.legacyCompletion(systemPrompt, userPrompt, maxTokens, temperature, &ChatResponseFormat{Type: "json_object"}, validator)
}

// ChatStructuredWithSchema is a one-call compatibility facade. It translates
// the legacy request shape and performs at most the caller-supplied single
// validation callback; it never retries or owns business parsing/policy.
func (c *AIClient) ChatStructuredWithSchema(systemPrompt, userPrompt string, maxTokens int, temperature float64, schema *ChatJSONSchema, validator func(string) error) (string, error) {
	format := &ChatResponseFormat{Type: "json_object", JSONSchema: schema}
	if schema != nil {
		format.Type = "json_schema"
	}
	return c.legacyCompletion(systemPrompt, userPrompt, maxTokens, temperature, format, validator)
}

func (r *HHAIResponder) candidateContext(resume ResumeItem) LegacyCandidateContext {
	legacy, _, err := r.canonicalCandidateContext(resume)
	if err == nil {
		return legacy
	}
	// Compatibility callers cannot return an error. A canonical conflict
	// therefore produces an intentionally empty factual context.
	return LegacyCandidateContext{ResumeTitle: resume.Title, Salary: resume.Salary, Location: resume.Area, Contacts: r.contacts}
}

func (r *HHAIResponder) canonicalCandidateContext(resume ResumeItem) (LegacyCandidateContext, *CandidateContextResolver, error) {
	return r.canonicalCandidateContextContext(ctxOrBackground(r.ctx), resume)
}

func (r *HHAIResponder) canonicalCandidateContextContext(ctx context.Context, resume ResumeItem) (LegacyCandidateContext, *CandidateContextResolver, error) {
	if r != nil && r.candidateRepository != nil {
		candidate, err := r.candidateRepository.CurrentCandidate(ctxOrBackground(ctx))
		if err != nil {
			return LegacyCandidateContext{}, nil, err
		}
		resolver := NewCandidateContextResolverFromCandidate(candidate)
		view, err := CanonicalEmployerSafeProjection(candidate)
		if err != nil {
			return LegacyCandidateContext{}, nil, err
		}
		fullName := candidate.Identity.FullName
		if strings.TrimSpace(fullName) == "" {
			fullName = r.GetFullName()
		}
		location := candidate.Identity.Location
		if strings.TrimSpace(location) == "" {
			location = resume.Area
		}
		totalExperience, totalKnown, err := resolver.canonicalTotalExperience()
		if err != nil {
			return LegacyCandidateContext{}, nil, err
		}
		legacy := LegacyCandidateContext{
			FullName: fullName, ResumeTitle: resume.Title, Salary: resume.Salary,
			Experience: joinCanonicalExperience(view), Skills: joinCanonicalSkills(view),
			Location: location, Contacts: r.contacts, Profile: canonicalProfileForLegacy(candidate), Stories: canonicalStoriesForLegacy(candidate),
			TotalExperienceMonthsKnown: totalKnown, TotalExperienceMonths: totalExperience.Value, SafeKnowledge: view,
		}
		for _, education := range view.Profile.Education {
			legacy.EducationKnown = true
			legacy.EducationLevel = education.Level
			legacy.EducationDetails = joinNonEmptyStrings(", ", education.Institution, education.Specialty, education.Details)
			break
		}
		for _, item := range view.Profile.WorkExperience {
			legacy.Experience = joinNonEmptyStrings("\n\n", legacy.Experience, item.Description)
		}
		for _, item := range view.Profile.Projects {
			legacy.Experience = joinNonEmptyStrings("\n\n", legacy.Experience, item.Description)
		}
		return legacy, resolver, nil
	}
	originalProfile := r.candidateProfile
	profile := originalProfile
	resumeFacts := r.resumeFacts
	if strings.TrimSpace(resumeFacts.ExperienceText) == "" && strings.TrimSpace(r.resumeExperience) != "" {
		// Compatibility input is folded into the canonical source before any
		// prompt-facing projection is produced.
		resumeFacts.ExperienceText = r.resumeExperience
	}
	readAt := profile.UpdatedAt
	if readAt.IsZero() {
		readAt = time.Unix(1, 0).UTC()
	}
	// Merge into a local copy only. Legacy profile persistence remains the
	// writer; this supplies the canonical runtime snapshot for reads.
	profile.MergeHHResumeFacts(resume, resumeFacts, readAt)
	candidateRepository := NewJSONCandidateRepositoryFromInput(CanonicalCandidateInput{
		Profile: profile, Knowledge: CandidateKnowledgeBase{Profile: profile},
		Stories: r.candidateStories, ResumeFacts: &resumeFacts,
		Contacts: r.contacts, GitHubURL: r.githubURL,
	})
	candidate, diagnostics, err := candidateRepository.CurrentCandidateWithDiagnostics(ctxOrBackground(ctx))
	if err != nil {
		return LegacyCandidateContext{}, nil, err
	}
	if len(diagnostics.Conflicts) > 0 {
		return LegacyCandidateContext{}, nil, errors.New("critical canonical candidate conflict")
	}
	safeKnowledge, _ := CanonicalEmployerSafeProjection(candidate)
	fullName := r.GetFullName()
	if profile.Identity.FullName.Confirmed && sourceTrustedForEmployerCommunication(profile.Identity.FullName.Source) && strings.TrimSpace(profile.Identity.FullName.Value) != "" {
		fullName = profile.Identity.FullName.Value
	}
	location := resume.Area
	if profile.Identity.Location.Confirmed && sourceTrustedForEmployerCommunication(profile.Identity.Location.Source) && strings.TrimSpace(profile.Identity.Location.Value) != "" {
		location = profile.Identity.Location.Value
	}
	educationKnown := false
	var educationLevel, educationDetails string
	for _, education := range candidate.Education {
		if !safeMetadata(education.Metadata) {
			continue
		}
		educationKnown = true
		educationLevel = education.Level
		educationDetails = joinNonEmptyStrings(", ", education.Institution, education.Specialty, education.Details)
		break
	}
	totalExperienceKnown := safeProfileFact(candidate.Profile.TotalExperienceMonths.ProfileFact)
	totalExperienceMonths := candidate.Profile.TotalExperienceMonths.Value
	if !totalExperienceKnown {
		totalExperienceMonths = 0
	}
	legacy := LegacyCandidateContext{
		FullName:                   fullName,
		ResumeTitle:                resume.Title,
		Salary:                     resume.Salary,
		Experience:                 joinNonEmptyStrings("\n\n", profile.TrustedExperienceText(), profile.TrustedProjectsText()),
		Skills:                     joinNonEmptyStrings(", ", profile.TrustedSkillsText()),
		Location:                   location,
		Contacts:                   r.contacts,
		EducationKnown:             educationKnown,
		EducationLevel:             educationLevel,
		EducationDetails:           educationDetails,
		TotalExperienceMonthsKnown: totalExperienceKnown,
		TotalExperienceMonths:      totalExperienceMonths,
		Profile:                    profile,
		Stories:                    append([]CandidateStory(nil), r.candidateStories...),
		SafeKnowledge:              safeKnowledge,
	}
	return legacy, NewCandidateContextResolverFromCandidate(candidate), nil
}

func (r *HHAIResponder) addPendingQuestionForRequirement(vacancy Vacancy, requirement HardRequirementEvaluation) {
	if requirement.Status != hardRequirementStatusUnknown || isOptionalRequirement(requirement) {
		return
	}
	topic := strings.TrimSpace(requirement.Requirement)
	if topic == "" {
		return
	}
	question := fmt.Sprintf("Работал ли ты с %s? Укажи только подтверждённый уровень и конкретный опыт.", topic)
	if requirement.Category == hardRequirementCategoryEducation {
		question = "Какой подтверждённый уровень образования и специальность подходят к требованию: " + topic + "?"
	} else if requirement.Category == hardRequirementCategoryLocation {
		question = "Подходят ли тебе условия/локация из требования: " + topic + "?"
	} else if requirement.Category == hardRequirementCategoryLanguage {
		question = "Какой у тебя подтверждённый уровень языка/языков для требования: " + topic + "?"
	}
	if r.candidateMutations != nil {
		if current, err := r.candidateRepository.CurrentCandidate(r.ctx); err == nil {
			for _, unknown := range current.Unknowns {
				if normalizeProfileName(unknown.Question) == normalizeProfileName(question) || normalizeProfileName(unknown.RelatedEntity) == normalizeProfileName(topic) && unknown.Status == CandidateUnknownNeedsConfirmation {
					return
				}
			}
			_, err := r.candidateMutations.AskUnknown(r.ctx, AskCandidateUnknownCommand{Actor: KnowledgeActorAI, Value: CandidateUnknown{Question: question, RelatedEntity: topic, Status: CandidateUnknownNeedsConfirmation}, Update: KnowledgeUpdate{Source: KnowledgeSourceRecord{Type: KnowledgeSourceUnknown, Evidence: []string{"vacancy hard requirement"}}, Reason: "required " + requirement.Category}})
			if err != nil && logger != nil {
				logger.Warn("Could not save candidate unknown for vacancy %d: %v", vacancy.ID, err)
			}
		}
		return
	}
	added := r.candidateProfile.AddPendingQuestion(PendingProfileQuestion{
		Topic: topic, Question: question, Category: requirement.Category, VacancyID: vacancy.ID, Reason: "required " + requirement.Category,
	})
	if !added {
		return
	}
	if r.candidateProfilePath == "" {
		return
	}
	if err := SaveCandidateProfile(r.candidateProfilePath, r.candidateProfile); err != nil {
		if logger != nil {
			logger.Warn("Could not save pending candidate profile question for vacancy %d: %v", vacancy.ID, err)
		}
		return
	}
	if logger != nil {
		logger.Info("Profile question added for unknown requirement %q", topic)
	}
}

func joinNonEmptyStrings(separator string, values ...string) string {
	var nonEmpty []string
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			nonEmpty = append(nonEmpty, value)
		}
	}
	return strings.Join(nonEmpty, separator)
}

func (r *HHAIResponder) LoadProfileData() error {
	if err := r.ctx.Err(); err != nil {
		return err
	}

	req, err := r.buildRequest(http.MethodGet, "/applicant/my_resumes", nil, nil)
	if err != nil {
		return err
	}

	resp, err := r.requester.Do(req)
	if err != nil {
		return err
	}

	if resp.Status != http.StatusOK {
		return unexpectedHTTPStatus(resp.Status)
	}

	bodyText := string(resp.Body)

	if strings.Contains(bodyText, "{&#34;") {
		bodyText = html.UnescapeString(bodyText)
	}

	target := `{"redirectConfig":`
	idx := strings.Index(bodyText, target)
	if idx == -1 {
		return errors.New("redirect config not found on page")
	}

	// jsonStart := bodyText[idx:]
	//logger.Debug("%.255s", jsonStart)

	var resumesData struct {
		LatestResumeHash string `json:"latestResumeHash"`
		ApplicantResumes []struct {
			Attributes struct {
				Id   string `json:"id"`
				Hash string `json:"hash"`
				User string `json:"user"`
			} `json:"_attributes"`
			Title []struct {
				String string `json:"string"`
			} `json:"title"`
			Salary []struct {
				Amount   int    `json:"amount"`
				Currency string `json:"currency"`
			} `json:"salary"`
			Area []struct {
				Title string `json:"title"`
			} `json:"area"`
			KeySkills []struct {
				String string `json:"string"`
			} `json:"keySkills"`
		} `json:"applicantResumes"`
		Account struct {
			FirstName  string `json:"firstName"`
			MiddleName string `json:"middleName"`
			LastName   string `json:"lastName"`
			Email      string `json:"email"`
		} `json:"account"`
		UserNotifications []struct {
			UserId int64 `json:"userId"`
		} `json:"userNotifications"`
		// Chatik struct {
		// 	ChatikOrigin string `json:"chatikOrigin"`
		// } `json:"chatik"`
		Config struct {
			StaticHost                 string `json:"staticHost"`
			ApiXhhHost                 string `json:"apiXhhHost"`
			HhcdnHost                  string `json:"hhcdnHost"`
			ImageResizingCdnHost       string `json:"imageResizingCdnHost"`
			DevBuildNotifyEnabled      bool   `json:"devBuildNotifyEnabled"`
			ExternalMicroFrontendHosts struct {
				ApplicantServicesFront string `json:"applicant-services-front"`
				EmployerReviewsFront   string `json:"employer-reviews-front"`
				Chatik                 string `json:"chatik"`
				SkillsFront            string `json:"skills-front"`
				SupportFront           string `json:"support-front"`
				ResumeProfileFront     string `json:"resume-profile-front"`
				BrandingFront          string `json:"branding-front"`
				WebcallFront           string `json:"webcall-front"`
				MentorsFront           string `json:"mentors-front"`
				CareerPlatformFront    string `json:"career-platform-front"`
			} `json:"externalMicroFrontendHosts"`
		} `json:"config"`
	}

	// if err := json.Unmarshal([]byte(jsonStart), &resumesData); err != nil {
	// 	return fmt.Errorf("failed to parse resumes: %w", err)
	// }

	decoder := json.NewDecoder(strings.NewReader(bodyText[idx:]))
	if err := decoder.Decode(&resumesData); err != nil {
		return fmt.Errorf("failed to parse resumes: %w", err)
	}

	r.latestResumeHash = resumesData.LatestResumeHash
	r.firstName = resumesData.Account.FirstName
	r.middleName = resumesData.Account.MiddleName
	r.lastName = resumesData.Account.LastName
	r.email = resumesData.Account.Email
	// The applicant's user id is no longer populated in userNotifications on the
	// new /applicant/my_resumes page; it is available in each resume's _attributes.user.
	if len(resumesData.UserNotifications) > 0 {
		r.userId = resumesData.UserNotifications[0].UserId
	} else if len(resumesData.ApplicantResumes) > 0 {
		if id, err := strconv.ParseInt(resumesData.ApplicantResumes[0].Attributes.User, 10, 64); err == nil {
			r.userId = id
		}
	}
	r.chatURL = resumesData.Config.ExternalMicroFrontendHosts.Chatik
	r.resumeProfileFrontURL = resumesData.Config.ExternalMicroFrontendHosts.ResumeProfileFront

	r.resumes = make([]ResumeItem, 0, len(resumesData.ApplicantResumes))
	for _, resume := range resumesData.ApplicantResumes {
		id, _ := strconv.ParseInt(resume.Attributes.Id, 10, 64)

		var title string
		if len(resume.Title) > 0 {
			title = resume.Title[0].String
		}

		var area string
		if len(resume.Area) > 0 {
			area = resume.Area[0].Title
		}

		var skills []string
		for _, skill := range resume.KeySkills {
			skills = append(skills, skill.String)
		}

		var salaryAmount int
		var salaryCurrency string
		if len(resume.Salary) > 0 {
			salaryAmount = resume.Salary[0].Amount
			salaryCurrency = resume.Salary[0].Currency
		}

		r.resumes = append(r.resumes, ResumeItem{
			Id:     id,
			Hash:   resume.Attributes.Hash,
			Title:  title,
			Area:   area,
			Skills: strings.Join(skills, ", "),
			Salary: strings.Replace(fmt.Sprintf("%d %s", salaryAmount, salaryCurrency), "RUR", "руб", 1),
		})
	}

	return nil
}

func (r *HHAIResponder) SetActiveJobSearchStatus() (bool, error) {
	if err := r.ctx.Err(); err != nil {
		return false, err
	}
	if !r.autoJobStatus {
		logger.Info("Job-search status update disabled by configuration")
		return false, nil
	}
	if r.dryRun {
		logger.Info("DRY-RUN: would update job-search status")
		return true, nil
	}
	service, err := r.newLegacyWriteService()
	if err != nil {
		return false, err
	}
	result, err := service.SetJobSearchStatus(ctxOrBackground(r.ctx), hhwriteport.JobSearchStatusRequest{Status: "looking_for_offers"})
	if err != nil {
		return false, hhWriteErrorFromPort(err)
	}
	return result.ProviderStatus == http.StatusOK, nil
}

func (r *HHAIResponder) GetVacancyTests(responseURL string) (map[string]VacancyTest, error) {
	return r.getVacancyTestsContext(r.ctx, responseURL)
}

func (r *HHAIResponder) getVacancyTestsContext(ctx context.Context, responseURL string) (map[string]VacancyTest, error) {
	if ctx == nil {
		return nil, errors.New("HH test context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	req, err := r.buildRequest(http.MethodGet, responseURL, nil, nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.requester.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	if resp.Status != http.StatusOK {
		return nil, unexpectedHTTPStatus(resp.Status)
	}

	var tests map[string]VacancyTest
	if err := decodeEmbeddedJSON(resp.Body, `,"vacancyTests":`, &tests); err != nil {
		return nil, err
	}

	return tests, nil
}

func (r *HHAIResponder) SendResponse(payload url.Values, refererURL string) (map[string]any, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	if r.dryRun {
		return map[string]any{"dry_run": true}, nil
	}
	if !r.autoApply {
		return map[string]any{"disabled": true}, nil
	}
	vacancyID, err := strconv.Atoi(payload.Get("vacancy_id"))
	if err != nil || vacancyID <= 0 {
		return nil, errors.New("vacancy preflight requires a valid vacancy_id")
	}
	if err := r.requireLiveApplicationPreflight(vacancyID); err != nil {
		return nil, err
	}
	request, err := vacancyResponseRequestFromValues(payload, refererURL)
	if err != nil {
		return nil, err
	}
	service, err := r.newLegacyWriteService()
	if err != nil {
		return nil, err
	}
	result, err := service.SubmitVacancyResponse(ctxOrBackground(r.ctx), request)
	if err != nil {
		return nil, hhWriteErrorFromPort(err)
	}
	return map[string]any{"status": result.ProviderStatus, "metadata": result.Metadata}, nil
}

func (r *HHAIResponder) ApplyVacancy(vacancyID int, refererURL, letter string) (map[string]any, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	if !r.autoApply {
		return map[string]any{"disabled": true}, nil
	}
	if r.dryRun {
		return map[string]any{"dry_run": true}, nil
	}
	if err := r.requireLiveApplicationPreflight(vacancyID); err != nil {
		return nil, err
	}
	token := r.XSRFToken()
	if token == "" {
		return nil, errors.New("xsrf token not found")
	}

	payload := url.Values{
		"_xsrf":            {token},
		"vacancy_id":       {strconv.Itoa(vacancyID)},
		"resume_hash":      {r.resumeHash},
		"letter":           {letter},
		"ignore_postponed": {"true"},
	}

	return r.SendResponse(payload, refererURL)
}

func (r *HHAIResponder) GetResumeExperience() (string, error) {
	facts, err := r.GetResumeFacts()
	if err != nil {
		return "", err
	}
	return facts.ExperienceText, nil
}

func (r *HHAIResponder) GetResumeFacts() (ResumeFacts, error) {
	if err := r.ctx.Err(); err != nil {
		return ResumeFacts{}, err
	}

	req, err := r.buildRequest(http.MethodGet, fmt.Sprintf("/resume/%s", r.resumeHash), nil, nil)
	if err != nil {
		return ResumeFacts{}, err
	}

	resp, err := r.requester.Do(req)
	if err != nil {
		return ResumeFacts{}, err
	}

	if resp.Status != http.StatusOK {
		return ResumeFacts{}, unexpectedHTTPStatus(resp.Status)
	}
	return parseResumeFacts(resp.Body, time.Now())
}

func (r *HHAIResponder) GetVacancyDescription(vacancyId int) (string, error) {
	reader := r.hhReadClient()
	if reader == nil {
		return "", errors.New("HH responder is not configured")
	}
	adapter, err := reader.readAdapter()
	if err != nil {
		return "", err
	}
	return adapter.ReadVacancyDescription(r.ctx, vacancyId)
}
func (r *HHAIResponder) getVacancyDescriptionContext(ctx context.Context, vacancyId int) (string, error) {
	reader := r.hhReadClient()
	if reader == nil {
		return "", errors.New("HH responder is not configured")
	}
	adapter, err := reader.readAdapter()
	if err != nil {
		return "", err
	}
	return adapter.ReadVacancyDescription(ctx, vacancyId)
}

func (r *HHAIResponder) ApplyVacancyWithTest(vacancyId int, letter string) (map[string]any, []QAPair, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, nil, err
	}
	if !r.dryRun {
		if err := r.requireLiveApplicationPreflight(vacancyId); err != nil {
			return nil, nil, err
		}
	}

	responseURL := r.ResolveURL(fmt.Sprintf("/applicant/vacancy_response?vacancyId=%d&startedWithQuestion=false&hhtmFrom=vacancy", vacancyId))
	tests, err := r.GetVacancyTests(responseURL)
	if err != nil {
		return nil, nil, err
	}

	test, ok := tests[strconv.Itoa(vacancyId)]
	if !ok {
		return nil, nil, fmt.Errorf("vacancy marked with test but no test data found for vacancy %d", vacancyId)
	}

	if len(test.Tasks) == 0 {
		return nil, nil, fmt.Errorf("vacancy marked with test but no tasks returned for vacancy %d", vacancyId)
	}

	var token string
	if !r.dryRun {
		token = r.XSRFToken()
		if token == "" {
			return nil, nil, errors.New("xsrf token not found")
		}
	}

	payload := url.Values{
		"_xsrf":            {token},
		"uidPk":            {test.UIDPk},
		"guid":             {test.GUID},
		"startTime":        {test.StartTime},
		"testRequired":     {test.Required},
		"vacancy_id":       {strconv.Itoa(vacancyId)},
		"resume_hash":      {r.resumeHash},
		"ignore_postponed": {"true"},
		"incomplete":       {"false"},
		"lux":              {"true"},
		"withoutTest":      {"no"},
		"letter":           {letter},
	}
	payload.Set("mark_applicant_visible_in_vacancy_country", "false")
	payload.Set("country_ids", "[]")

	solutions, err := r.ai.SolveTests(test.Tasks, r.contacts, r.githubURL, r.extraTestSolutionPrompt)
	if err != nil {
		return nil, nil, fmt.Errorf("ai failed to answer test: %w", err)
	}

	if len(solutions) != len(test.Tasks) {
		return nil, nil, fmt.Errorf("incomplete test answers: got %d, expected %d", len(solutions), len(test.Tasks))
	}
	if err := r.ctx.Err(); err != nil {
		return nil, nil, err
	}

	// logger.Debug("AI answers: %v", answers)

	for _, task := range test.Tasks {
		taskID := task.ID
		fieldName := "task_" + strconv.Itoa(taskID)

		answer, ok := solutions[taskID]
		if !ok {
			return nil, nil, fmt.Errorf("ai returned no answer for task %d", taskID)
		}
		if answer.HasChoice {
			payload.Set(fieldName, strconv.Itoa(answer.SolutionID))
			continue
		}

		payload.Set(fieldName+"_text", answer.TextSolution)
	}

	// The test answer validation above proves that the AI response is complete
	// for the first snapshot. Re-read the provider metadata immediately before
	// the atomic vacancy response so changed tasks/options cannot reuse stale
	// answers or be remapped heuristically.
	freshTests, err := r.GetVacancyTests(responseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("fresh vacancy test metadata read failed: %w", err)
	}
	freshTest, ok := freshTests[strconv.Itoa(vacancyId)]
	if !ok {
		return nil, nil, fmt.Errorf("vacancy test metadata disappeared before response")
	}
	if result := hhwritepreflight.CompareTestMetadata(vacancyId, hhTestMetadata(test), hhTestMetadata(freshTest)); !result.Passed() {
		return nil, nil, fmt.Errorf("vacancy test metadata changed before response: %s", strings.Join(result.Reasons, "; "))
	}

	respJSON, err := r.SendResponse(payload, responseURL)
	if err != nil {
		return nil, nil, err
	}

	testSolutions := buildReadableTestSolutions(test.Tasks, solutions)
	return respJSON, testSolutions, nil
}

func (r *HHAIResponder) fetchVacancyPage(page int) ([]Vacancy, error) {
	return r.fetchVacancyPageWithSearch(r.searchParams, r.baseURL, page)
}

func (r *HHAIResponder) fetchVacancyPageForProfile(profile vacancySearchProfile, page int) ([]Vacancy, error) {
	return r.fetchVacancyPageWithSearch(profile.Params, profile.BaseURL, page)
}

func (r *HHAIResponder) fetchVacancyPageWithSearch(searchParams url.Values, baseURL *url.URL, page int) ([]Vacancy, error) {
	return r.fetchVacancyPageContext(r.ctx, searchParams, baseURL, page)
}
func (r *HHAIResponder) fetchVacancyPageContext(ctx context.Context, searchParams url.Values, baseURL *url.URL, page int) ([]Vacancy, error) {
	reader := r.hhReadClient()
	if reader == nil {
		return nil, errors.New("HH responder is not configured")
	}
	adapter, err := reader.readAdapter()
	if err != nil {
		return nil, err
	}
	// Search profiles historically carry their own base URL. Preserve that
	// compatibility case with the same typed read client and its configured
	// HTTP/rate-limit options.
	if baseURL != nil && (r.baseURL == nil || baseURL.String() != r.baseURL.String()) {
		interval, concurrency := time.Duration(0), 0
		if r.requester != nil {
			interval, concurrency = r.requester.interval, r.requester.readConcurrency
		}
		token := ""
		if r.jar != nil {
			token = r.XSRFToken()
		}
		httpClient := r.client
		if httpClient == nil && r.requester != nil {
			httpClient = r.requester.client
		}
		adapter, err = hhreadadapter.NewClient(hhreadadapter.Options{
			BaseURL: baseURL, SearchParams: searchParams, HTTPClient: httpClient,
			XSRFToken: token, UserID: r.userId,
			RequestInterval: interval, ReadConcurrency: concurrency,
		})
		if err != nil {
			return nil, err
		}
	}
	pageValue, err := adapter.ReadVacanciesWithSearch(ctx, searchParams, page)
	if err != nil {
		return nil, err
	}
	values := make([]Vacancy, 0, len(pageValue.Items))
	for _, record := range pageValue.Items {
		value, mapErr := mapHHVacancy(record)
		if mapErr != nil {
			return nil, mapErr
		}
		values = append(values, value)
	}
	return values, nil
}

func (r *HHAIResponder) fetchVacanciesFromSearchProfiles(summary *RunSummaryResult) ([]Vacancy, error) {
	profiles := r.searchProfiles
	if len(profiles) == 0 {
		profiles = []vacancySearchProfile{{
			Name:    "Default search",
			BaseURL: r.baseURL,
			Params:  r.searchParams,
			URL:     searchProfileURL(r.baseURL, r.searchParams),
		}}
	}

	uniqueVacancies := make([]Vacancy, 0)
	seenIDs := make(map[int]struct{})
	summary.SearchProfiles = make([]SearchProfileSummary, 0, len(profiles))
	for _, profile := range profiles {
		profileSummary := SearchProfileSummary{Name: profile.Name, URL: profile.URL}
		for page := 0; ; page++ {
			if err := r.ctx.Err(); err != nil {
				return nil, err
			}

			vacancies, err := r.fetchVacancyPageForProfile(profile, page)
			if err != nil {
				summary.SearchProfiles = append(summary.SearchProfiles, profileSummary)
				summary.VacanciesAfterDedup = len(uniqueVacancies)
				return nil, err
			}
			profileSummary.VacanciesFetched += len(vacancies)
			summary.VacanciesFetchedRaw += len(vacancies)
			summary.VacanciesFetched += len(vacancies)
			if len(vacancies) == 0 {
				break
			}

			for _, vacancy := range vacancies {
				if _, exists := seenIDs[vacancy.ID]; exists {
					summary.DuplicatesSkipped++
					continue
				}
				seenIDs[vacancy.ID] = struct{}{}
				uniqueVacancies = append(uniqueVacancies, vacancy)
			}
		}
		summary.SearchProfiles = append(summary.SearchProfiles, profileSummary)
	}
	summary.VacanciesAfterDedup = len(uniqueVacancies)
	return uniqueVacancies, nil
}

func (r *HHAIResponder) coverLetterSemanticExamples(vacancy Vacancy, description string, evaluation VacancyEvaluation, resolver *CandidateContextResolver) []SafeSemanticSelection {
	if r == nil || r.semanticRetriever == nil || resolver == nil {
		return []SafeSemanticSelection{}
	}
	candidate, diagnostics, err := resolver.canonicalCandidate()
	if err != nil || len(diagnostics.Conflicts) > 0 || strings.TrimSpace(candidate.ID) == "" {
		return []SafeSemanticSelection{}
	}
	query := buildCoverLetterSemanticQuery(vacancy.Name, description, MatchResult{MatchedSkills: append([]string{}, evaluation.StrongMatch...), MissingSkills: append([]string{}, evaluation.Missing...)})
	results, err := r.semanticRetriever.Retrieve(r.ctx, SemanticRetrievalRequest{CandidateID: candidate.ID, Query: query, EntityTypes: semanticEntityTypesForPurpose(SemanticRetrievalPurposeCoverLetter), Limit: semanticRetrievalTopK, Purpose: SemanticRetrievalPurposeCoverLetter})
	if err != nil {
		if logger != nil {
			logger.Warn("semantic retrieval skipped for automatic cover letter: %v", err)
		}
		return []SafeSemanticSelection{}
	}
	selected := BuildSafeSemanticContext(candidate, results)
	if logger != nil {
		logger.Debug("semantic retrieval used purpose=%s candidates=%d selected=%d ids=%s", SemanticRetrievalPurposeCoverLetter, len(results), len(selected), semanticSelectionIDs(selected))
	}
	return selected
}

func (r *HHAIResponder) SaveCookies() error {
	return r.jar.Save(r.cookiesPath)
}

// TouchResume raises (updates) resume position in search results
func (r *HHAIResponder) TouchResume() (bool, error) {
	if err := r.ctx.Err(); err != nil {
		return false, err
	}
	if !r.autoTouch {
		logger.Info("Resume touching disabled by configuration")
		return false, nil
	}
	if r.dryRun {
		logger.Info("DRY-RUN: would touch resume %s", r.resumeHash)
		resumeTitle := ""
		if resume := r.GetCurrentResume(); resume != nil {
			resumeTitle = resume.Title
		}
		r.writeEvent(ResumeTouchResult{
			Type:        "resume_touch_preview",
			Resume:      r.resumeHash,
			ResumeTitle: resumeTitle,
			Updated:     false,
			Time:        time.Now(),
		})
		return true, nil
	}
	service, err := r.newLegacyWriteService()
	if err != nil {
		return false, err
	}
	result, err := service.TouchResume(ctxOrBackground(r.ctx), hhwriteport.ResumeTouchRequest{ResumeHash: r.resumeHash})
	if err != nil {
		return false, hhWriteErrorFromPort(err)
	}
	return result.ProviderStatus == http.StatusOK, nil

}

type MemoryPersistentJar struct {
	mu          sync.Mutex
	cookies     map[string][]*http.Cookie
	persistPath string
}

func cookieEqual(a, b *http.Cookie) bool {
	return a.Name == b.Name &&
		a.Value == b.Value &&
		a.Path == b.Path &&
		a.Domain == b.Domain &&
		a.Secure == b.Secure &&
		a.Expires.Equal(b.Expires)
}

func NewMemoryPersistentJar(cookiesPath string) (*MemoryPersistentJar, error) {
	jar := &MemoryPersistentJar{
		cookies:     make(map[string][]*http.Cookie),
		persistPath: cookiesPath,
	}

	data, err := os.ReadFile(cookiesPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return jar, nil
		}
		return nil, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) < 7 {
			parts = strings.Fields(line)
		}
		if len(parts) < 7 {
			continue
		}

		domain := parts[0]
		expiresUnix, _ := strconv.ParseInt(parts[4], 10, 64)

		cookie := &http.Cookie{
			Domain: domain,
			Path:   parts[2],
			Secure: strings.EqualFold(parts[3], "TRUE"),
			Name:   parts[5],
			Value:  parts[6],
		}

		if expiresUnix > 0 {
			cookie.Expires = time.Unix(expiresUnix, 0)
		}

		jar.cookies[domain] = append(jar.cookies[domain], cookie)
	}

	if logger != nil {
		for domain, list := range jar.cookies {
			var names []string
			for _, c := range list {
				names = append(names, c.Name)
			}
			logger.Debug("jar loaded %d cookie(s) domain=%s: [%s]", len(list), domain, strings.Join(names, ","))
		}
	}

	return jar, scanner.Err()
}

func (j *MemoryPersistentJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()

	host := u.Hostname()
	changed := false

	for _, cookie := range cookies {
		domain := cookie.Domain
		if domain == "" {
			domain = host
		}

		var updated []*http.Cookie
		exists := false

		for _, c := range j.cookies[domain] {
			if c.Name == cookie.Name && c.Path == cookie.Path {
				exists = true

				if cookie.Expires.IsZero() && !c.Expires.IsZero() {
					cookie.Expires = c.Expires
				}

				if cookieEqual(c, cookie) {
					updated = append(updated, c)
				} else {
					updated = append(updated, cookie)
					changed = true
				}
			} else {
				updated = append(updated, c)
			}
		}

		if !exists {
			updated = append(updated, cookie)
			changed = true
		}

		j.cookies[domain] = updated
	}

	if changed && j.persistPath != "" {
		_ = j.saveLockedTo(j.persistPath)
	}
}

func (j *MemoryPersistentJar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()

	var matched []*http.Cookie
	host := u.Hostname()
	now := time.Now()
	changed := false

	for domain, list := range j.cookies {
		// Standard cookie domain semantics: a dot-prefixed domain like ".hh.ru"
		// matches the bare host "hh.ru" and any subdomain "*.hh.ru".
		domainNoDot := strings.TrimPrefix(domain, ".")
		if domain == host ||
			host == domainNoDot ||
			strings.HasSuffix(host, "."+domainNoDot) {

			var active []*http.Cookie

			for _, cookie := range list {
				if !cookie.Expires.IsZero() && cookie.Expires.Before(now) {
					changed = true
					continue
				}

				if cookie.Secure && u.Scheme != "https" {
					continue
				}

				copied := *cookie
				matched = append(matched, &copied)
				active = append(active, cookie)
			}

			if len(active) != len(list) {
				j.cookies[domain] = active
			}
		}
	}

	if changed && j.persistPath != "" {
		_ = j.saveLockedTo(j.persistPath)
	}

	return matched
}

func (j *MemoryPersistentJar) Save(path string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.saveLockedTo(path)
}

func (j *MemoryPersistentJar) saveLockedTo(path string) error {
	if path == "" {
		return nil
	}

	var buffer bytes.Buffer

	buffer.WriteString("# Netscape HTTP Cookie File\n")
	buffer.WriteString("# http://curl.haxx.se/rfc/cookie_spec.html\n")
	buffer.WriteString("# This is a generated file! Do not edit.\n\n")

	for domain, list := range j.cookies {
		for _, cookie := range list {
			if cookie.Name == "" {
				continue
			}

			expires := int64(0)
			if !cookie.Expires.IsZero() {
				expires = cookie.Expires.Unix()
			}

			secure := "FALSE"
			if cookie.Secure {
				secure = "TRUE"
			}

			cookiePath := cookie.Path
			if cookiePath == "" {
				cookiePath = "/"
			}

			row := []string{
				domain,
				"TRUE",
				cookiePath,
				secure,
				strconv.FormatInt(expires, 10),
				cookie.Name,
				cookie.Value,
			}

			buffer.WriteString(strings.Join(row, "\t"))
			buffer.WriteByte('\n')
		}
	}

	tmpPath := path + "~"

	if err := os.WriteFile(tmpPath, buffer.Bytes(), 0o600); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}

func decodeEmbeddedJSON[T any](data []byte, marker string, out *T) error {
	_, after, ok := bytes.Cut(data, []byte(marker))
	if !ok {
		return fmt.Errorf("marker %q not found in response", marker)
	}

	var raw json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(after))
	if err := decoder.Decode(&raw); err != nil {
		return err
	}

	return json.Unmarshal(raw, out)
}

func parseConfig() (Config, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		workingDir = "."
	}
	return parseConfigArgs(os.Args[1:], workingDir)
}

func parseConfigArgs(args []string, workingDir string) (Config, error) {
	loaded, err := appconfig.Load(args, os.LookupEnv, workingDir)
	if err != nil {
		return Config{}, err
	}
	return legacyConfigFromPackage(loaded), nil
}

func legacyConfigFromPackage(value appconfig.Config) Config {
	return Config{
		HHReadOnly:     value.HHReadOnly,
		StorageBackend: value.StorageBackend,
		DatabaseURL:    value.DatabaseURL,
		CandidateID:    value.CandidateID,
		FollowUpPolicy: FollowUpPolicy{
			AfterApplicationWithoutReply:      value.FollowUp.AfterApplicationWithoutReply,
			AfterCandidateMessageWithoutReply: value.FollowUp.AfterCandidateMessageWithoutReply,
			MaxFollowUps:                      value.FollowUp.MaxFollowUps,
			MinimumInterval:                   value.FollowUp.MinimumInterval,
		},
		SearchURL:                 value.SearchURL,
		SearchURLs:                value.SearchURLs,
		CookiesPath:               value.CookiesPath,
		LogLevel:                  value.LogLevel,
		Resume:                    value.Resume,
		MaxResponses:              value.MaxResponses,
		AIBaseURL:                 value.AIBaseURL,
		AIModel:                   value.AIModel,
		AIAPIKey:                  value.AIAPIKey,
		EmbeddingProvider:         value.EmbeddingProvider,
		EmbeddingBaseURL:          value.EmbeddingBaseURL,
		EmbeddingAPIKey:           value.EmbeddingAPIKey,
		EmbeddingModel:            value.EmbeddingModel,
		EmbeddingDimensions:       value.EmbeddingDimensions,
		AITimeout:                 value.AITimeout,
		AIConnectTimeout:          value.AIConnectTimeout,
		AIAttempts:                value.AIAttempts,
		ExtraLetterPrompt:         value.ExtraLetterPrompt,
		ExtraTestSolutionPrompt:   value.ExtraTestSolutionPrompt,
		HHReadConcurrency:         value.HHReadConcurrency,
		RequestInterval:           value.RequestInterval,
		OutputPath:                value.OutputPath,
		Contacts:                  value.Contacts,
		ListResumes:               value.ListResumes,
		ForceLetter:               value.ForceLetter,
		ExtraChatReplyPrompt:      value.ExtraChatReplyPrompt,
		GithubURL:                 value.GithubURL,
		DryRun:                    value.DryRun,
		HHWriteEnabled:            value.HHWriteEnabled,
		HHChatURL:                 value.HHChatURL,
		HHMaxWritesPerRun:         value.HHMaxWritesPerRun,
		HHMaxWritesPerDay:         value.HHMaxWritesPerDay,
		AutoApply:                 value.AutoApply,
		AutoChat:                  value.AutoChat,
		AutoTouch:                 value.AutoTouch,
		AutoJobStatus:             value.AutoJobStatus,
		ChatMode:                  value.ChatMode,
		MinSalary:                 value.MinSalary,
		MinSalaryCurrency:         value.MinSalaryCurrency,
		IncludeKeywords:           value.IncludeKeywords,
		ExcludeKeywords:           value.ExcludeKeywords,
		MinMatchScore:             value.MinMatchScore,
		RunOnce:                   value.RunOnce,
		MaxVacanciesPerRun:        value.MaxVacanciesPerRun,
		MaxApplicationsPerRun:     value.MaxApplicationsPerRun,
		MaxConversationsPerRun:    value.MaxConversationsPerRun,
		AlreadyRespondedStatePath: value.AlreadyRespondedStatePath,
		CandidateProfilePath:      value.CandidateProfilePath,
		CandidateStoriesPath:      value.CandidateStoriesPath,
		HHSyncStatePath:           value.HHSyncStatePath,
		MonitorInterval:           value.MonitorInterval,
		MonitorQuietHours:         value.MonitorQuietHours,
		NotificationCooldown:      value.NotificationCooldown,
		ConversationDisplayTTL:    value.ConversationDisplayTTL,
		BackgroundInboxRefresh:    value.BackgroundInboxRefresh,
	}
}

// DefaultFollowUpPolicy is the narrow composition conversion from primitive
// config values to the domain policy. Policy behavior remains in follow_up.go.
func DefaultFollowUpPolicy() FollowUpPolicy {
	defaults := appconfig.DefaultFollowUpConfig()
	return FollowUpPolicy{defaults.AfterApplicationWithoutReply, defaults.AfterCandidateMessageWithoutReply, defaults.MaxFollowUps, defaults.MinimumInterval}
}

func parseDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
	return appconfig.ParseDurationEnv(name, fallback)
}

func getEnv(name, fallback string) string {
	return appconfig.GetEnv(name, fallback)
}

func getEnvBool(name string, fallback bool) (bool, error) {
	return appconfig.GetEnvBool(name, fallback)
}

func loadDotEnv(path string) error {
	return appconfig.LoadDotEnv(path)
}

func parseLogLevel(level string) LogLevel {
	switch strings.ToLower(level) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

func (r *HHAIResponder) runOnceTasks() {
	if r.autoTouch {
		updated, err := r.TouchResume()
		if err != nil {
			logger.Error("Touch resume error: %v", err)
		} else if updated {
			logger.Info("%s: resume touch completed", runModePrefix(r.dryRun))
		}
	} else {
		logger.Info("Resume touching disabled by configuration")
	}

	if r.autoJobStatus {
		success, err := r.SetActiveJobSearchStatus()
		if err != nil {
			logger.Error("Job-search status error: %v", err)
		} else if success {
			logger.Info("%s: job-search status update completed", runModePrefix(r.dryRun))
		}
	} else {
		logger.Info("Job-search status updates disabled by configuration")
	}

	if r.autoApply {
		if err := r.ApplyVacancies(); err != nil {
			logger.Error("Apply error: %v", err)
		}
	} else {
		logger.Info("Automatic applications disabled by configuration")
	}

	if r.effectiveChatMode() != "off" {
		if err := r.AutoRespondChats(); err != nil {
			logger.Error("Auto chat error: %v", err)
		}
	} else {
		logger.Info("Chat processing disabled by configuration")
	}
}

func runModePrefix(dryRun bool) string {
	if dryRun {
		return "DRY-RUN"
	}
	return "Live mode"
}

func (r *HHAIResponder) Run() {
	logger.Info("Starting tasks...")
	if r.dryRun {
		logger.Info("DRY-RUN enabled: HH write requests are blocked; previews will be generated")
	}
	if r.runOnce {
		logger.Info("HH_RUN_ONCE enabled: executing each allowed task once")
		r.runOnceTasks()
		logger.Info("Run-once tasks finished")
		return
	}

	r.startRecurringTasks()

	// Block main until shutdown
	<-r.ctx.Done()
	logger.Info("Shutting down...")
}

func commandResult(err error, stderr io.Writer, code int) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	_, _ = fmt.Fprintln(stderr, err)
	return code
}

func runDefaultConfig(ctx context.Context, cfg Config, stdout, stderr io.Writer) int {
	logger = NewLogger(stderr, parseLogLevel(cfg.LogLevel))
	responder, err := NewHHAIResponder(ctx, cfg)
	if err != nil {
		logger.Error("%v", err)
		return 1
	}
	defer responder.closeResources()
	if cfg.ListResumes {
		for _, res := range responder.resumes {
			_, _ = fmt.Fprintf(stdout, "%s\t%s\n", res.Hash, res.Title)
		}
		return 0
	}
	responder.Run()
	return 0
}

func (r *HHAIResponder) closeResources() {
	if r == nil {
		return
	}
	if r.careerClose != nil {
		r.careerClose()
		r.careerClose = nil
	}
	if r.candidateClose != nil {
		r.candidateClose()
		r.candidateClose = nil
	}
}

func profileCLIUsesPostgres(args []string) bool {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv("STORAGE_BACKEND")))
	for i, arg := range args {
		if (arg == "-storage-backend" || arg == "--storage-backend") && i+1 < len(args) {
			backend = strings.ToLower(strings.TrimSpace(args[i+1]))
		}
	}
	return backend == storageBackendPostgres
}

func cloneValues(values url.Values) url.Values {
	result := make(url.Values, len(values))
	for key, list := range values {
		result[key] = append([]string(nil), list...)
	}
	return result
}

func unexpectedHTTPStatus(status int) error {
	return fmt.Errorf("unexpected HTTP status %d %s", status, http.StatusText(status))
}

func parseStrictJSON[T any](answer string, target *T) error {
	decoder := json.NewDecoder(strings.NewReader(answer))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("ai returned invalid JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("ai returned invalid JSON: trailing data")
		}
		return fmt.Errorf("ai returned invalid JSON: trailing data: %w", err)
	}
	return nil
}

// parseJSON is kept as a compatibility wrapper for callers outside the
// structured-output path. Structured AI responses must use parseStrictJSON.
func parseJSON[T any](answer string, target *T) error {
	return parseStrictJSON(answer, target)
}
