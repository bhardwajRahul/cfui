package r2dav

import "testing"

func TestCleanPathRejectsTraversal(t *testing.T) {
	tests := []string{
		"../secret",
		"folder/../secret",
		`C:\secret`,
		`folder\secret`,
	}
	for _, tc := range tests {
		t.Run(tc, func(t *testing.T) {
			if _, err := CleanPath(tc, true); err == nil {
				t.Fatalf("expected %q to be rejected", tc)
			}
		})
	}
}

func TestCleanPathNormalizesSafePaths(t *testing.T) {
	got, err := CleanPath("folder//child.txt", true)
	if err != nil {
		t.Fatalf("CleanPath: %v", err)
	}
	if got != "/folder/child.txt" {
		t.Fatalf("unexpected cleaned path %q", got)
	}
	root, err := CleanPath("", false)
	if err != nil {
		t.Fatalf("CleanPath root: %v", err)
	}
	if root != "/" {
		t.Fatalf("unexpected root path %q", root)
	}
}
