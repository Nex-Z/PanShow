package storage

import (
	"strings"
	"testing"
)

func TestDownloadContentDispositionUsesSourceBaseName(t *testing.T) {
	got := downloadContentDisposition("/docs/report final.pdf")
	want := `attachment; filename="report final.pdf"; filename*=UTF-8''report%20final.pdf`
	if got != want {
		t.Fatalf("disposition = %q, want %q", got, want)
	}
}

func TestDownloadContentDispositionSupportsUTF8Filename(t *testing.T) {
	got := downloadContentDisposition("/docs/测试.txt")
	if !strings.Contains(got, `filename="__.txt"`) {
		t.Fatalf("disposition = %q, want ASCII fallback filename", got)
	}
	if !strings.Contains(got, `filename*=UTF-8''%E6%B5%8B%E8%AF%95.txt`) {
		t.Fatalf("disposition = %q, want UTF-8 encoded filename", got)
	}
}

func TestPublicObjectURLUsesCustomDomainAndRootPrefix(t *testing.T) {
	publicBaseURL, err := parsePublicBaseURL("https://assets.example.com/media/")
	if err != nil {
		t.Fatalf("parse public base URL: %v", err)
	}
	client := &Client{
		rootPrefix:    "private-root",
		publicBaseURL: publicBaseURL,
	}

	got, ok := client.publicObjectURL("/docs/report final.pdf")
	if !ok {
		t.Fatal("public object URL was not available")
	}
	want := "https://assets.example.com/media/private-root/docs/report%20final.pdf"
	if got != want {
		t.Fatalf("public URL = %q, want %q", got, want)
	}
}

func TestParsePublicBaseURLRejectsMissingHost(t *testing.T) {
	if _, err := parsePublicBaseURL("https:///assets"); err == nil {
		t.Fatal("parse public base URL succeeded without a host")
	}
}
