package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWatsonConfigBehavior(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	data := "[options]\nSTOP_ON_START = YeS\n[default_tags]\nmy project = one \"two three\" one\nmultiline =\n    one\n    two three\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Bool("options", "STOP_ON_START", false) {
		t.Fatal("option names should be case insensitive")
	}
	if got := c.List("default_tags", "my project"); !reflect.DeepEqual(got, []string{"one", "two three", "one"}) {
		t.Fatalf("quoted list: %#v", got)
	}
	if got := c.List("default_tags", "multiline"); !reflect.DeepEqual(got, []string{"one", "two three"}) {
		t.Fatalf("multiline list: %#v", got)
	}
}

func TestRawConfigParserInheritanceAndRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	data := "[DEFAULT]\nFoo = base\n\n[Mixed]\nBar = first\n second\nempty =\n  one\n  two\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.HasSection("mixed") || !c.HasSection("Mixed") {
		t.Fatal("section names must be case sensitive")
	}
	if got := c.Get("Mixed", "FOO", ""); got != "base" {
		t.Fatalf("inherited default = %q", got)
	}
	want := "[DEFAULT]\nfoo = base\n\n[Mixed]\nbar = first\n\tsecond\nempty = \n\tone\n\ttwo\n\n"
	if got := string(c.Bytes()); got != want {
		t.Fatalf("rewrite mismatch\nwant: %q\n got: %q", want, got)
	}
}

func TestRawConfigParserRejectsDuplicates(t *testing.T) {
	for name, data := range map[string]string{
		"section": "[s]\na=1\n[s]\nb=2\n",
		"option":  "[s]\na=1\nA=2\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "already exists") {
				t.Fatalf("expected duplicate error, got %v", err)
			}
		})
	}
}

func TestRawConfigParserErrorText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("outside=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	want := "File contains no section headers.\nfile: '" + path + "', line: 1\n'outside=1\\n'"
	if err == nil || err.Error() != want {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestShellFieldsPreservesEmptyQuotedItems(t *testing.T) {
	want := []string{"", "two three", "", `a\q`, `a"b`, `a\b`, "a b"}
	if got := shellFields(`'' "two three" "" "a\q" "a\"b" "a\\b" a\ b`); !reflect.DeepEqual(got, want) {
		t.Fatalf("want %#v, got %#v", want, got)
	}
}
