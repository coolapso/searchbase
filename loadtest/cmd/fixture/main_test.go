package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFixtureContracts(t *testing.T) {
	server := httptest.NewServer(fixtureHandler(0, 0))
	defer server.Close()
	for _, size := range []string{"small", "medium", "large"} {
		response, err := http.Post(server.URL+"/search/text", "application/json", strings.NewReader(`{"query":"load-test-query-`+size+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		var results searchResponse
		if err := json.NewDecoder(response.Body).Decode(&results); err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if len(results.Results) != 3 || !strings.HasSuffix(results.Results[0].Href, "/"+size) {
			t.Fatalf("bad %s search response: %+v", size, results)
		}
		page, err := http.Get(server.URL + "/site/" + size + "-js")
		if err != nil {
			t.Fatal(err)
		}
		if page.StatusCode != 200 || page.Header.Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("bad %s JS page", size)
		}
		_ = page.Body.Close()
	}
	response, err := http.Post(server.URL+"/extract", "application/json", strings.NewReader(`{"url":"http://fixture:8081/site/small"}`))
	if err != nil {
		t.Fatal(err)
	}
	var extracted struct {
		Success  bool   `json:"success"`
		Markdown string `json:"markdown"`
	}
	if err := json.NewDecoder(response.Body).Decode(&extracted); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if !extracted.Success || extracted.Markdown == "" {
		t.Fatal("invalid mock extract response")
	}
	response, err = http.Post(server.URL+"/failure/status", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unexpected failure status: %d", response.StatusCode)
	}
	_ = response.Body.Close()
}
