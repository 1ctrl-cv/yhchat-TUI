// Package privacycheck guards the repository against accidentally committing
// real personal data (phone numbers). Tests must only use the reserved example
// numbers below.
package privacycheck

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// allowedNumbers are reserved example/test numbers. Any other mobile number in
// the repository fails this test.
var allowedNumbers = map[string]bool{
	"13800000000": true,
	"13800001234": true,
}

var (
	fullNumberPattern   = regexp.MustCompile(`\b1[3-9][0-9]{9}\b`)
	maskedNumberPattern = regexp.MustCompile(`\b1[3-9][0-9]\*{4}[0-9]{4}\b`)
	// allowedMasked mirrors allowedNumbers in masked display form.
	allowedMasked = map[string]bool{
		"138****0000": true,
		"138****1234": true,
	}
	textExtensions = map[string]bool{
		".go": true, ".md": true, ".txt": true, ".json": true,
		".ps1": true, ".sh": true, ".yml": true, ".yaml": true,
		".toml": true, ".mod": true, ".sum": true,
	}
	skipDirs = map[string]bool{
		".git": true, "vendor": true, "dist": true, "node_modules": true,
		".gocache": true, ".gopath": true, ".gomodcache": true, "_tools": true,
		"assets": true, "internal/tui/testdata/screens": true,
	}
)

// TestNoPersonalPhoneNumbers scans tracked text files for real-looking mobile
// numbers and only permits the reserved example numbers.
func TestNoPersonalPhoneNumbers(t *testing.T) {
	root := repoRoot(t)
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel != "." && skipDirs[rel] {
				return filepath.SkipDir
			}
			return nil
		}
		if !textExtensions[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(data)
		for _, match := range fullNumberPattern.FindAllString(text, -1) {
			if !allowedNumbers[match] {
				t.Errorf("%s contains a real-looking phone number (%s); use 13800000000 or 13800001234 in tests/docs", rel, maskNumber(match))
			}
		}
		for _, match := range maskedNumberPattern.FindAllString(text, -1) {
			if !allowedMasked[maskNumber(match)] {
				t.Errorf("%s contains a real-looking masked phone number (%s); use the reserved example numbers", rel, match)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk repository: %v", walkErr)
	}
}

// maskNumber keeps only enough digits to identify a violation without
// repeating the full number in the test output.
func maskNumber(number string) string {
	if len(number) < 7 {
		return number
	}
	return number[:3] + "****" + number[len(number)-4:]
}

// repoRoot returns the module root (the parent of internal/).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source file")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root not found at %s: %v", root, err)
	}
	return root
}
