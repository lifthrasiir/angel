package chat

import (
	"testing"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/llm"
	. "github.com/lifthrasiir/angel/internal/types"
)

func TestApplyCurationRules(t *testing.T) {
	tests := []struct {
		name     string
		input    []FrontendMessage
		expected []FrontendMessage
	}{
		{
			name: "Basic scenario: User -> Model -> User -> Model",
			input: []FrontendMessage{
				{Type: TypeUserText, Parts: []Part{{Text: "User 1"}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
				{Type: TypeUserText, Parts: []Part{{Text: "User 2"}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 2"}}},
			},
			expected: []FrontendMessage{
				{Type: TypeUserText, Parts: []Part{{Text: "User 1"}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
				{Type: TypeUserText, Parts: []Part{{Text: "User 2"}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 2"}}},
			},
		},
		{
			name: "Consecutive user input: User -> User -> Model (first user removed)",
			input: []FrontendMessage{
				{Type: TypeUserText, Parts: []Part{{Text: "User 1 (to be removed)"}}},
				{Type: TypeUserText, Parts: []Part{{Text: "User 2"}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
			},
			expected: []FrontendMessage{
				{Type: TypeUserText, Parts: []Part{{Text: "User 2"}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
			},
		},
		{
			name: "Function call without response: Model(FC) -> Model (FC removed)",
			input: []FrontendMessage{
				{Type: TypeFunctionCall, Parts: []Part{{FunctionCall: &FunctionCall{Name: "tool"}}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
			},
			expected: []FrontendMessage{
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
			},
		},
		{
			name: "Function call with response: Model(FC) -> User(FR) -> Model (all kept)",
			input: []FrontendMessage{
				{Type: TypeFunctionCall, Parts: []Part{{FunctionCall: &FunctionCall{Name: "tool"}}}},
				{Type: TypeFunctionResponse, Parts: []Part{{FunctionResponse: &FunctionResponse{Name: "tool"}}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
			},
			expected: []FrontendMessage{
				{Type: TypeFunctionCall, Parts: []Part{{FunctionCall: &FunctionCall{Name: "tool"}}}},
				{Type: TypeFunctionResponse, Parts: []Part{{FunctionResponse: &FunctionResponse{Name: "tool"}}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
			},
		},
		{
			name: "Consecutive user input with thought in between: User -> Thought -> User -> Model (first user kept)",
			input: []FrontendMessage{
				{Type: TypeUserText, Parts: []Part{{Text: "User 1"}}},
				{Type: TypeThought, Parts: []Part{{Text: "Thinking..."}}},
				{Type: TypeUserText, Parts: []Part{{Text: "User 2"}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
			},
			expected: []FrontendMessage{
				{Type: TypeUserText, Parts: []Part{{Text: "User 1"}}},
				{Type: TypeThought, Parts: []Part{{Text: "Thinking..."}}},
				{Type: TypeUserText, Parts: []Part{{Text: "User 2"}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
			},
		},
		{
			name: "Consecutive user input with error in between: User -> Thought -> User -> Model (first user removed)",
			input: []FrontendMessage{
				{Type: TypeUserText, Parts: []Part{{Text: "User 1 (to be removed)"}}},
				{Type: TypeError, Parts: []Part{{Text: "Canceled by user"}}},
				{Type: TypeUserText, Parts: []Part{{Text: "User 2"}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
			},
			expected: []FrontendMessage{
				{Type: TypeError, Parts: []Part{{Text: "Canceled by user"}}},
				{Type: TypeUserText, Parts: []Part{{Text: "User 2"}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
			},
		},
		{
			name: "Function call without response with thought in between: Model(FC) -> Thought -> Model (FC removed)",
			input: []FrontendMessage{
				{Type: TypeFunctionCall, Parts: []Part{{FunctionCall: &FunctionCall{Name: "tool"}}}},
				{Type: TypeThought, Parts: []Part{{Text: "Thinking..."}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
			},
			expected: []FrontendMessage{
				{Type: TypeThought, Parts: []Part{{Text: "Thinking..."}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
			},
		},
		{
			name: "Mixed scenario: User -> Model -> User(removed) -> User -> Model(FC) -> Thought -> Model(removed) -> Model",
			input: []FrontendMessage{
				{Type: TypeUserText, Parts: []Part{{Text: "User A"}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model A"}}},
				{Type: TypeUserText, Parts: []Part{{Text: "User B (removed)"}}},
				{Type: TypeUserText, Parts: []Part{{Text: "User C"}}},
				{Type: TypeFunctionCall, Parts: []Part{{FunctionCall: &FunctionCall{Name: "tool"}}}},
				{Type: TypeThought, Parts: []Part{{Text: "Thinking..."}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model D"}}}, // This is not a function response
			},
			expected: []FrontendMessage{
				{Type: TypeUserText, Parts: []Part{{Text: "User A"}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model A"}}},
				{Type: TypeUserText, Parts: []Part{{Text: "User C"}}},
				{Type: TypeThought, Parts: []Part{{Text: "Thinking..."}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model D"}}},
			},
		},
		{
			name: "ExecutableCode FunctionCall without response: Model(EC) -> Model (EC removed)",
			input: []FrontendMessage{
				{Type: TypeFunctionCall, Parts: []Part{{FunctionCall: &FunctionCall{Name: llm.GeminiCodeExecutionToolName}}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
			},
			expected: []FrontendMessage{
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
			},
		},
		{
			name: "ExecutableCode FunctionCall with response: Model(EC) -> User(ECR) -> Model (all kept)",
			input: []FrontendMessage{
				{Type: TypeFunctionCall, Parts: []Part{{FunctionCall: &FunctionCall{Name: llm.GeminiCodeExecutionToolName}}}},
				{Type: TypeFunctionResponse, Parts: []Part{{FunctionResponse: &FunctionResponse{Name: llm.GeminiCodeExecutionToolName}}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
			},
			expected: []FrontendMessage{
				{Type: TypeFunctionCall, Parts: []Part{{FunctionCall: &FunctionCall{Name: llm.GeminiCodeExecutionToolName}}}},
				{Type: TypeFunctionResponse, Parts: []Part{{FunctionResponse: &FunctionResponse{Name: llm.GeminiCodeExecutionToolName}}}},
				{Type: TypeModelText, Parts: []Part{{Text: "Model 1"}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			curated := applyCurationRules(tt.input)

			if len(curated) != len(tt.expected) {
				t.Fatalf("Expected length %d, got %d. Curated: %+v, Expected: %+v", len(tt.expected), len(curated), curated, tt.expected)
			}

			for i := range curated {
				// Compare relevant fields for FrontendMessage
				if curated[i].Type != tt.expected[i].Type ||
					(len(curated[i].Parts) > 0 && len(tt.expected[i].Parts) > 0 && curated[i].Parts[0].Text != tt.expected[i].Parts[0].Text) ||
					(len(curated[i].Parts) > 0 && len(tt.expected[i].Parts) > 0 && curated[i].Parts[0].FunctionCall != nil && tt.expected[i].Parts[0].FunctionCall != nil && curated[i].Parts[0].FunctionCall.Name != tt.expected[i].Parts[0].FunctionCall.Name) ||
					(len(curated[i].Parts) > 0 && len(tt.expected[i].Parts) > 0 && curated[i].Parts[0].FunctionResponse != nil && tt.expected[i].Parts[0].FunctionResponse != nil && curated[i].Parts[0].FunctionResponse.Name != tt.expected[i].Parts[0].FunctionResponse.Name) {
					t.Errorf("Mismatch at index %d.\nExpected: %+v\nGot:      %+v", i, tt.expected[i], curated[i])
				}
			}
		})
	}
}

// TestInlineDataFilenameGeneration tests the filename generation function
func TestInlineDataFilenameGeneration(t *testing.T) {
	testCases := []struct {
		mimeType     string
		counter      int
		expectedFile string
	}{
		{"image/png", 1, "generated_image_001.png"},
		{"image/jpeg", 10, "generated_image_010.jpg"},
		{"image/gif", 999, "generated_image_999.gif"},
		{"application/pdf", 42, "generated_document_042.pdf"},
		{"text/plain", 5, "generated_text_005.txt"},
		{"application/json", 100, "generated_data_100.json"},
		{"unknown/type", 7, "generated_file_007"},
	}

	for _, tc := range testCases {
		t.Run(tc.mimeType, func(t *testing.T) {
			result := GenerateFilenameFromMimeType(tc.mimeType, tc.counter)
			if result != tc.expectedFile {
				t.Errorf("generateFilenameFromMimeType(%s, %d) = %s, expected %s",
					tc.mimeType, tc.counter, result, tc.expectedFile)
			}
		})
	}
}
