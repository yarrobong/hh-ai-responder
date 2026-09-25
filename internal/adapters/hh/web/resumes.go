package hhweb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type BrowserResume struct {
	BrowserHash string
	HHID        int64
	Title       string
	InternalID  string
	ProviderID  string
}

func ParseBrowserResumes(data []byte) ([]BrowserResume, error) {
	text := html.UnescapeString(string(data))
	start := strings.Index(text, `{"redirectConfig":`)
	if start < 0 {
		start = strings.Index(text, "{\"redirectConfig\":")
	}
	if start < 0 {
		return nil, errors.New("HH resume page state is missing")
	}
	var root map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader([]byte(text[start:])))
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("parse HH resume page state: %w", err)
	}
	var rawResumes []struct {
		Attributes struct {
			ID   string `json:"id"`
			Hash string `json:"hash"`
		} `json:"_attributes"`
		Title []struct {
			String string `json:"string"`
		} `json:"title"`
	}
	raw, ok := root["applicantResumes"]
	if !ok || json.Unmarshal(raw, &rawResumes) != nil {
		return nil, errors.New("HH resume page contains no valid applicant resumes")
	}
	if len(rawResumes) == 0 {
		return nil, errors.New("HH resume page contains no resumes")
	}
	result := make([]BrowserResume, 0, len(rawResumes))
	seen := make(map[string]struct{}, len(rawResumes))
	for index, rawResume := range rawResumes {
		hash := strings.TrimSpace(rawResume.Attributes.Hash)
		if hash == "" {
			return nil, fmt.Errorf("HH resume %d has no browser hash", index)
		}
		if _, exists := seen[hash]; exists {
			return nil, errors.New("HH resume page contains duplicate browser hashes")
		}
		seen[hash] = struct{}{}
		idText := strings.TrimSpace(rawResume.Attributes.ID)
		var id int64
		if idText != "" {
			parsed, err := strconv.ParseInt(idText, 10, 64)
			if err != nil || parsed <= 0 {
				return nil, fmt.Errorf("HH resume %d has invalid provider ID", index)
			}
			id = parsed
		}
		title := ""
		if len(rawResume.Title) > 0 {
			title = strings.TrimSpace(rawResume.Title[0].String)
		}
		result = append(result, BrowserResume{BrowserHash: hash, HHID: id, Title: title})
	}
	return result, nil
}

type ResumeMappingReader interface {
	ReadBrowserResumes(context.Context) ([]BrowserResume, error)
}

type HTTPResumeMappingReader struct {
	client interface {
		Do(*http.Request) (*http.Response, error)
	}
	baseURL *url.URL
}

func NewHTTPResumeMappingReader(client interface {
	Do(*http.Request) (*http.Response, error)
}, baseURL *url.URL) (*HTTPResumeMappingReader, error) {
	if client == nil || baseURL == nil || baseURL.Host == "" {
		return nil, errors.New("HH resume mapping client and base URL are required")
	}
	return &HTTPResumeMappingReader{client: client, baseURL: baseURL}, nil
}

func (r *HTTPResumeMappingReader) ReadBrowserResumes(ctx context.Context) ([]BrowserResume, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL.ResolveReference(&url.URL{Path: "/applicant/my_resumes"}).String(), nil)
	if err != nil {
		return nil, err
	}
	response, err := r.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HH resume mapping GET returned status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return ParseBrowserResumes(body)
}
