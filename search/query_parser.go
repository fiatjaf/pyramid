package search

import (
	"strings"

	bleve "github.com/blevesearch/bleve/v2"
	bleveQuery "github.com/blevesearch/bleve/v2/search/query"
)

// token types
type TokenType int

const (
	TokenWord TokenType = iota
	TokenOR
	TokenAND
	TokenNOT
	TokenLParen
	TokenRParen
	TokenQuote
	TokenEOF
)

type Token struct {
	Type  TokenType
	Value string
}

type Parser struct {
	lexer *Lexer
	field string
}

func parse(input string, field string) (bleveQuery.Query, []string, error) {
	lexer := NewLexer(input)
	p := &Parser{
		lexer: lexer,
	}

	var exactMatches []string
	var reusableCurrentMatch strings.Builder
	var currentExactMatch *strings.Builder
	var currentWords []string
	var negated bool
	var parents []bleveQuery.Query
	var parentOps []TokenType       // tracks if parent should be AND or OR
	var lastOp TokenType = TokenAND // track last operator for parentheses

	curr := bleve.NewBooleanQuery()

	for {
		token := p.lexer.NextToken()

		if token.Type == TokenEOF {
			if len(currentWords) > 0 {
				match := bleve.NewMatchQuery(strings.Join(currentWords, " "))
				match.SetOperator(bleveQuery.MatchQueryOperatorAnd)
				match.SetField(field)
				if negated {
					curr.AddMustNot(match)
				} else {
					curr.AddMust(match)
				}
			}
			break
		}

		if token.Type == TokenQuote {
			if currentExactMatch == nil {
				currentExactMatch = &reusableCurrentMatch
			} else {
				exactMatches = append(exactMatches, currentExactMatch.String())
				currentExactMatch.Reset()
				reusableCurrentMatch = *currentExactMatch
				currentExactMatch = nil
			}
			continue
		}

		if currentExactMatch != nil {
			if currentExactMatch.Len() > 0 {
				currentExactMatch.WriteByte(' ')
			}
			currentExactMatch.WriteString(token.Value)
			currentWords = append(currentWords, token.Value)
			continue
		}

		if token.Type == TokenWord {
			currentWords = append(currentWords, token.Value)
			continue
		} else if len(currentWords) > 0 {
			match := bleve.NewMatchQuery(strings.Join(currentWords, " "))
			match.SetOperator(bleveQuery.MatchQueryOperatorAnd)
			match.SetField(field)
			if negated {
				curr.AddMustNot(match)
			} else {
				curr.AddMust(match)
			}
			currentWords = currentWords[:0]
			negated = false
		}

		switch token.Type {
		case TokenLParen:
			// push current query to parents stack with the last operator
			parents = append(parents, curr)
			parentOps = append(parentOps, lastOp)
			// reset lastOp to default for inner parentheses
			lastOp = TokenAND
			// start new boolean query for parentheses content
			curr = bleve.NewBooleanQuery()
			continue
		case TokenRParen:
			// finalize any remaining words
			if len(currentWords) > 0 {
				match := bleve.NewMatchQuery(strings.Join(currentWords, " "))
				match.SetOperator(bleveQuery.MatchQueryOperatorAnd)
				match.SetField(field)
				if negated {
					curr.AddMustNot(match)
				} else {
					curr.AddMust(match)
				}
				currentWords = currentWords[:0]
				negated = false
			}

			// pop parent and merge with current
			if len(parents) > 0 {
				parent := parents[len(parents)-1]
				op := parentOps[len(parentOps)-1]

				// create a new boolean query to combine parent and current
				var combined bleveQuery.Query
				switch op {
				case TokenOR:
					or := bleve.NewDisjunctionQuery()
					or.AddQuery(parent)
					or.AddQuery(curr)
					combined = or
				case TokenAND:
					and := bleve.NewConjunctionQuery()
					and.AddQuery(parent)
					and.AddQuery(curr)
					combined = and
				}

				curr = bleve.NewBooleanQuery()
				curr.AddMust(combined)
				parents = parents[:len(parents)-1]
				parentOps = parentOps[:len(parentOps)-1]
			}
			continue
		}

		next := p.lexer.NextToken()
		following := p.lexer.PeekToken()
		if next.Type == TokenNOT {
			negated = true
		}

		switch token.Type {
		case TokenOR:
			if next.Type != TokenLParen && !(next.Type == TokenNOT && following.Type == TokenLParen) {
				// if this is not followed by a "(" or "NOT (" consider the follow next word as the only parameter
				other := bleve.NewMatchQuery(next.Value)
				other.SetOperator(bleveQuery.MatchQueryOperatorAnd)
				other.SetField(field)
				or := bleve.NewDisjunctionQuery()
				or.AddQuery(curr)
				or.AddQuery(other)
				curr = bleve.NewBooleanQuery()
				curr.AddMust(or)
			} else {
				lastOp = TokenOR
			}
		case TokenAND:
			if next.Type != TokenLParen && !(next.Type == TokenNOT && following.Type == TokenLParen) {
				// if this is not followed by a "(" consider the follow next word as the only parameter
				other := bleve.NewMatchQuery(next.Value)
				other.SetOperator(bleveQuery.MatchQueryOperatorAnd)
				other.SetField(field)
				and := bleve.NewConjunctionQuery()
				and.AddQuery(curr)
				and.AddQuery(other)
				curr = bleve.NewBooleanQuery()
				curr.AddMust(and)
			} else {
				lastOp = TokenAND
			}
		case TokenNOT:
			if next.Type != TokenLParen {
				// if this is not followed by a "(" or "NOT (" consider the follow next word as the only parameter
				other := bleve.NewMatchQuery(next.Value)
				other.SetOperator(bleveQuery.MatchQueryOperatorAnd)
				other.SetField(field)
				curr.AddMustNot(other)
			} else {
				negated = true
			}
		default:
			p.lexer.ReturnToken(next)
		}
	}

	return curr, exactMatches, nil
}
