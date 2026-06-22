package tools

import (
	"strings"
	"testing"
)

func TestToolGrammarEmpty(t *testing.T) {
	if g := ToolGrammar(nil); g != "" {
		t.Errorf("nil defs: got %q, want %q", g, "")
	}
	if g := ToolGrammar([]ToolDef{}); g != "" {
		t.Errorf("empty defs: got %q, want %q", g, "")
	}
}

func TestToolGrammarSingleTool(t *testing.T) {
	defs := []ToolDef{{Name: "search", Description: "search the web"}}
	g := ToolGrammar(defs)

	required := []string{
		`root      ::=`,
		`tool-name ::=`,
		`"\"search\""`,
		`object    ::=`,
		`value     ::=`,
		`array     ::=`,
		`string    ::=`,
		`number    ::=`,
		`ws        ::=`,
	}
	for _, want := range required {
		if !strings.Contains(g, want) {
			t.Errorf("grammar missing %q\nfull grammar:\n%s", want, g)
		}
	}
}

func TestToolGrammarMultipleTools(t *testing.T) {
	defs := []ToolDef{
		{Name: "search"},
		{Name: "get_weather"},
		{Name: "calc"},
	}
	g := ToolGrammar(defs)

	toolNameLine := ""
	for _, line := range strings.Split(g, "\n") {
		if strings.HasPrefix(line, "tool-name ::=") {
			toolNameLine = line
			break
		}
	}
	if toolNameLine == "" {
		t.Fatal("tool-name rule not found in grammar")
	}

	for _, name := range []string{`"\"search\""`, `"\"get_weather\""`, `"\"calc\""`} {
		if !strings.Contains(toolNameLine, name) {
			t.Errorf("tool-name line missing %q\nline: %s", name, toolNameLine)
		}
	}
	if !strings.Contains(toolNameLine, " | ") {
		t.Errorf("tool-name line missing \" | \" separators\nline: %s", toolNameLine)
	}
}

func TestToolGrammarSpecialCharsInName(t *testing.T) {
	defs := []ToolDef{{Name: `back\slash`}}
	g := ToolGrammar(defs)
	// backslash in name should be escaped to \\ in the GBNF literal
	if !strings.Contains(g, `"\\"`) {
		t.Errorf("backslash not escaped in grammar:\n%s", g)
	}
}

func TestGbnfQuoteName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"search", `"\"search\""`},
		{"a-b", `"\"a-b\""`},
		{`quo"te`, `"\"quo\"te\""`},
		{`back\slash`, `"\"back\\slash\""`},
	}
	for _, tt := range tests {
		got := gbnfQuoteName(tt.name)
		if got != tt.want {
			t.Errorf("gbnfQuoteName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
