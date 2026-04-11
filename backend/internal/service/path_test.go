package service

import (
	"reflect"
	"testing"
)

func TestNormalizePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "empty is root", raw: "", want: "/"},
		{name: "adds slash", raw: "a/b", want: "/a/b"},
		{name: "cleans repeated slash", raw: "/a//b/", want: "/a/b"},
		{name: "rejects parent traversal", raw: "/a/../b", wantErr: true},
		{name: "rejects encoded dot", raw: "/a/%2e%2e/b", wantErr: true},
		{name: "rejects backslash", raw: "/a\\b", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizePath(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDirectoryAncestors(t *testing.T) {
	t.Parallel()

	got := DirectoryAncestors("/a/b/c")
	want := []string{"/", "/a", "/a/b", "/a/b/c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestNormalizePathPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "adds slash", raw: "a/*", want: "/a/*"},
		{name: "recursive glob", raw: "/a/**", want: "/a/**"},
		{name: "rejects parent traversal", raw: "/a/../b", wantErr: true},
		{name: "rejects empty", raw: "", wantErr: true},
		{name: "rejects malformed glob", raw: "/a/[", wantErr: true},
		{name: "rejects empty character class", raw: "/a/[]", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizePathPattern(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatchPathPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		target  string
		want    bool
	}{
		{pattern: "/", target: "/", want: true},
		{pattern: "/", target: "/a", want: false},
		{pattern: "/a/*", target: "/a/b", want: true},
		{pattern: "/a/*", target: "/a/b/c", want: false},
		{pattern: "/a/**", target: "/a", want: true},
		{pattern: "/a/**", target: "/a/b/c", want: true},
		{pattern: "/docs-*/**", target: "/docs-cn/guide", want: true},
		{pattern: "/docs-?", target: "/docs-cn", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.pattern+" "+tt.target, func(t *testing.T) {
			t.Parallel()
			if got := MatchPathPattern(tt.pattern, tt.target); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
