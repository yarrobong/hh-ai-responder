package hhweb

import "testing"

func TestParseBrowserResumesUsesExactEmbeddedIdentity(t *testing.T) {
	data := []byte(`<script>{"redirectConfig":{},"applicantResumes":[{"_attributes":{"id":"123","hash":"browser-hash-123"},"title":[{"string":"Backend developer"}]},{"_attributes":{"id":"456","hash":"browser-hash-456"},"title":[{"string":"Support specialist"}]}]}</script>`)
	resumes, err := ParseBrowserResumes(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(resumes) != 2 || resumes[0].BrowserHash != "browser-hash-123" || resumes[0].HHID != 123 || resumes[0].Title != "Backend developer" {
		t.Fatalf("resumes=%+v", resumes)
	}
}

func TestParseBrowserResumesRejectsMissingOrDuplicateHash(t *testing.T) {
	cases := []string{
		`{"redirectConfig":{},"applicantResumes":[{"_attributes":{"id":"1"}}]}`,
		`{"redirectConfig":{},"applicantResumes":[{"_attributes":{"id":"1","hash":"same"}},{"_attributes":{"id":"2","hash":"same"}}]}`,
		`{"redirectConfig":{},"applicantResumes":"malformed"}`,
	}
	for _, data := range cases {
		if _, err := ParseBrowserResumes([]byte(data)); err == nil {
			t.Errorf("invalid resume page parsed: %s", data)
		}
	}
}

func TestParseBrowserResumesDoesNotSelectByTitle(t *testing.T) {
	data := []byte(`{"redirectConfig":{},"applicantResumes":[{"_attributes":{"id":"1","hash":"hash-1"},"title":[{"string":"Target"}]}]}`)
	resumes, err := ParseBrowserResumes(data)
	if err != nil {
		t.Fatal(err)
	}
	if resumes[0].InternalID != "" || resumes[0].ProviderID != "" {
		t.Fatalf("parser invented local identity: %+v", resumes[0])
	}
}
