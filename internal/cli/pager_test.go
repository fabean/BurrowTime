package cli

import (
	"path/filepath"
	"testing"
)

func TestPlainLessPagerPreservesANSIColor(t *testing.T) {
	t.Setenv("LESS", "")
	environment := pagerEnvironment(filepath.Join(string(filepath.Separator), "usr", "bin", "less"), nil)
	if got := environmentValue(environment, "LESS"); got != "-R" {
		t.Fatalf("LESS=%q, want -R", got)
	}
}

func TestLessPagerRespectsExplicitOptions(t *testing.T) {
	t.Setenv("LESS", "-FX")
	environment := pagerEnvironment("less", nil)
	if got := environmentValue(environment, "LESS"); got != "-FX" {
		t.Fatalf("LESS=%q, want existing value", got)
	}
}

func environmentValue(environment []string, name string) string {
	prefix := name + "="
	value := ""
	for _, entry := range environment {
		if len(entry) >= len(prefix) && entry[:len(prefix)] == prefix {
			value = entry[len(prefix):]
		}
	}
	return value
}
