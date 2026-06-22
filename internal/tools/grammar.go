package tools

import "strings"

// toolGrammarHeader — root rule; tool-name rule is appended after this.
const toolGrammarHeader = `root      ::= "{" ws "\"tool\"" ws ":" ws tool-name ws "," ws "\"arguments\"" ws ":" ws object "}" ws
`

// toolGrammarBody — standard JSON rules compatible with llama.cpp GBNF.
//
// Known limitation: stop strings in toGenOpts include "\n", so grammar-
// constrained JSON containing newlines in string values may be truncated early.
// Fix in a follow-up by allowing multi-line generation when grammar is active.
const toolGrammarBody = `object    ::= "{" ws (string ":" ws value ("," ws string ":" ws value)*)? "}" ws
value     ::= object | array | string | number | ("true" | "false" | "null") ws
array     ::= "[" ws (value ("," ws value)*)? "]" ws
string    ::= "\"" ([^"\\\x7F\x00-\x1F] | "\\" (["\\bfnrt] | "u" [0-9a-fA-F]{4}))* "\"" ws
number    ::= ("-"? ([0-9] | [1-9] [0-9]{0,15})) ("." [0-9]+)? ([eE] [-+]? [0-9] [1-9]{0,15})? ws
ws        ::= | " " | "\n" [ \t]{0,20}
`

// ToolGrammar generates a GBNF grammar that constrains model output to:
//
//	{"tool": "<one-of-names>", "arguments": {<any-valid-json>}}
//
// Returns "" when defs is empty (caller should skip injection).
// Output is valid input for llama.SamplerInitGrammar(vocab, grammar, "root").
func ToolGrammar(defs []ToolDef) string {
	if len(defs) == 0 {
		return ""
	}
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = gbnfQuoteName(d.Name)
	}
	var b strings.Builder
	b.WriteString(toolGrammarHeader)
	b.WriteString("tool-name ::= ")
	b.WriteString(strings.Join(names, " | "))
	b.WriteByte('\n')
	b.WriteString(toolGrammarBody)
	return b.String()
}

// gbnfQuoteName wraps a tool name as a GBNF quoted literal that matches
// the JSON string "name". Example: "search" → `"\"search\""`.
func gbnfQuoteName(name string) string {
	var b strings.Builder
	b.WriteString(`"\"`)
	for _, ch := range name {
		switch ch {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteRune(ch)
		}
	}
	b.WriteString(`\""`)
	return b.String()
}
