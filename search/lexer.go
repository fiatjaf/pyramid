package search

import (
	"strings"
	"unicode"
)

// lexer tokenizes the input string
type Lexer struct {
	input string
	pos   int

	peekedQueue []Token
}

func NewLexer(input string) *Lexer {
	return &Lexer{input: input, pos: 0}
}

func (l *Lexer) peek() rune {
	if l.pos >= len(l.input) {
		return 0
	}
	return rune(l.input[l.pos])
}

func (l *Lexer) advance() rune {
	if l.pos >= len(l.input) {
		return 0
	}
	ch := rune(l.input[l.pos])
	l.pos++
	return ch
}

func (l *Lexer) skipWhitespace() {
	for l.peek() != 0 && unicode.IsSpace(l.peek()) {
		l.advance()
	}
}

func (l *Lexer) readWord() string {
	start := l.pos

	// read regular word (alphanumeric, hyphens, underscores)
	for l.peek() != 0 && !unicode.IsSpace(l.peek()) &&
		l.peek() != '(' && l.peek() != ')' && l.peek() != '"' {
		l.advance()
	}

	return l.input[start:l.pos]
}

func (l *Lexer) PeekToken() Token {
	next := l.NextToken()
	l.peekedQueue = append(l.peekedQueue, next)
	return next
}

func (l *Lexer) ReturnToken(tok Token) {
	l.peekedQueue = append(l.peekedQueue, tok)
}

func (l *Lexer) NextToken() (tok Token) {
	if len(l.peekedQueue) > 0 {
		next := l.peekedQueue[len(l.peekedQueue)-1]
		l.peekedQueue = l.peekedQueue[0 : len(l.peekedQueue)-1]
		return next
	}

	l.skipWhitespace()

	if l.pos >= len(l.input) {
		return Token{Type: TokenEOF}
	}

	ch := l.peek()

	switch ch {
	case '(':
		l.advance()
		return Token{Type: TokenLParen, Value: "("}
	case ')':
		l.advance()
		return Token{Type: TokenRParen, Value: ")"}
	case '"':
		l.advance()
		return Token{Type: TokenQuote, Value: "\""}
	default:
		word := l.readWord()
		upperWord := strings.ToUpper(word)

		switch upperWord {
		case "OR", "||":
			return Token{Type: TokenOR, Value: word}
		case "AND", "&&":
			return Token{Type: TokenAND, Value: word}
		case "NOT", "!":
			return Token{Type: TokenNOT, Value: word}
		default:
			return Token{Type: TokenWord, Value: word}
		}
	}
}
