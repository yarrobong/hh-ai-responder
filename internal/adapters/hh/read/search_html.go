package hhread

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
	"hh-ai-responder/internal/vacancy"
)

var salaryAmountRE = regexp.MustCompile(`[0-9](?:[[:space:]]*[0-9])*`)

func parseVacancySearchHTML(data []byte, baseURL *url.URL) ([]vacancy.Vacancy, error) {
	if baseURL == nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, errors.New("base URL is required for HTML vacancy parsing")
	}
	document, err := xhtml.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}
	cards := findNodes(document, func(node *xhtml.Node) bool { return attr(node, "data-qa") == "vacancy-serp__vacancy" })
	if len(cards) == 0 {
		if findNodes(document, func(node *xhtml.Node) bool {
			qa := attr(node, "data-qa")
			return qa == "vacancy-serp__results" || qa == "empty-vacancy-search-block"
		}) != nil {
			return []vacancy.Vacancy{}, nil
		}
		return nil, errors.New(`vacancy card container "vacancy-serp__vacancy" not found`)
	}
	values := make([]vacancy.Vacancy, 0, len(cards))
	var firstErr error
	for _, card := range cards {
		value, parseErr := parseVacancyCard(card, baseURL)
		if parseErr != nil {
			if firstErr == nil {
				firstErr = parseErr
			}
			continue
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		if firstErr != nil {
			return nil, fmt.Errorf("no valid vacancy cards: %w", firstErr)
		}
		return nil, errors.New("no valid vacancy cards")
	}
	return values, nil
}

func parseVacancyCard(card *xhtml.Node, baseURL *url.URL) (vacancy.Vacancy, error) {
	title := findNode(card, func(node *xhtml.Node) bool {
		return node.Type == xhtml.ElementNode && attr(node, "data-qa") == "serp-item__title"
	})
	if title == nil {
		return vacancy.Vacancy{}, errors.New("vacancy title link not found")
	}
	name := normalizeText(nodeText(title))
	if name == "" {
		return vacancy.Vacancy{}, errors.New("vacancy title is empty")
	}
	link, err := vacancyURL(card, title, baseURL)
	if err != nil {
		return vacancy.Vacancy{}, err
	}
	value := vacancy.Vacancy{ID: vacancyID(link.Path), Name: name, Links: map[string]string{"desktop": link.String()}, Compensation: parseCompensation(card)}
	if value.ID <= 0 {
		return vacancy.Vacancy{}, errors.New("vacancy ID not found in canonical URL")
	}
	if node := findNode(card, func(node *xhtml.Node) bool {
		return node.Type == xhtml.ElementNode && attr(node, "data-qa") == "vacancy-serp__vacancy-employer"
	}); node != nil {
		value.Company.Name = normalizeText(nodeText(node))
	}
	if node := findNode(card, func(node *xhtml.Node) bool {
		return node.Type == xhtml.ElementNode && attr(node, "data-qa") == "vacancy-serp__vacancy-address"
	}); node != nil {
		value.Area.Name = normalizeText(nodeText(node))
	}
	if node := findNode(card, func(node *xhtml.Node) bool {
		qa := attr(node, "data-qa")
		return node.Type == xhtml.ElementNode && strings.HasPrefix(qa, "vacancy-serp__vacancy-work-experience-")
	}); node != nil {
		value.WorkExperience = normalizeText(nodeText(node))
	}
	if node := findNode(card, func(node *xhtml.Node) bool {
		qa := attr(node, "data-qa")
		return node.Type == xhtml.ElementNode && strings.HasPrefix(qa, "vacancy-label-work-schedule-")
	}); node != nil {
		value.WorkSchedule = normalizeText(nodeText(node))
	}
	return value, nil
}

func vacancyURL(card, title *xhtml.Node, baseURL *url.URL) (*url.URL, error) {
	var canonical []*url.URL
	var queryIDs []int
	for _, node := range findNodes(card, func(node *xhtml.Node) bool {
		return node.Type == xhtml.ElementNode && node.Data == "a" && attr(node, "href") != ""
	}) {
		parsed, err := resolveURL(attr(node, "href"), baseURL)
		if err != nil {
			continue
		}
		if id := vacancyID(parsed.Path); id > 0 {
			canonical = append(canonical, parsed)
		}
		if id := queryVacancyID(parsed); id > 0 {
			queryIDs = append(queryIDs, id)
		}
	}
	canonicalID, err := consistentID(linkIDs(canonical))
	if err != nil {
		return nil, fmt.Errorf("canonical vacancy links disagree: %w", err)
	}
	queryID, err := consistentID(queryIDs)
	if err != nil {
		return nil, fmt.Errorf("vacancyId query values disagree: %w", err)
	}
	if canonicalID > 0 && queryID > 0 && canonicalID != queryID {
		return nil, fmt.Errorf("canonical vacancy ID %d disagrees with vacancyId query %d", canonicalID, queryID)
	}
	if canonicalID == 0 && queryID == 0 {
		return nil, errors.New("canonical vacancy URL or vacancyId query is missing")
	}
	id := canonicalID
	if id == 0 {
		id = queryID
	}
	return canonicalURL(baseURL, id), nil
}
func linkIDs(values []*url.URL) []int {
	result := make([]int, 0, len(values))
	for _, value := range values {
		result = append(result, vacancyID(value.Path))
	}
	return result
}
func consistentID(values []int) (int, error) {
	if len(values) == 0 {
		return 0, nil
	}
	for _, value := range values[1:] {
		if value != values[0] {
			return 0, fmt.Errorf("found %d and %d", values[0], value)
		}
	}
	return values[0], nil
}
func queryVacancyID(value *url.URL) int {
	if value == nil {
		return 0
	}
	id, err := strconv.Atoi(value.Query().Get("vacancyId"))
	if err == nil && id > 0 {
		return id
	}
	return 0
}
func canonicalURL(baseURL *url.URL, id int) *url.URL {
	result := *baseURL
	result.Path = fmt.Sprintf("/vacancy/%d", id)
	result.RawPath = ""
	result.RawQuery = ""
	result.Fragment = ""
	return &result
}
func resolveURL(raw string, base *url.URL) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty vacancy URL")
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	result := base.ResolveReference(ref)
	if result.Scheme == "" || result.Host == "" {
		return nil, errors.New("vacancy URL has no scheme or host")
	}
	return result, nil
}
func vacancyID(path string) int {
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

func parseCompensation(card *xhtml.Node) vacancy.Compensation {
	node := findNode(card, func(node *xhtml.Node) bool {
		if node.Type != xhtml.ElementNode {
			return false
		}
		for _, className := range strings.Fields(attr(node, "class")) {
			if strings.HasPrefix(className, "compensation-labels") {
				return true
			}
		}
		return attr(node, "data-qa") == "vacancy-serp__compensation"
	})
	if node == nil {
		return vacancy.Compensation{}
	}
	child := firstChild(node)
	if child == nil {
		return vacancy.Compensation{}
	}
	return parseSalary(normalizeText(nodeText(child)))
}
func parseSalary(raw string) vacancy.Compensation {
	text := normalizeText(raw)
	if text == "" {
		return vacancy.Compensation{}
	}
	matches := salaryAmountRE.FindAllString(text, -1)
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
			return vacancy.Compensation{}
		}
		amounts = append(amounts, amount)
	}
	if len(amounts) == 0 || len(amounts) > 2 {
		return vacancy.Compensation{}
	}
	currency := salaryCurrency(text)
	if currency == "" {
		return vacancy.Compensation{}
	}
	result := vacancy.Compensation{Currency: currency}
	lower := strings.ToLower(strings.TrimSpace(text))
	from, to := strings.HasPrefix(lower, "от ") || lower == "от", strings.HasPrefix(lower, "до ") || lower == "до"
	switch len(amounts) {
	case 1:
		if from {
			result.From = &amounts[0]
		} else if to {
			result.To = &amounts[0]
		} else {
			result.From, result.To = &amounts[0], &amounts[0]
		}
	case 2:
		result.From, result.To = &amounts[0], &amounts[1]
	}
	return result
}
func salaryCurrency(value string) string {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "руб"):
		return "RUR"
	case strings.Contains(lower, "$"), strings.Contains(lower, "usd"):
		return "USD"
	case strings.Contains(lower, "€"), strings.Contains(lower, "eur"):
		return "EUR"
	default:
		return ""
	}
}
func findNodes(root *xhtml.Node, match func(*xhtml.Node) bool) []*xhtml.Node {
	result := []*xhtml.Node{}
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if match(node) {
			result = append(result, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	if root != nil {
		walk(root)
	}
	return result
}
func findNode(root *xhtml.Node, match func(*xhtml.Node) bool) *xhtml.Node {
	values := findNodes(root, match)
	if len(values) == 0 {
		return nil
	}
	return values[0]
}
func attr(node *xhtml.Node, name string) string {
	if node == nil {
		return ""
	}
	for _, value := range node.Attr {
		if value.Key == name {
			return value.Val
		}
	}
	return ""
}
func nodeText(node *xhtml.Node) string {
	var result strings.Builder
	var walk func(*xhtml.Node)
	walk = func(value *xhtml.Node) {
		if value.Type == xhtml.TextNode {
			result.WriteString(value.Data)
		}
		for child := value.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	if node != nil {
		walk(node)
	}
	return result.String()
}
func normalizeText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}
func firstChild(node *xhtml.Node) *xhtml.Node {
	if node == nil {
		return nil
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.ElementNode {
			return child
		}
	}
	return nil
}
