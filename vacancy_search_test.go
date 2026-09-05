package main

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func TestParseVacanciesFromSearchResponsePrefersEmbeddedJSON(t *testing.T) {
	baseURL := mustURL(t, "https://hh.example")
	body := []byte(`prefix,"vacancies":[{"vacancyId":123,"name":"Legacy vacancy","links":{"desktop":"https://hh.example/vacancy/123"}}]}`)

	vacancies, err := parseVacanciesFromSearchResponse(body, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(vacancies) != 1 || vacancies[0].ID != 123 || vacancies[0].Name != "Legacy vacancy" {
		t.Fatalf("embedded JSON parser was not used: %+v", vacancies)
	}
}

func TestParseVacanciesFromSearchHTML(t *testing.T) {
	baseURL := mustURL(t, "https://ekaterinburg.hh.ru/search/vacancy?text=python")
	html := `<!doctype html>
<main data-qa="vacancy-serp__results">
  <article data-qa="vacancy-serp__vacancy">
    <a data-qa="serp-item__title" href="/vacancy/136980025?query=python">  Python&nbsp;backend developer </a>
    <div class="compensation-labels--generated">
      <span>150&nbsp;000 – 200&nbsp;000 ₽ за месяц, на руки</span>
      <div data-qa="vacancy-serp__vacancy-work-experience-between3And6">Опыт 3-6 лет</div>
      <span data-qa="vacancy-label-work-schedule-remote">Можно удалённо</span>
    </div>
    <a data-qa="vacancy-serp__vacancy-employer" href="/employer/1523892"> ООО Example </a>
    <span data-qa="vacancy-serp__vacancy-address"> Москва </span>
    <a data-qa="vacancy-serp__vacancy_response" href="/applicant/vacancy_response?vacancyId=136980025&employerId=1523892">Откликнуться</a>
  </article>
  <article data-qa="vacancy-serp__vacancy">
    <a data-qa="serp-item__title" href="/search/vacancy?text=python">Fallback&nbsp;ID</a>
    <span class="compensation-labels_magritte"><span>до 150 000 руб за месяц</span></span>
    <a data-qa="vacancy-serp__vacancy-employer">Компания</a>
    <a data-qa="vacancy-serp__vacancy_response" href="/applicant/vacancy_response?vacancyId=136954048&employerId=11769934">Откликнуться</a>
  </article>
</main>`

	vacancies, err := parseVacanciesFromSearchHTML([]byte(html), baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(vacancies) != 2 {
		t.Fatalf("got %d vacancies, want 2", len(vacancies))
	}

	first := vacancies[0]
	if first.ID != 136980025 {
		t.Fatalf("canonical vacancy ID: got %d", first.ID)
	}
	if first.Name != "Python backend developer" {
		t.Fatalf("title: got %q", first.Name)
	}
	if first.Links["desktop"] != "https://ekaterinburg.hh.ru/vacancy/136980025?query=python" {
		t.Fatalf("absolute vacancy URL: got %q", first.Links["desktop"])
	}
	if first.Company.Name != "ООО Example" || first.Area.Name != "Москва" {
		t.Fatalf("company/area: company=%q area=%q", first.Company.Name, first.Area.Name)
	}
	if first.WorkExperience != "Опыт 3-6 лет" || first.WorkSchedule != "Можно удалённо" {
		t.Fatalf("experience/schedule: experience=%q schedule=%q", first.WorkExperience, first.WorkSchedule)
	}
	if first.ResponseURL != "" {
		t.Fatalf("available response was incorrectly marked as already sent: %q", first.ResponseURL)
	}
	assertCompensation(t, first.Compensation, 150000, 200000, "RUR")

	second := vacancies[1]
	if second.ID != 136954048 {
		t.Fatalf("query fallback vacancy ID: got %d", second.ID)
	}
	if second.Links["desktop"] != "https://ekaterinburg.hh.ru/vacancy/136954048" {
		t.Fatalf("query fallback desktop URL: got %q", second.Links["desktop"])
	}
	if second.Compensation.To == nil || *second.Compensation.To != 150000 || second.Compensation.From != nil {
		t.Fatalf("upper-bound salary parsed incorrectly: %+v", second.Compensation)
	}
}

func TestParseVacanciesFromSearchHTMLEmptyResults(t *testing.T) {
	baseURL := mustURL(t, "https://ekaterinburg.hh.ru/search/vacancy?text=python")
	html := `<!doctype html>
<main data-qa="vacancy-serp__results">
  <div data-qa="empty-vacancy-search-block">
    <h2 data-qa="title">Ничего не нашлось</h2>
  </div>
</main>`

	vacancies, err := parseVacanciesFromSearchHTML([]byte(html), baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if vacancies == nil {
		t.Fatal("empty search result returned a nil slice")
	}
	if len(vacancies) != 0 {
		t.Fatalf("got %d vacancies, want 0", len(vacancies))
	}
}

func TestParseVacanciesFromSearchHTMLRejectsUnrelatedHTML(t *testing.T) {
	baseURL := mustURL(t, "https://hh.example/search/vacancy?text=python")
	_, err := parseVacanciesFromSearchHTML([]byte(`<html><body><h1>Login required</h1></body></html>`), baseURL)
	if err == nil {
		t.Fatal("unrelated HTML was accepted as an empty search result")
	}
}

func TestVacancyURLFromCardHTMLCanonicalTitleURL(t *testing.T) {
	baseURL := mustURL(t, "https://hh.example/search/vacancy?text=python")
	document := parseTestHTML(t, `<article data-qa="vacancy-serp__vacancy">
  <a data-qa="serp-item__title" href="/vacancy/123?from=search">Canonical vacancy</a>
  <a data-qa="vacancy-serp__vacancy_response" href="/applicant/vacancy_response?vacancyId=123">Откликнуться</a>
</article>`)
	card := findHTMLNode(document, func(node *xhtml.Node) bool {
		return htmlAttr(node, "data-qa") == "vacancy-serp__vacancy"
	})
	titleNode := findHTMLNode(card, func(node *xhtml.Node) bool {
		return htmlAttr(node, "data-qa") == "serp-item__title"
	})

	vacancyURL, err := vacancyURLFromCardHTML(card, titleNode, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if vacancyURL.String() != "https://hh.example/vacancy/123?from=search" {
		t.Fatalf("canonical title URL: got %q", vacancyURL.String())
	}
}

func TestVacancyURLFromCardHTMLFallbackVacancyIDCreatesCanonicalURL(t *testing.T) {
	baseURL := mustURL(t, "https://hh.example/search/vacancy?text=python")
	document := parseTestHTML(t, `<article data-qa="vacancy-serp__vacancy">
  <a data-qa="serp-item__title" href="/search/vacancy?text=python">Fallback vacancy</a>
  <a data-qa="vacancy-serp__vacancy_response" href="/applicant/vacancy_response?vacancyId=456">Откликнуться</a>
</article>`)
	card := findHTMLNode(document, func(node *xhtml.Node) bool {
		return htmlAttr(node, "data-qa") == "vacancy-serp__vacancy"
	})
	titleNode := findHTMLNode(card, func(node *xhtml.Node) bool {
		return htmlAttr(node, "data-qa") == "serp-item__title"
	})

	vacancyURL, err := vacancyURLFromCardHTML(card, titleNode, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if vacancyURL.String() != "https://hh.example/vacancy/456" {
		t.Fatalf("fallback canonical URL: got %q", vacancyURL.String())
	}
}

func TestVacancyURLFromCardHTMLRejectsMismatchedIDs(t *testing.T) {
	baseURL := mustURL(t, "https://hh.example/search/vacancy?text=python")
	document := parseTestHTML(t, `<article data-qa="vacancy-serp__vacancy">
  <a data-qa="serp-item__title" href="/vacancy/123">Mismatched vacancy</a>
  <a data-qa="vacancy-serp__vacancy_response" href="/applicant/vacancy_response?vacancyId=456">Откликнуться</a>
</article>`)
	card := findHTMLNode(document, func(node *xhtml.Node) bool {
		return htmlAttr(node, "data-qa") == "vacancy-serp__vacancy"
	})
	titleNode := findHTMLNode(card, func(node *xhtml.Node) bool {
		return htmlAttr(node, "data-qa") == "serp-item__title"
	})

	if _, err := vacancyURLFromCardHTML(card, titleNode, baseURL); err == nil {
		t.Fatal("mismatched canonical and query IDs were accepted")
	}
}

func TestVacancyURLFromCardHTMLDoesNotUseResponseURLAsDesktopURL(t *testing.T) {
	baseURL := mustURL(t, "https://hh.example/search/vacancy?text=python")
	document := parseTestHTML(t, `<article data-qa="vacancy-serp__vacancy">
  <a data-qa="serp-item__title" href="/applicant/vacancy_response?vacancyId=789&employerId=42">Apply</a>
</article>`)
	card := findHTMLNode(document, func(node *xhtml.Node) bool {
		return htmlAttr(node, "data-qa") == "vacancy-serp__vacancy"
	})
	titleNode := findHTMLNode(card, func(node *xhtml.Node) bool {
		return htmlAttr(node, "data-qa") == "serp-item__title"
	})

	vacancyURL, err := vacancyURLFromCardHTML(card, titleNode, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if vacancyURL.String() != "https://hh.example/vacancy/789" {
		t.Fatalf("response URL was used as desktop URL: got %q", vacancyURL.String())
	}
}

func TestParseVacanciesFromSearchHTMLSkipsInvalidCard(t *testing.T) {
	baseURL := mustURL(t, "https://hh.example")
	html := `<article data-qa="vacancy-serp__vacancy"><a data-qa="serp-item__title" href="/vacancy/1"></a></article>
<article data-qa="vacancy-serp__vacancy"><a data-qa="serp-item__title" href="/vacancy/2">Valid</a></article>`

	vacancies, err := parseVacanciesFromSearchHTML([]byte(html), baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(vacancies) != 1 || vacancies[0].ID != 2 {
		t.Fatalf("invalid card was not skipped: %+v", vacancies)
	}
}

func TestParseSalaryText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		from     *int
		to       *int
		currency string
	}{
		{name: "from rubles", text: "от 100 000 ₽", from: intPtr(100000), currency: "RUR"},
		{name: "to rubles", text: "до 150 000 руб", to: intPtr(150000), currency: "RUR"},
		{name: "range en dash", text: "100 000 – 150 000 ₽", from: intPtr(100000), to: intPtr(150000), currency: "RUR"},
		{name: "range em dash", text: "100 000 — 150 000 RUR", from: intPtr(100000), to: intPtr(150000), currency: "RUR"},
		{name: "dollar", text: "от 2 000 USD", from: intPtr(2000), currency: "USD"},
		{name: "euro", text: "до 2 000 €", to: intPtr(2000), currency: "EUR"},
		{name: "tenge", text: "100 000 KZT", from: intPtr(100000), to: intPtr(100000), currency: "KZT"},
		{name: "belarusian ruble", text: "от 4 000 BYN", from: intPtr(4000), currency: "BYN"},
		{name: "unknown currency", text: "100 000 tokens", currency: ""},
		{name: "unknown format", text: "по договорённости", currency: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := parseSalaryText(test.text)
			assertCompensation(t, got, valueOrZero(test.from), valueOrZero(test.to), test.currency)
			if (test.from == nil) != (got.From == nil) || (test.to == nil) != (got.To == nil) {
				t.Fatalf("known bounds changed: got=%+v want from=%v to=%v", got, test.from, test.to)
			}
		})
	}
}

func TestParseVacanciesFromSearchResponseReportsBothFormats(t *testing.T) {
	baseURL := mustURL(t, "https://hh.example")
	_, err := parseVacanciesFromSearchResponse([]byte(`<html><body>not a vacancy page</body></html>`), baseURL)
	if err == nil || !strings.Contains(err.Error(), "embedded JSON") || !strings.Contains(err.Error(), "server-rendered HTML") {
		t.Fatalf("diagnostic error does not identify both parser attempts: %v", err)
	}
}

func assertCompensation(t *testing.T, got Compensation, from, to int, currency string) {
	t.Helper()
	if got.Currency != currency {
		t.Fatalf("currency: got %q, want %q", got.Currency, currency)
	}
	if from > 0 && (got.From == nil || *got.From != from) {
		t.Fatalf("from: got %+v, want %d", got.From, from)
	}
	if from == 0 && got.From != nil {
		t.Fatalf("from: got %d, want nil", *got.From)
	}
	if to > 0 && (got.To == nil || *got.To != to) {
		t.Fatalf("to: got %+v, want %d", got.To, to)
	}
	if to == 0 && got.To != nil {
		t.Fatalf("to: got %d, want nil", *got.To)
	}
}

func intPtr(value int) *int { return &value }

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func parseTestHTML(t *testing.T, source string) *xhtml.Node {
	t.Helper()
	document, err := xhtml.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return document
}
