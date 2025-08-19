package main

import (
	"context"
	"fmt"
	"io"
	"iter"
	"strconv"
	"strings"
	"time"
)

// AngelEvalProvider implements the LLMProvider interface for the angel-eval model.
type AngelEvalProvider struct{}

// SendMessageStream processes the Forth-like language and streams responses.
func (p *AngelEvalProvider) SendMessageStream(ctx context.Context, params SessionParams) (iter.Seq[CaGenerateContentResponse], io.Closer, error) {
	// The input will be the last message from the user.
	var input string
	if len(params.Contents) > 0 && len(params.Contents[len(params.Contents)-1].Parts) > 0 {
		// Directly access the Text field of the Part struct
		input = params.Contents[len(params.Contents)-1].Parts[0].Text
	}

	if input == "" {
		return nil, nil, fmt.Errorf("no input provided for angel-eval")
	}

	return func(yield func(CaGenerateContentResponse) bool) {
		err := parseAndExecute(input, yield)
		if err != nil {
			yield(CaGenerateContentResponse{
				Response: VertexGenerateContentResponse{
					Candidates: []Candidate{{
						Content: Content{
							Parts: []Part{
								{Text: fmt.Sprintf("Error: %v", err)},
							},
						},
					}},
				},
			})
		}
	}, io.NopCloser(nil), nil
}

// GenerateContentOneShot is not fully supported for angel-eval, as it's stream-based.
func (p *AngelEvalProvider) GenerateContentOneShot(ctx context.Context, params SessionParams) (OneShotResult, error) {
	return OneShotResult{}, fmt.Errorf("GenerateContentOneShot not supported for angel-eval, use SendMessageStream")
}

// CountTokens is a placeholder for angel-eval.
func (p *AngelEvalProvider) CountTokens(ctx context.Context, contents []Content, modelName string) (*CaCountTokenResponse, error) {
	totalTokens := 0
	for _, content := range contents {
		for _, part := range content.Parts {
			// Directly access the Text field of the Part struct
			totalTokens += len(part.Text) // Use len() for UTF-8 byte length
		}
	}
	return &CaCountTokenResponse{TotalTokens: totalTokens}, nil
}

// MaxTokens is a placeholder for angel-eval.
func (p *AngelEvalProvider) MaxTokens() int {
	return 1024 // A reasonable default for a simple eval model
}

// RelativeDisplayOrder implements the LLMProvider interface for AngelEvalProvider.
func (p *AngelEvalProvider) RelativeDisplayOrder() int {
	return -100
}

func (p *AngelEvalProvider) DefaultGenerationParams() SessionGenerationParams {
	return SessionGenerationParams{}
}

// Forth-like language interpreter logic will go here.
// This will involve a stack and functions for each operation.

// Stack for the Forth-like language
type stack []interface{}

func (s *stack) push(v interface{}) {
	*s = append(*s, v)
}

func (s *stack) pop() (interface{}, error) {
	if len(*s) == 0 {
		return nil, fmt.Errorf("stack underflow")
	}
	v := (*s)[len(*s)-1]
	*s = (*s)[:len(*s)-1]
	return v, nil
}

// parseAndExecute parses the input and executes Forth-like operations.
func parseAndExecute(input string, yield func(CaGenerateContentResponse) bool) error {
	st := make(stack, 0)
	tokens := tokenize(input)

	for _, token := range tokens {
		select {
		case <-context.Background().Done(): // Check for context cancellation
			return context.Background().Err()
		default:
		}

		if num, err := strconv.ParseFloat(token, 64); err == nil {
			st.push(num)
		} else if strings.HasPrefix(token, "\"") && strings.HasSuffix(token, "\"") {
			// Handle string literals
			str := strings.Trim(token, "\"")
			str = strings.ReplaceAll(str, "\"\"", "\"") // Unescape double quotes
			st.push(str)
		} else if strings.HasPrefix(token, "(") && strings.HasSuffix(token, ")") {
			// Comment, do nothing
		} else {
			switch token {
			case "say":
				val, err := st.pop()
				if err != nil {
					return err
				}
				strVal, ok := val.(string)
				if !ok {
					return fmt.Errorf("type mismatch: expected string for 'say', got %T", val)
				}
				if !yield(CaGenerateContentResponse{
					Response: VertexGenerateContentResponse{
						Candidates: []Candidate{{
							Content: Content{
								Parts: []Part{
									{Text: strVal},
								},
							},
						}},
					},
				}) {
					return nil // Stop if yield returns false
				}
			case "think":
				contentVal, err := st.pop()
				if err != nil {
					return err
				}
				titleVal, err := st.pop()
				if err != nil {
					return err
				}

				contentStr, ok := contentVal.(string)
				if !ok {
					return fmt.Errorf("type mismatch: expected string for 'think' content, got %T", contentVal)
				}
				titleStr, ok := titleVal.(string)
				if !ok {
					return fmt.Errorf("type mismatch: expected string for 'think' title, got %T", titleVal)
				}

				if !yield(CaGenerateContentResponse{
					Response: VertexGenerateContentResponse{
						Candidates: []Candidate{{
							Content: Content{
								Parts: []Part{
									{Thought: true, Text: fmt.Sprintf("**%s**\n\n%s", titleStr, contentStr)},
								},
							},
						}},
					},
				}) {
					return nil // Stop if yield returns false
				}
			case "sleep":
				val, err := st.pop()
				if err != nil {
					return err
				}
				sleepTime, ok := val.(float64)
				if !ok {
					return fmt.Errorf("type mismatch: expected number for 'sleep', got %T", val)
				}
				time.Sleep(time.Duration(sleepTime * float64(time.Second)))
			default:
				return fmt.Errorf("unknown operation: %s", token)
			}
		}
	}
	return nil
}

// tokenize splits the input string into tokens, handling quoted strings and comments.
func tokenize(input string) []string {
	var tokens []string
	var currentToken strings.Builder
	inString := false
	inComment := false

	for i := 0; i < len(input); i++ {
		char := input[i]

		if inString {
			if char == '"' {
				if i+1 < len(input) && input[i+1] == '"' {
					// Escaped double quote
					currentToken.WriteByte('"')
					i++ // Skip next quote
				} else {
					// End of string
					inString = false
					currentToken.WriteByte('"')
					tokens = append(tokens, currentToken.String())
					currentToken.Reset()
				}
			} else {
				currentToken.WriteByte(char)
			}
		} else if inComment {
			if char == ')' {
				inComment = false
				// tokens = append(tokens, "("+currentToken.String()+")") // Keep comment as a single token
				currentToken.Reset() // Discard comment content
			} else {
				currentToken.WriteByte(char)
			}
		} else {
			switch char {
			case '"':
				if currentToken.Len() > 0 {
					tokens = append(tokens, currentToken.String())
					currentToken.Reset()
				}
				inString = true
				currentToken.WriteByte('"')
			case '(': // Start of comment
				if currentToken.Len() > 0 {
					tokens = append(tokens, currentToken.String())
					currentToken.Reset()
				}
				inComment = true
				// currentToken.WriteByte('(') // Don't include '(' in comment token
			case ' ', '\t', '\n', '\r':
				if currentToken.Len() > 0 {
					tokens = append(tokens, currentToken.String())
					currentToken.Reset()
				}
			default:
				currentToken.WriteByte(char)
			}
		}
	}

	if inString {
		// Unclosed string literal is a syntax error
		return []string{} // Or return an error
	}
	if inComment {
		// Unclosed comment is a syntax error
		return []string{} // Or return an error
	}

	if currentToken.Len() > 0 {
		tokens = append(tokens, currentToken.String())
	}

	return tokens
}
