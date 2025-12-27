package chat

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/database"
	"github.com/lifthrasiir/angel/internal/env"
	"github.com/lifthrasiir/angel/internal/llm"
	"github.com/lifthrasiir/angel/internal/prompts"
	"github.com/lifthrasiir/angel/internal/tool"
	. "github.com/lifthrasiir/angel/internal/types"
)

func AppendAttachmentParts(db *database.Database, toolResults tool.HandlerResults, partsForContent []Part) []Part {
	for _, attachment := range toolResults.Attachments {
		// Retrieve blob data from DB using hash
		blobData, err := database.GetBlob(db, attachment.Hash)
		if err != nil {
			log.Printf("Failed to retrieve blob data for hash %s: %v", attachment.Hash, err)
			continue // Skip this attachment
		}

		// Add inlineData part with Base64 encoded blob data
		partsForContent = append(partsForContent, Part{
			InlineData: &InlineData{
				MimeType: attachment.MimeType,
				Data:     base64.StdEncoding.EncodeToString(blobData),
			},
		})
	}
	return partsForContent
}

// Helper function to convert FrontendMessage to Content for LLM
func ConvertFrontendMessagesToContent(db *database.Database, frontendMessages []FrontendMessage) []Content {
	var contents []Content
	// Apply curation rules before converting to Content
	curatedMessages := applyCurationRules(frontendMessages)

	for _, fm := range curatedMessages {
		var parts []Part

		if fm.Type == TypeCommand {
			continue // Command messages are only visible to users
		}

		// Add text part if present
		if len(fm.Parts) > 0 && fm.Parts[0].Text != "" {
			parts = append(parts, Part{
				Text:             fm.Parts[0].Text,
				ThoughtSignature: fm.Parts[0].ThoughtSignature,
			})
		}

		// Add attachments as InlineData with preceding hash information
		hasBinaryAttachments := false
		for _, att := range fm.Attachments {
			if att.Hash != "" { // Only process if hash exists
				if att.Omitted {
					// Attachment was omitted due to clearblobs command
					parts = append(parts,
						Part{Text: fmt.Sprintf("[Binary with hash %s is currently **UNPROCESSED**. You **MUST** use recall(query='%[1]s') to gain access to its content for internal analysis. **Until recalled, you have NO information about this binary's content, and any attempt to describe or act upon it will be pure guesswork.**]", att.Hash)},
					)
				} else {
					// Normal blob processing
					blobData, err := database.GetBlob(db, att.Hash)
					if err != nil {
						log.Printf("Error retrieving blob data for hash %s: %v", att.Hash, err)
						// Decide how to handle this error: skip attachment, return error, etc.
						// For now, we'll skip this attachment to avoid breaking the whole message.
						continue
					}
					hasBinaryAttachments = true
					parts = append(parts,
						Part{Text: fmt.Sprintf("[Binary with hash %s follows:]", att.Hash)},
						Part{
							InlineData: &InlineData{
								MimeType: att.MimeType,
								Data:     base64.StdEncoding.EncodeToString(blobData),
							},
						},
					)
				}
			}
		}

		// Add warning message after all binary attachments have been displayed
		if hasBinaryAttachments {
			parts = append(parts, Part{Text: "[IMPORTANT: The hashes shown above are explicitly for SHA-512/256 hash-accepting tools only and must never be exposed to users without explicit request.]"})
		}

		// Handle function calls and responses (these should override text/attachments for their specific message types)
		if fm.Type == TypeFunctionCall && len(fm.Parts) > 0 && fm.Parts[0].FunctionCall != nil {
			fc := fm.Parts[0].FunctionCall
			if fc.Name == llm.GeminiCodeExecutionToolName {
				var ec ExecutableCode
				// fc.Args is map[string]interface{}, need to marshal then unmarshal
				argsBytes, err := json.Marshal(fc.Args)
				if err != nil {
					log.Printf("Error marshaling FunctionCall args to JSON for ExecutableCode: %v", err)
					parts = append(parts, Part{FunctionCall: fc, ThoughtSignature: fm.Parts[0].ThoughtSignature}) // Fallback
				} else if err := json.Unmarshal(argsBytes, &ec); err != nil {
					log.Printf("Error unmarshaling ExecutableCode from FunctionCall args: %v", err)
					parts = append(parts, Part{FunctionCall: fc, ThoughtSignature: fm.Parts[0].ThoughtSignature}) // Fallback
				} else {
					parts = append(parts, Part{ExecutableCode: &ec, ThoughtSignature: fm.Parts[0].ThoughtSignature})
				}
			} else {
				parts = append(parts, Part{FunctionCall: fc, ThoughtSignature: fm.Parts[0].ThoughtSignature})
			}
		} else if fm.Type == TypeFunctionResponse && len(fm.Parts) > 0 && fm.Parts[0].FunctionResponse != nil {
			fr := fm.Parts[0].FunctionResponse
			if fr.Name == llm.GeminiCodeExecutionToolName {
				var cer CodeExecutionResult
				// fr.Response is interface{}, need to marshal then unmarshal
				responseBytes, err := json.Marshal(fr.Response)
				if err != nil {
					log.Printf("Error marshaling FunctionResponse.Response to JSON for CodeExecutionResult: %v", err)
					parts = append(parts, Part{FunctionResponse: fr, ThoughtSignature: fm.Parts[0].ThoughtSignature}) // Fallback
				} else if err := json.Unmarshal(responseBytes, &cer); err != nil {
					log.Printf("Error unmarshaling CodeExecutionResult from FunctionResponse.Response: %v", err)
					parts = append(parts, Part{FunctionResponse: fr, ThoughtSignature: fm.Parts[0].ThoughtSignature}) // Fallback
				} else {
					parts = append(parts, Part{CodeExecutionResult: &cer, ThoughtSignature: fm.Parts[0].ThoughtSignature})
				}
			} else {
				parts = append(parts, Part{FunctionResponse: fr, ThoughtSignature: fm.Parts[0].ThoughtSignature})
			}
		} else if (fm.Type == TypeSystemPrompt || fm.Type == TypeEnvChanged) && len(fm.Parts) > 0 && fm.Parts[0].Text != "" {
			// System_prompt should expand to *two* `Content`s
			prompt := fm.Parts[0].Text
			if fm.Type == TypeEnvChanged {
				var envChanged env.EnvChanged
				err := json.Unmarshal([]byte(prompt), &envChanged)
				if err != nil {
					log.Printf("Error unmarshalling envChanged JSON: %v", err)
				} else {
					prompt = prompts.GetEnvChangeContext(envChanged)
				}
			}
			contents = append(contents,
				Content{
					Role: RoleModel,
					Parts: []Part{{
						FunctionCall: &FunctionCall{
							Name: "new_system_prompt",
							Args: map[string]interface{}{},
						},
						ThoughtSignature: fm.Parts[0].ThoughtSignature,
					}},
				},
				Content{
					Role: RoleUser,
					Parts: []Part{{
						FunctionResponse: &FunctionResponse{
							Name:     "new_system_prompt",
							Response: map[string]interface{}{"prompt": prompt},
						},
					}},
				},
			)
			continue
		}

		// If parts is still empty, add an empty text part to satisfy Gemini API requirements
		if len(parts) == 0 {
			parts = append(parts, Part{Text: ""})
		}

		contents = append(contents, Content{
			Role:  fm.Type.Role(),
			Parts: parts,
		})
	}
	return contents
}

// applyCurationRules applies the specified curation rules to a slice of FrontendMessage.
func applyCurationRules(messages []FrontendMessage) []FrontendMessage {
	var curated []FrontendMessage
	for i := 0; i < len(messages); i++ {
		currentMsg := messages[i]

		// Rule 1: Remove consecutive user text messages
		// If current is user text and next is user text (ignoring errors in between)
		if currentMsg.Type == TypeUserText {
			nextUserTextIndex := -1
			for j := i + 1; j < len(messages); j++ {
				if messages[j].Type == TypeError || messages[j].Type == TypeModelError {
					continue // Ignore errors for continuity
				}
				if messages[j].Type == TypeUserText {
					nextUserTextIndex = j
					break
				}
				// If we find any other type of message, it breaks the "consecutive user text" chain
				break
			}
			if nextUserTextIndex != -1 {
				// This 'currentMsg' is followed by another user text message, so skip it.
				continue
			}
		}

		// Rule 2: Remove function_call if not followed by function_response
		// If current is model function_call
		if currentMsg.Type == TypeFunctionCall {
			foundResponse := false
			for j := i + 1; j < len(messages); j++ {
				if messages[j].Type == TypeThought {
					continue // Ignore thoughts and errors for continuity
				}
				if messages[j].Type == TypeFunctionResponse {
					foundResponse = true
					break
				}
				// If we find any other type of message, it means no immediate function response
				break
			}
			if !foundResponse {
				// This 'currentMsg' (function_call) is not followed by a function_response, so skip it.
				continue
			}
		}

		curated = append(curated, currentMsg)
	}
	return curated
}

// GenerateFilenameFromMimeType generates a filename based on MIME type with sequential numbering
func GenerateFilenameFromMimeType(mimeType string, counter int) string {
	var extension, prefix string

	switch mimeType {
	case "image/png":
		extension = ".png"
		prefix = "generated_image"
	case "image/jpeg":
		extension = ".jpg"
		prefix = "generated_image"
	case "image/gif":
		extension = ".gif"
		prefix = "generated_image"
	case "image/webp":
		extension = ".webp"
		prefix = "generated_image"
	case "image/svg+xml":
		extension = ".svg"
		prefix = "generated_image"
	case "audio/mpeg":
		extension = ".mp3"
		prefix = "generated_audio"
	case "audio/wav":
		extension = ".wav"
		prefix = "generated_audio"
	case "audio/ogg":
		extension = ".ogg"
		prefix = "generated_audio"
	case "video/mp4":
		extension = ".mp4"
		prefix = "generated_video"
	case "video/webm":
		extension = ".webm"
		prefix = "generated_video"
	case "application/pdf":
		extension = ".pdf"
		prefix = "generated_document"
	case "text/plain":
		extension = ".txt"
		prefix = "generated_text"
	case "text/markdown":
		extension = ".md"
		prefix = "generated_text"
	case "application/json":
		extension = ".json"
		prefix = "generated_data"
	case "text/csv":
		extension = ".csv"
		prefix = "generated_data"
	default:
		// For unknown MIME types, generate a generic filename
		extension = ""
		prefix = "generated_file"
	}

	return fmt.Sprintf("%s_%03d%s", prefix, counter, extension)
}
