package template

import (
	"github.com/alaisi/syscalltodo/io"
	"github.com/alaisi/syscalltodo/str"
)

type Template interface {
	Execute(writer io.Writer, ctx map[string]any)
}

func Must(template Template, err error) Template {
	if err != nil {
		panic(err)
	}
	return template
}

func ParseFiles(filePath string) (Template, error) {
	text, err := io.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	ops, err := parse(string(text))
	if err != nil {
		return nil, err
	}
	return &executable{ops}, nil
}

type executable struct {
	operations []Template
}

func (ex *executable) Execute(writer io.Writer, ctx map[string]any) {
	for _, op := range ex.operations {
		op.Execute(writer, ctx)
	}
}

func parse(text string) ([]Template, error) {
	lexer := &lexer{text}
	operations := make([]Template, 0, 255)
	for {
		next, eof := lexer.pop()
		if eof {
			break
		}
		token, err := parseToken(*next, lexer)
		if err != nil {
			return nil, err
		}
		operations = append(operations, token...)
	}
	return operations, nil
}

func parseToken(token token, lexer *lexer) ([]Template, error) {
	if token.kind == textToken {
		return []Template{&textOp{token.value}}, nil
	}
	if token.kind != beginBrackets {
		return nil, parseError("Unexpected token: " + token.value)
	}
	tag, eof := lexer.pop()
	if eof {
		return nil, parseError("Unexpected end of template")
	}
	if err := lexer.assertNextIsEndBrackets(); err != nil {
		return nil, err
	}
	expr := str.Trim(tag.value)
	if len(expr) >= 1 && expr[0] == '.' {
		return parseValueExpr(expr[1:])
	}
	if len(expr) >= 4 && expr[0:4] == "if ." {
		return parseIfExpr(expr[4:], lexer)
	}
	if len(expr) >= 7 && expr[0:7] == "range ." {
		return parseRangeExpr(expr[7:], lexer)
	}
	return nil, parseError("Unexpected expression: " + expr)
}

func parseValueExpr(valueKey string) ([]Template, error) {
	return []Template{&valueOp{valueKey}}, nil
}

func parseIfExpr(conditionKey string, lexer *lexer) ([]Template, error) {
	ifBody := make([]Template, 0, 1)
	var elseBody []Template
	for {
		next, eof := lexer.pop()
		if eof {
			return nil, parseError("Unexpected end of template")
		}
		if next.kind == beginBrackets {
			peeked, eof := lexer.pop()
			if eof {
				return nil, parseError("Unexpected end of template")
			}
			tag := str.Trim(peeked.value)
			if tag == "end" {
				if err := lexer.assertNextIsEndBrackets(); err != nil {
					return nil, err
				}
				break
			}
			if tag == "else" {
				if err := lexer.assertNextIsEndBrackets(); err != nil {
					return nil, err
				}
				children, err := parseChildren(lexer)
				if err != nil {
					return nil, err
				}
				elseBody = children
				break
			}
			lexer.push(peeked)
		}
		token, err := parseToken(*next, lexer)
		if err != nil {
			return nil, err
		}
		ifBody = append(ifBody, token...)
	}
	return []Template{&ifOp{conditionKey, ifBody, elseBody}}, nil
}

func parseRangeExpr(sliceKey string, lexer *lexer) ([]Template, error) {
	children, err := parseChildren(lexer)
	if err != nil {
		return nil, err
	}
	return []Template{&rangeOp{sliceKey, children}}, nil
}

func parseChildren(lexer *lexer) ([]Template, error) {
	children := make([]Template, 0, 1)
	for {
		next, eof := lexer.pop()
		if eof {
			return nil, parseError("Unexpected end of template")
		}
		if next.kind == beginBrackets {
			peeked, eof := lexer.pop()
			if eof {
				return nil, parseError("Unexpected end of template")
			}
			if str.Trim(peeked.value) == "end" {
				if err := lexer.assertNextIsEndBrackets(); err != nil {
					return nil, err
				}
				return children, nil
			}
			lexer.push(peeked)
		}
		token, err := parseToken(*next, lexer)
		if err != nil {
			return nil, err
		}
		children = append(children, token...)
	}
}

type textOp struct {
	text string
}

func (op *textOp) Execute(writer io.Writer, ctx map[string]any) {
	writer.Write([]byte(op.text))
}

type valueOp struct {
	key string
}

func (op *valueOp) Execute(writer io.Writer, ctx map[string]any) {
	writeHtmlEscaped(writer, str.ToString(ctx[op.key]))
}

func writeHtmlEscaped(writer io.Writer, html string) {
	for _, c := range html {
		var escaped string
		switch c {
		case '<':
			escaped = "&lt;"
		case '>':
			escaped = "&gt;"
		case '&':
			escaped = "&amp;"
		case '"':
			escaped = "&#34;"
		case '\'':
			escaped = "&#39;"
		case '\x00':
			escaped = "\uFFFD"
		default:
			escaped = string(c)
		}
		writer.Write([]byte(escaped))
	}
}

type ifOp struct {
	key      string
	ifBody   []Template
	elseBody []Template
}

func (op *ifOp) Execute(writer io.Writer, ctx map[string]any) {
	if condition, isBool := ctx[op.key].(bool); isBool {
		if condition {
			executeChildren(op.ifBody, writer, ctx)
		} else {
			executeChildren(op.elseBody, writer, ctx)
		}
	}
}

func executeChildren(children []Template, writer io.Writer, ctx map[string]any) {
	for _, child := range children {
		child.Execute(writer, ctx)
	}
}

type rangeOp struct {
	key  string
	body []Template
}

func (op *rangeOp) Execute(writer io.Writer, ctx map[string]any) {
	if array, isMapArray := ctx[op.key].([]map[string]any); isMapArray {
		for _, item := range array {
			executeChildren(op.body, writer, item)
		}
	}
}

type tokenType int

const (
	textToken tokenType = iota
	beginBrackets
	endBrackets
)

type token struct {
	kind  tokenType
	value string
}
type lexer struct {
	src string
}

func (lexer *lexer) pop() (*token, bool) {
	if lexer.src == "" {
		return nil, true
	}
	split := -1
	for i, c := range lexer.src {
		if isDoubleBrackets(c, lexer.src, i) {
			split = i - 1
			break
		}
	}
	if split < 0 {
		next := lexer.src
		lexer.src = ""
		return &token{textToken, next}, false
	}
	if split == 0 {
		tokenType := beginBrackets
		if lexer.src[0] == '}' {
			tokenType = endBrackets
		}
		next := lexer.src[0:2]
		lexer.src = lexer.src[2:]
		return &token{tokenType, next}, false
	}
	next := lexer.src[0:split]
	lexer.src = lexer.src[split:]
	return &token{textToken, next}, false
}

func (lexer *lexer) push(token *token) {
	lexer.src = token.value + lexer.src
}

func isDoubleBrackets(c rune, text string, i int) bool {
	return i > 0 && text[i-1] == byte(c) && (c == '{' || c == '}')
}

func (lexer *lexer) assertNextIsEndBrackets() error {
	if next, eof := lexer.pop(); eof || next.kind != endBrackets {
		return parseError("Unexpected token, expected }}, got " + next.value)
	}
	return nil
}

type parseError string

func (p parseError) Error() string {
	return "Template Parsing failed: " + string(p)
}
