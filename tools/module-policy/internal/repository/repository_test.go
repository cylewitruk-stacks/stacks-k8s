// Package repository verifies repository-wide documentation and build-context boundaries.
package repository

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestMarkdownLinksResolve(t *testing.T) {
	root := repositoryRoot(t)
	link := regexp.MustCompile(`\[[^]]+\]\(([^)]+)\)`)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			// Installed npm packages are external artifacts, not repository documentation.
			if entry.Name() == ".git" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		// #nosec G304 G122 -- Path is derived from repository fixtures or a private test directory, not a remote request.
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range link.FindAllStringSubmatch(string(content), -1) {
			target := strings.Split(match[1], "#")[0]
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			// #nosec G703 -- Path is derived from repository fixtures or a private test directory, not a remote request.
			if _, err := os.Stat(filepath.Join(filepath.Dir(path), filepath.FromSlash(target))); err != nil {
				t.Errorf("%s: unresolved link %q", path, match[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDockerContextIsDefaultDeny(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".dockerignore"))
	if err != nil {
		t.Fatal(err)
	}
	rules := make([]string, 0)
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rules = append(rules, line)
	}
	if len(rules) == 0 || rules[0] != "**" {
		t.Fatal("root Docker context must begin with a default-deny ** rule")
	}
	required := []string{
		"!apis/network/**",
		"!operators/network/**",
		"!operators/observability/**",
		"**/.claude",
		"**/.env",
		"**/.env.*",
	}
	lastNegation := -1
	positions := make(map[string]int, len(rules))
	for index, rule := range rules {
		positions[rule] = index
		if strings.HasPrefix(rule, "!") {
			lastNegation = index
		}
	}
	for _, expected := range required {
		if _, found := positions[expected]; !found {
			t.Errorf("root .dockerignore is missing required rule %q", expected)
		}
	}
	for _, sensitive := range []string{"**/.claude", "**/.env", "**/.env.*"} {
		if position, found := positions[sensitive]; found && position <= lastNegation {
			t.Errorf("sensitive exclusion %q must follow every allow rule", sensitive)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate contract test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
}
