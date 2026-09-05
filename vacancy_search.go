package main

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
)

var vacancySalaryAmountRE = regexp.MustCompile(`[0-9](?:[[:space:]]*[0-9])*`)

// parseVacanciesFromSearchResponse keeps the old embedded JSON format as the
// first parser and falls back to HH's server-rendered search HTML.
func parseVacanciesFromSearchResponse(data []byte, baseURL *url.URL) ([]Vacancy, error) {
	var vacancies []Vacancy
	jsonErr := decodeEmbeddedJSON(data, `,"vacancies":`, &vacancies)
	if jsonErr == nil {
		return vacancies, nil
	}

	vacancies, htmlErr := parseVacanciesFromSearchHTML(data, baseURL)
	if htmlErr == nil {
		return vacancies, nil
	}

	return nil, fmt.Errorf("unable to parse HH vacancy search response: embedded JSON: %v; server-rendered HTML: %v", jsonErr, htmlErr)
}

func parseVacanciesFromSearchHTML(data []byte, baseURL *url.URL) ([]Vacancy, error) {
	if baseURL == nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, errors.New("base URL is required for HTML vacancy parsing")
	}

	document, err := xhtml.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}

	cards := findHTMLNodes(document, func(node *xhtml.Node) bool {
		return htmlAttr(node, "data-qa") == "vacancy-serp__vacancy"
	})
	if len(cards) == 0 {
		if isRecognizedEmptySearchHTML(document) {
			return []Vacancy{}, nil
		}
		return nil, fmt.Errorf("vacancy card container %q not found", "vacancy-serp__vacancy")
	}

	vacancies := make([]Vacancy, 0, len(cards))
	var firstCardErr error
	for _, card := range cards {
		vacancy, err := parseVacancyCardHTML(card, baseURL)
		if err != nil {
			if firstCardErr == nil {
				firstCardErr = err
			}
			continue
		}
		vacancies = append(vacancies, vacancy)
	}

	if len(vacancies) == 0 {
		if firstCardErr != nil {
			return nil, fmt.Errorf("no valid vacancy cards: %w", firstCardErr)
		}
		return nil, errors.New("no valid vacancy cards")
	}

	return vacancies, nil
}

func isRecognizedEmptySearchHTML(document *xhtml.Node) bool {
	// These are HH's search-result and explicit empty-result markers. A
	// document without one of them may be a login, protection, or error page,
	// and must not be interpreted as an empty search result.
	return findHTMLNode(document, func(node *xhtml.Node) bool {
		dataQA := htmlAttr(node, "data-qa")
		return dataQA == "vacancy-serp__results" || dataQA == "empty-vacancy-search-block"
	}) != nil
}

func parseVacancyCardHTML(card *xhtml.Node, baseURL *url.URL) (Vacancy, error) {
	titleNode := findHTMLNode(card, func(node *xhtml.Node) bool {
		return node.Type == xhtml.ElementNode && htmlAttr(node, "data-qa") == "serp-item__title"
	})
	if titleNode == nil {
		return Vacancy{}, errors.New("vacancy title link not found")
	}

	name := normalizeHTMLText(htmlNodeText(titleNode))
	if name == "" {
		return Vacancy{}, errors.New("vacancy title is empty")
	}

	vacancyURL, err := vacancyURLFromCardHTML(card, titleNode, baseURL)
	if err != nil {
		return Vacancy{}, err
	}

	vacancyID := vacancyIDFromCanonicalPath(vacancyURL.Path)
	if vacancyID <= 0 {
		return Vacancy{}, errors.New("vacancy ID not found in canonical URL")
	}

	vacancy := Vacancy{
		ID:   vacancyID,
		Name: name,
		Links: map[string]string{
			"desktop": vacancyURL.String(),
		},
		Compensation: parseVacancyCompensationHTML(card),
	}

	if companyNode := findHTMLNode(card, func(node *xhtml.Node) bool {
		return node.Type == xhtml.ElementNode && htmlAttr(node, "data-qa") == "vacancy-serp__vacancy-employer"
	}); companyNode != nil {
		vacancy.Company.Name = normalizeHTMLText(htmlNodeText(companyNode))
	}

	if areaNode := findHTMLNode(card, func(node *xhtml.Node) bool {
		return node.Type == xhtml.ElementNode && htmlAttr(node, "data-qa") == "vacancy-serp__vacancy-address"
	}); areaNode != nil {
		vacancy.Area.Name = normalizeHTMLText(htmlNodeText(areaNode))
	}

	if experienceNode := findHTMLNode(card, func(node *xhtml.Node) bool {
		qa := htmlAttr(node, "data-qa")
		return node.Type == xhtml.ElementNode && strings.HasPrefix(qa, "vacancy-serp__vacancy-work-experience-")
	}); experienceNode != nil {
		vacancy.WorkExperience = normalizeHTMLText(htmlNodeText(experienceNode))
	}

	if scheduleNode := findHTMLNode(card, func(node *xhtml.Node) bool {
		qa := htmlAttr(node, "data-qa")
		return node.Type == xhtml.ElementNode && strings.HasPrefix(qa, "vacancy-label-work-schedule-")
	}); scheduleNode != nil {
		vacancy.WorkSchedule = normalizeHTMLText(htmlNodeText(scheduleNode))
	}

	return vacancy, nil
}

func vacancyURLFromCardHTML(card, titleNode *xhtml.Node, baseURL *url.URL) (*url.URL, error) {
	if card == nil || titleNode == nil {
		return nil, errors.New("vacancy card or title link is missing")
	}
	if baseURL == nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, errors.New("base URL is required for vacancy URL parsing")
	}

	var canonicalLinks []*url.URL
	var queryIDs []int
	for _, node := range findHTMLNodes(card, func(node *xhtml.Node) bool {
		return node.Type == xhtml.ElementNode && node.Data == "a" && htmlAttr(node, "href") != ""
	}) {
		parsed, err := resolveVacancyURL(htmlAttr(node, "href"), baseURL)
		if err != nil {
			continue
		}
		if vacancyID := vacancyIDFromCanonicalPath(parsed.Path); vacancyID > 0 {
			canonicalLinks = append(canonicalLinks, parsed)
		}
		if vacancyID := vacancyIDFromURLQuery(parsed); vacancyID > 0 {
			queryIDs = append(queryIDs, vacancyID)
		}
	}

	canonicalID, err := consistentVacancyID(canonicalLinkIDs(canonicalLinks))
	if err != nil {
		return nil, fmt.Errorf("canonical vacancy links disagree: %w", err)
	}
	queryID, err := consistentVacancyID(queryIDs)
	if err != nil {
		return nil, fmt.Errorf("vacancyId query values disagree: %w", err)
	}
	if canonicalID > 0 && queryID > 0 && canonicalID != queryID {
		return nil, fmt.Errorf("canonical vacancy ID %d disagrees with vacancyId query %d", canonicalID, queryID)
	}

	if canonicalID > 0 {
		var sourceURL *url.URL
		if titleURL, resolveErr := resolveVacancyURL(htmlAttr(titleNode, "href"), baseURL); resolveErr == nil &&
			vacancyIDFromCanonicalPath(titleURL.Path) == canonicalID {
			sourceURL = titleURL
		} else {
			sourceURL = canonicalLinks[0]
		}
		return canonicalVacancyURL(baseURL, canonicalID, sourceURL), nil
	}
	if queryID > 0 {
		return canonicalVacancyURL(baseURL, queryID, nil), nil
	}

	return nil, errors.New("canonical vacancy URL or vacancyId query is missing")
}

func canonicalLinkIDs(links []*url.URL) []int {
	ids := make([]int, 0, len(links))
	for _, link := range links {
		ids = append(ids, vacancyIDFromCanonicalPath(link.Path))
	}
	return ids
}

func consistentVacancyID(ids []int) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	first := ids[0]
	for _, id := range ids[1:] {
		if id != first {
			return 0, fmt.Errorf("found %d and %d", first, id)
		}
	}
	return first, nil
}

func vacancyIDFromURLQuery(parsed *url.URL) int {
	if parsed == nil {
		return 0
	}
	id, err := strconv.Atoi(parsed.Query().Get("vacancyId"))
	if err == nil && id > 0 {
		return id
	}
	return 0
}

func canonicalVacancyURL(baseURL *url.URL, vacancyID int, sourceURL *url.URL) *url.URL {
	canonical := *baseURL
	canonical.Path = fmt.Sprintf("/vacancy/%d", vacancyID)
	canonical.RawPath = ""
	canonical.RawQuery = ""
	canonical.Fragment = ""
	if sourceURL != nil && sameURLOrigin(sourceURL, baseURL) {
		canonical.RawQuery = sourceURL.RawQuery
	}
	return &canonical
}

func sameURLOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func resolveVacancyURL(rawURL string, baseURL *url.URL) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("empty vacancy URL")
	}

	ref, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse vacancy URL: %w", err)
	}
	resolved := baseURL.ResolveReference(ref)
	if resolved.Scheme == "" || resolved.Host == "" {
		return nil, errors.New("vacancy URL has no scheme or host")
	}
	return resolved, nil
}

func vacancyIDFromCanonicalPath(path string) int {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || parts[0] != "vacancy" {
		return 0
	}
	id, err := strconv.Atoi(parts[1])
	if err == nil && id > 0 {
		return id
	}
	return 0
}

func parseVacancyCompensationHTML(card *xhtml.Node) Compensation {
	compensationNode := findHTMLNode(card, func(node *xhtml.Node) bool {
		if node.Type != xhtml.ElementNode {
			return false
		}
		for _, className := range strings.Fields(htmlAttr(node, "class")) {
			if strings.HasPrefix(className, "compensation-labels") {
				return true
			}
		}
		return htmlAttr(node, "data-qa") == "vacancy-serp__compensation"
	})
	if compensationNode == nil {
		return Compensation{}
	}

	salaryNode := firstElementChild(compensationNode)
	if salaryNode == nil {
		return Compensation{}
	}
	return parseSalaryText(normalizeHTMLText(htmlNodeText(salaryNode)))
}

func parseSalaryText(raw string) Compensation {
	text := normalizeHTMLText(raw)
	if text == "" {
		return Compensation{}
	}

	matches := vacancySalaryAmountRE.FindAllString(text, -1)
	amounts := make([]int, 0, len(matches))
	for _, match := range matches {
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, match)
		amount, err := strconv.Atoi(digits)
		if err != nil || amount <= 0 {
			return Compensation{}
		}
		amounts = append(amounts, amount)
	}
	if len(amounts) == 0 || len(amounts) > 2 {
		return Compensation{}
	}

	currency := salaryCurrency(text)
	if currency == "" {
		return Compensation{}
	}
	compensation := Compensation{Currency: currency}
	trimmed := strings.ToLower(strings.TrimSpace(text))
	fromPrefix := strings.HasPrefix(trimmed, "от ") || trimmed == "от"
	toPrefix := strings.HasPrefix(trimmed, "до ") || trimmed == "до"

	switch len(amounts) {
	case 1:
		switch {
		case fromPrefix:
			compensation.From = &amounts[0]
		case toPrefix:
			compensation.To = &amounts[0]
		default:
			compensation.From = &amounts[0]
			compensation.To = &amounts[0]
		}
	case 2:
		compensation.From = &amounts[0]
		compensation.To = &amounts[1]
	}

	return compensation
}

func salaryCurrency(text string) string {
	text = strings.ToLower(text)
	switch {
	case strings.Contains(text, "so'm"), strings.Contains(text, "сум"), strings.Contains(text, "uzs"):
		return "UZS"
	case strings.Contains(text, "₽"), strings.Contains(text, "руб"), strings.Contains(text, "rur"):
		return "RUR"
	case strings.Contains(text, "$"), strings.Contains(text, "usd"):
		return "USD"
	case strings.Contains(text, "€"), strings.Contains(text, "eur"):
		return "EUR"
	case strings.Contains(text, "kzt"), strings.Contains(text, "₸"):
		return "KZT"
	case strings.Contains(text, "byn"):
		return "BYN"
	default:
		return ""
	}
}

func firstElementChild(node *xhtml.Node) *xhtml.Node {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.ElementNode {
			return child
		}
	}
	return nil
}

func findHTMLNode(root *xhtml.Node, predicate func(*xhtml.Node) bool) *xhtml.Node {
	if root == nil {
		return nil
	}
	if predicate(root) {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := findHTMLNode(child, predicate); found != nil {
			return found
		}
	}
	return nil
}

func findHTMLNodes(root *xhtml.Node, predicate func(*xhtml.Node) bool) []*xhtml.Node {
	var nodes []*xhtml.Node
	var visit func(*xhtml.Node)
	visit = func(node *xhtml.Node) {
		if node == nil {
			return
		}
		if predicate(node) {
			nodes = append(nodes, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(root)
	return nodes
}

func htmlAttr(node *xhtml.Node, key string) string {
	if node == nil {
		return ""
	}
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func htmlNodeText(node *xhtml.Node) string {
	var builder strings.Builder
	var visit func(*xhtml.Node)
	visit = func(current *xhtml.Node) {
		if current == nil {
			return
		}
		if current.Type == xhtml.TextNode {
			builder.WriteString(current.Data)
			builder.WriteByte(' ')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return builder.String()
}

func normalizeHTMLText(value string) string {
	// x/net/html decodes character references while building the DOM.
	return strings.Join(strings.Fields(value), " ")
}
