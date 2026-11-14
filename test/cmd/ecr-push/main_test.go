package main

import (
	"slices"
	"testing"
)

func TestParseRef(t *testing.T) {
	tests := []struct {
		ref  string
		repo string
		tags []string
	}{
		{"app/web", "app/web", nil},
		{"app/web:", "app/web", nil},
		{"app/web:v1", "app/web", []string{"v1"}},
		{"app/web:v6,latest", "app/web", []string{"v6", "latest"}},
	}
	for _, tt := range tests {
		repo, tags := parseRef(tt.ref)
		if repo != tt.repo || !slices.Equal(tags, tt.tags) {
			t.Errorf("parseRef(%q) = %q, %q; want %q, %q", tt.ref, repo, tags, tt.repo, tt.tags)
		}
	}
}
