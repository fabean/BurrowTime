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

func TestConfigSetLookupAndBooleanFallbacks(t *testing.T) {
	c := New()
	if err := c.Set("DEFAULT", "option", "bad"); err == nil {
		t.Fatal("DEFAULT set accepted")
	}
	if err := c.Set("Mixed", "UPPER", "first"); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("Mixed", "upper", "second"); err != nil {
		t.Fatal(err)
	}
	if !c.HasOption("Mixed", "UPPER") || c.HasOption("missing", "upper") || c.HasOption("Mixed", "missing") {
		t.Fatal("option presence mismatch")
	}
	if c.Get("Mixed", "upper", "") != "second" || c.Get("missing", "upper", "fallback") != "fallback" {
		t.Fatal("lookup mismatch")
	}
	if !c.Bool("Mixed", "missing", true) {
		t.Fatal("boolean fallback ignored")
	}
	if err := c.Set("Mixed", "bool", "off"); err != nil {
		t.Fatal(err)
	}
	if c.Bool("Mixed", "bool", true) {
		t.Fatal("false boolean parsed as true")
	}
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("[DEFAULT]\ninherited=yes\n[Mixed]\nvalue=own\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil || !c.HasOption("Mixed", "INHERITED") || c.HasOption("Mixed", "missing") {
		t.Fatalf("inheritance err=%v", err)
	}
}

func TestConfigParseErrorCases(t *testing.T) {
	tests := []struct{ name, data, want string }{
		{"empty default duplicate", "[DEFAULT]\n[DEFAULT]\n", "already exists"},
		{"default duplicate", "[DEFAULT]\na=1\n[DEFAULT]\n", "already exists"},
		{"missing equals", "[s]\ninvalid\n", "parsing errors"},
		{"empty key", "[s]\n=invalid\n", "parsing errors"},
		{"unterminated header", "[s]\n[invalid\n", "parsing errors"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			if err := os.WriteFile(path, []byte(tt.data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("directory accepted as config")
	}
	if leadingWhitespace(" \t") != 2 {
		t.Fatal("whitespace count mismatch")
	}
	if got := shellFields("trailing\\"); !reflect.DeepEqual(got, []string{"trailing\\"}) {
		t.Fatalf("trailing escape=%#v", got)
	}
}
