package domain

import "testing"

func TestNormalizePathRejectsTraversalSegments(t *testing.T) {
	for _, input := range []string{
		"../secret.txt",
		"/workspace/../secret.txt",
		"/workspace/docs/../../secret.txt",
		`workspace\..\secret.txt`,
	} {
		t.Run(input, func(t *testing.T) {
			_, err := NormalizePath(input)
			if ErrorCodeOf(err) != CodeInvalidArgument {
				t.Fatalf("NormalizePath(%q) error = %v, want invalid argument", input, err)
			}
		})
	}
}

func TestNormalizePathCleansSafePath(t *testing.T) {
	got, err := NormalizePath(`workspace//docs\guide.md`)
	if err != nil {
		t.Fatalf("NormalizePath safe path: %v", err)
	}
	if got != "/workspace/docs/guide.md" {
		t.Fatalf("path = %q, want /workspace/docs/guide.md", got)
	}
}
