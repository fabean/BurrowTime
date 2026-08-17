package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"
)

// Config implements the deliberately small RawConfigParser surface Watson
// uses. Reading it never rewrites the user's file. Section names retain the
// case-sensitive semantics of Python's parser; option names are normalized to
// lower case.
type Config struct {
	defaults     map[string]string
	defaultOrder []string
	sections     map[string]map[string]string
	sectionOrder []string
	optionOrder  map[string][]string
}

func empty() *Config {
	return &Config{
		defaults:    map[string]string{},
		sections:    map[string]map[string]string{},
		optionOrder: map[string][]string{},
	}
}

// New returns an empty Watson configuration.
func New() *Config { return empty() }

func Load(path string) (*Config, error) {
	c := empty()
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	section := ""
	lastKey := ""
	lastIndent := 0
	lineNo := 0
	scanner := bufio.NewScanner(f)
	// ConfigParser has no Scanner-like 64 KiB line limit.
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		lineNo++
		raw := strings.TrimSuffix(scanner.Text(), "\r")
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}

		indent := leadingWhitespace(raw)
		if lastKey != "" && indent > lastIndent {
			values := c.sections[section]
			if section == "DEFAULT" {
				values = c.defaults
			}
			values[lastKey] += "\n" + trimmed
			continue
		}

		if strings.HasPrefix(trimmed, "[") {
			end := strings.LastIndexByte(trimmed, ']')
			if end > 1 {
				section = trimmed[1:end]
				if section == "DEFAULT" {
					if len(c.defaults) > 0 || contains(c.sectionOrder, "DEFAULT") {
						return nil, duplicateSection(path, lineNo, section)
					}
					// Track an explicitly present, even empty, DEFAULT section.
					c.sectionOrder = append(c.sectionOrder, "DEFAULT")
				} else {
					if _, exists := c.sections[section]; exists {
						return nil, duplicateSection(path, lineNo, section)
					}
					c.sections[section] = map[string]string{}
					c.sectionOrder = append(c.sectionOrder, section)
				}
				lastKey = ""
				lastIndent = 0
				continue
			}
		}

		if section == "" {
			return nil, missingSection(path, lineNo, raw)
		}
		idx := strings.IndexAny(raw, "=:")
		if idx < 0 {
			return nil, parsingError(path, lineNo, raw)
		}
		key := strings.ToLower(strings.TrimSpace(raw[:idx]))
		if key == "" {
			return nil, parsingError(path, lineNo, raw)
		}
		values := c.sections[section]
		orderKey := section
		if section == "DEFAULT" {
			values = c.defaults
			orderKey = "DEFAULT"
		}
		if _, exists := values[key]; exists {
			return nil, duplicateOption(path, lineNo, section, key)
		}
		if section == "DEFAULT" {
			c.defaultOrder = append(c.defaultOrder, key)
		} else {
			c.optionOrder[orderKey] = append(c.optionOrder[orderKey], key)
		}
		values[key] = strings.TrimSpace(raw[idx+1:])
		lastKey = key
		lastIndent = indent
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return c, nil
}

func leadingWhitespace(value string) int {
	for i, r := range value {
		if !unicode.IsSpace(r) {
			return i
		}
	}
	return len(value)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func duplicateSection(path string, line int, section string) error {
	return fmt.Errorf("While reading from %s [line %2d]: section %s already exists", pythonRepr(path), line, pythonRepr(section))
}

func duplicateOption(path string, line int, section, option string) error {
	return fmt.Errorf("While reading from %s [line %2d]: option %s in section %s already exists", pythonRepr(path), line, pythonRepr(option), pythonRepr(section))
}

func parsingError(path string, line int, raw string) error {
	return fmt.Errorf("Source contains parsing errors: %s\n\t[line %2d]: %s", pythonRepr(path), line, pythonRepr(raw+"\n"))
}

func missingSection(path string, line int, raw string) error {
	return fmt.Errorf("File contains no section headers.\nfile: %s, line: %d\n%s", pythonRepr(path), line, pythonRepr(raw+"\n"))
}

func pythonRepr(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "'", "\\'")
	value = strings.ReplaceAll(value, "\t", "\\t")
	value = strings.ReplaceAll(value, "\r", "\\r")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return "'" + value + "'"
}

func (c *Config) Set(section, option, value string) error {
	if section == "DEFAULT" {
		return errors.New("Invalid section name: 'DEFAULT'")
	}
	option = strings.ToLower(option)
	if c.sections[section] == nil {
		c.sections[section] = map[string]string{}
		c.sectionOrder = append(c.sectionOrder, section)
	}
	if _, exists := c.sections[section][option]; !exists {
		c.optionOrder[section] = append(c.optionOrder[section], option)
	}
	c.sections[section][option] = value
	return nil
}

func writeOption(b *bytes.Buffer, option, value string) {
	fmt.Fprintf(b, "%s = %s\n", option, strings.ReplaceAll(value, "\n", "\n\t"))
}

func (c *Config) Bytes() []byte {
	var b bytes.Buffer
	if contains(c.sectionOrder, "DEFAULT") {
		b.WriteString("[DEFAULT]\n")
		for _, option := range c.defaultOrder {
			writeOption(&b, option, c.defaults[option])
		}
		b.WriteByte('\n')
	}
	for _, section := range c.sectionOrder {
		if section == "DEFAULT" {
			continue
		}
		fmt.Fprintf(&b, "[%s]\n", section)
		for _, option := range c.optionOrder[section] {
			writeOption(&b, option, c.sections[section][option])
		}
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func (c *Config) Get(section, option, fallback string) string {
	option = strings.ToLower(option)
	if opts := c.sections[section]; opts != nil {
		if value, ok := opts[option]; ok {
			return value
		}
		if value, ok := c.defaults[option]; ok {
			return value
		}
	}
	return fallback
}

func (c *Config) HasSection(section string) bool { return c.sections[section] != nil }
func (c *Config) HasOption(section, option string) bool {
	options := c.sections[section]
	if options == nil {
		return false
	}
	option = strings.ToLower(option)
	if _, ok := options[option]; ok {
		return true
	}
	_, ok := c.defaults[option]
	return ok
}

func (c *Config) Bool(section, option string, fallback bool) bool {
	value := c.Get(section, option, "")
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
}

func (c *Config) List(section, option string) []string {
	value := c.Get(section, option, "")
	if strings.Contains(value, "\n") {
		items := []string{}
		for _, item := range strings.Split(value, "\n") {
			if item = strings.TrimSpace(item); item != "" {
				items = append(items, item)
			}
		}
		return items
	}
	return shellFields(value)
}

func shellFields(value string) []string {
	var out []string
	var current strings.Builder
	var quote rune
	escaped := false
	escapedInDouble := false
	tokenStarted := false
	flush := func() {
		if tokenStarted {
			out = append(out, current.String())
			current.Reset()
			tokenStarted = false
		}
	}
	for _, r := range value {
		if escaped {
			if escapedInDouble && !strings.ContainsRune("\"\\$`", r) && r != '\n' {
				current.WriteRune('\\')
			}
			current.WriteRune(r)
			tokenStarted = true
			escaped = false
			escapedInDouble = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			escapedInDouble = quote == '"'
			tokenStarted = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			tokenStarted = true
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			tokenStarted = true
			continue
		}
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		current.WriteRune(r)
		tokenStarted = true
	}
	if escaped {
		current.WriteRune('\\')
	}
	flush()
	return out
}
