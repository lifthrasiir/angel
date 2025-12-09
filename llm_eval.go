package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"math/rand"
	"strconv"
	"strings"
	"time"

	. "github.com/lifthrasiir/angel/gemini"
	. "github.com/lifthrasiir/angel/internal/types"
)

// AngelEvalProvider implements the LLMProvider interface for the angel-eval model.
type AngelEvalProvider struct{}

// SendMessageStream processes the Forth-like language and streams responses.
func (p *AngelEvalProvider) SendMessageStream(ctx context.Context, modelName string, params SessionParams) (iter.Seq[GenerateContentResponse], io.Closer, error) {
	// Find the last user message with actual text content and check for saved state
	var input string
	var savedState *EvalState

	// First, find the most recent user text input to determine the search boundary
	lastUserTextIndex := -1
	for i := len(params.Contents) - 1; i >= 0; i-- {
		content := params.Contents[i]
		if content.Role == RoleUser && len(content.Parts) > 0 {
			for _, part := range content.Parts {
				if part.Text != "" && part.FunctionResponse == nil {
					lastUserTextIndex = i
					break
				}
			}
			if lastUserTextIndex >= 0 {
				break
			}
		}
	}

	// Only look for saved state in FunctionCalls AFTER the most recent user text
	for i := len(params.Contents) - 1; i > lastUserTextIndex; i-- {
		content := params.Contents[i]
		if content.Role == RoleModel && len(content.Parts) > 0 {
			for _, part := range content.Parts {
				if part.FunctionCall != nil && part.ThoughtSignature != "" {
					// Decode saved state from ThoughtSignature
					stateData, err := base64.StdEncoding.DecodeString(part.ThoughtSignature)
					if err != nil {
						continue
					}
					var state EvalState
					if json.Unmarshal(stateData, &state) != nil {
						continue
					}
					savedState = &state
					// Look for the FunctionResponse that follows this FunctionCall
					// and push its result to the saved state's stack
					for j := i + 1; j < len(params.Contents); j++ {
						respContent := params.Contents[j]
						if respContent.Role == RoleUser && len(respContent.Parts) > 0 {
							for _, respPart := range respContent.Parts {
								if respPart.FunctionResponse != nil {
									// Found FunctionResponse, push result to stack
									resultJSON, err := json.Marshal(respPart.FunctionResponse.Response)
									if err == nil {
										savedState.Stack = append(savedState.Stack, string(resultJSON))
									}
									break
								}
							}
							break // Found the FunctionResponse, stop searching
						}
					}
					break
				}
			}
		}
		if savedState != nil {
			break
		}
	}

	// If no saved state found, look for user input
	if savedState == nil {
		for i := len(params.Contents) - 1; i >= 0; i-- {
			content := params.Contents[i]
			// Only look at user messages (function responses are added as user messages)
			if content.Role == RoleUser && len(content.Parts) > 0 {
				// Look for parts with actual text content (excluding FunctionResponse)
				for _, part := range content.Parts {
					if part.Text != "" && part.FunctionResponse == nil {
						input = part.Text
						break
					}
				}
				// If we found text from a user message, break the outer loop
				if input != "" {
					break
				}
			}
		}
	}

	if savedState == nil && input == "" {
		return nil, nil, fmt.Errorf("no input provided for angel-eval")
	}

	// Calculate initial prompt token count (fixed)
	initialPromptTokenCountResp, err := p.CountTokens(ctx, modelName, params.Contents)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to count tokens for angel-eval prompt: %w", err)
	}
	initialPromptTokenCount := initialPromptTokenCountResp.TotalTokens

	// Initialize cumulative total token count with the prompt tokens
	cumulativeTotalTokenCount := initialPromptTokenCount

	return func(yield func(GenerateContentResponse) bool) {
		err := parseAndExecute(ctx, input, savedState, func(resp GenerateContentResponse) bool {
			// Calculate tokens for the current response part
			currentResponsePartTokens := 0
			if len(resp.Candidates) > 0 && resp.Candidates[0].Content.Parts != nil {
				// Use the placeholder CountTokens for the current response part
				partTokenResp, err := p.CountTokens(ctx, modelName, []Content{resp.Candidates[0].Content})
				if err == nil {
					currentResponsePartTokens = partTokenResp.TotalTokens
				}
			}
			cumulativeTotalTokenCount += currentResponsePartTokens

			// Attach UsageMetadata to every response
			if resp.UsageMetadata == nil {
				resp.UsageMetadata = &UsageMetadata{}
			}
			resp.UsageMetadata.PromptTokenCount = initialPromptTokenCount
			resp.UsageMetadata.TotalTokenCount = cumulativeTotalTokenCount

			return yield(resp)
		})
		if err != nil {
			yield(GenerateContentResponse{
				Candidates: []Candidate{{
					Content: Content{
						Parts: []Part{
							{Text: fmt.Sprintf("Error: %v", err)},
						},
					},
				}},
			})
		}
	}, io.NopCloser(nil), nil
}

// GenerateContentOneShot is not fully supported for angel-eval, as it's stream-based.
func (p *AngelEvalProvider) GenerateContentOneShot(ctx context.Context, modelName string, params SessionParams) (OneShotResult, error) {
	return OneShotResult{}, fmt.Errorf("GenerateContentOneShot not supported for angel-eval, use SendMessageStream")
}

// CountTokens is a placeholder for angel-eval.
func (p *AngelEvalProvider) CountTokens(ctx context.Context, modelName string, contents []Content) (*CaCountTokenResponse, error) {
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
func (p *AngelEvalProvider) MaxTokens(modelName string) int {
	return 1024 // A reasonable default for a simple eval model
}

// Helper functions for random generation
func generateRandomString(length int) string {
	charset := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[rand.Intn(len(charset))]
	}
	return string(result)
}

func generateRandomNumber(digits int) (int, error) {
	if digits > 9 { // Prevent overflow for int (max 9 digits)
		return 0, fmt.Errorf("too many digits: %d (max 9)", digits)
	}

	min := 1
	for i := 1; i < digits; i++ {
		min *= 10
	}
	if digits == 1 {
		min = 0
	}

	max := 1
	for i := 0; i < digits; i++ {
		max *= 10
	}
	if digits == 1 {
		max = 10
	}

	return min + rand.Intn(max-min), nil
}

// Forth-like language interpreter logic will go here.
// This will involve a stack and functions for each operation.

// EvalState represents the execution state of angel-eval
type EvalState struct {
	ProgramCounter int           `json:"pc"`     // Next token to execute
	Stack          []interface{} `json:"stack"`  // Current stack contents
	Tokens         []string      `json:"tokens"` // All tokens from input
}

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

// parseAndExecute parses the input and executes Forth-like operations with state management.
func parseAndExecute(ctx context.Context, input string, savedState *EvalState, yield func(GenerateContentResponse) bool) error {
	var st stack
	var tokens []string
	var startIndex int

	if savedState != nil {
		// Restore saved state (stack already contains FunctionResponse result)
		st = stack(savedState.Stack)
		tokens = savedState.Tokens
		startIndex = savedState.ProgramCounter
	} else {
		// Fresh execution
		st = make(stack, 0)
		tokens = tokenize(input)
		startIndex = 0
	}

	// Create yieldPart wrapper function
	yieldPart := func(part Part) bool {
		return yield(GenerateContentResponse{
			Candidates: []Candidate{{
				Content: Content{
					Parts: []Part{part},
				},
			}},
		})
	}

	for i := startIndex; i < len(tokens); i++ {
		select {
		case <-ctx.Done(): // Check for context cancellation
			return ctx.Err()
		default:
		}

		token := tokens[i]
		if num, err := strconv.ParseFloat(token, 64); err == nil {
			st.push(num)
		} else if strings.HasPrefix(token, "\"") && strings.HasSuffix(token, "\"") {
			// Handle string literals
			str := strings.Trim(token, "\"")
			str = strings.ReplaceAll(str, "\"\"", "\"") // Unescape double quotes
			st.push(str)
		} else if strings.HasPrefix(token, "`") && strings.HasSuffix(token, "`") {
			// Handle backtick string literals
			str := strings.Trim(token, "`")
			str = strings.ReplaceAll(str, "``", "`") // Unescape backticks
			st.push(str)
		} else if strings.HasPrefix(token, "(") && strings.HasSuffix(token, ")") {
			// Comment, do nothing
		} else {
			// Check for s/ss/sss... commands (random alphanumeric strings)
			if strings.HasPrefix(token, "s") && len(token) >= 1 {
				allS := true
				for _, r := range token {
					if r != 's' {
						allS = false
						break
					}
				}
				if allS {
					length := len(token)
					st.push(generateRandomString(length))
					continue
				}
			}

			// Check for n/nn/nnn... commands (random numbers)
			if strings.HasPrefix(token, "n") && len(token) >= 1 {
				allN := true
				for _, r := range token {
					if r != 'n' {
						allN = false
						break
					}
				}
				if allN {
					digits := len(token)
					num, err := generateRandomNumber(digits)
					if err != nil {
						return err
					}
					st.push(float64(num))
					continue
				}
			}

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
				if !yieldPart(Part{Text: strVal}) {
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

				if !yieldPart(Part{Thought: true, Text: fmt.Sprintf("**%s**\n\n%s", titleStr, contentStr)}) {
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
				select {
				case <-time.After(time.Duration(sleepTime * float64(time.Second))):
					// Sleep completed
				case <-ctx.Done():
					return ctx.Err() // Context was cancelled
				}
			case "call":
				// Pop args JSON string
				argsVal, err := st.pop()
				if err != nil {
					return err
				}
				argsStr, ok := argsVal.(string)
				if !ok {
					return fmt.Errorf("type mismatch: expected string for call args, got %T", argsVal)
				}

				// Pop tool name
				nameVal, err := st.pop()
				if err != nil {
					return err
				}
				nameStr, ok := nameVal.(string)
				if !ok {
					return fmt.Errorf("type mismatch: expected string for call name, got %T", nameVal)
				}

				// Parse args JSON
				var argsMap map[string]interface{}
				if err := json.Unmarshal([]byte(argsStr), &argsMap); err != nil {
					return fmt.Errorf("call: invalid args JSON: %v", err)
				}

				// Create FunctionCall
				fc := FunctionCall{
					Name: nameStr,
					Args: argsMap,
				}

				// Save current state for next execution (continue after this call)
				nextState := &EvalState{
					ProgramCounter: i + 1, // Continue from next token after call
					Stack:          []interface{}(st),
					Tokens:         tokens,
				}

				// Create ThoughtSignature with proper error handling
				stateJSON, err := json.Marshal(nextState)
				if err != nil {
					return err
				}
				thoughtSignature := base64.StdEncoding.EncodeToString(stateJSON)

				// Display tool call as FunctionCall with state
				if !yieldPart(Part{FunctionCall: &fc, ThoughtSignature: thoughtSignature}) {
					return nil // Stop if yield returns false
				}

				// Terminate this execution - chat_stream will call us again with the restored state
				return nil
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
	inBacktick := false
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
		} else if inBacktick {
			if char == '`' {
				if i+1 < len(input) && input[i+1] == '`' {
					// Escaped backtick
					currentToken.WriteByte('`')
					i++ // Skip next backtick
				} else {
					// End of backtick string
					inBacktick = false
					currentToken.WriteByte('`')
					tokens = append(tokens, currentToken.String())
					currentToken.Reset()
				}
			} else {
				currentToken.WriteByte(char)
			}
		} else if inComment {
			if char == ')' {
				inComment = false
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
			case '`':
				if currentToken.Len() > 0 {
					tokens = append(tokens, currentToken.String())
					currentToken.Reset()
				}
				inBacktick = true
				currentToken.WriteByte('`')
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
	if inBacktick {
		// Unclosed backtick string is a syntax error
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
