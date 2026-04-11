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
