package gemini

import (
	"fmt"
)

// APIError represents an error from an API response.
type APIError struct {
	StatusCode int
	Message    string
	Response   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API response error: %d %s, response: %s", e.StatusCode, e.Message, e.Response)
}

// FinishReasonMessage creates an appropriate error message based on the finish reason.
func FinishReasonMessage(finishReason string) string {
	switch finishReason {
	case "":
		return "Generation stopped without a finish reason"
	case FinishReasonStop:
		return "Generation completed normally" // This should not be treated as error
	case FinishReasonMaxTokens:
		return "Response was truncated because it exceeded the maximum token limit"
	case FinishReasonSafety:
		return "Response was blocked due to safety concerns"
	case FinishReasonRecitation:
		return "Response was blocked due to potential recitation of copyrighted material"
	case FinishReasonLanguage:
		return "Response was blocked due to unsupported language"
	case FinishReasonOther:
		return "Generation stopped for an unspecified reason"
	case FinishReasonBlocklist:
		return "Response was blocked due to blocked content"
	case FinishReasonProhibitedContent:
		return "Response was blocked due to prohibited content"
	case FinishReasonSpii:
		return "Response was blocked due to sensitive personal information"
	case FinishReasonMalformedFunctionCall:
		return "Response was blocked due to malformed function call"
	case FinishReasonImageSafety:
		return "Response was blocked due to image safety concerns"
	case FinishReasonImageProhibitedContent:
		return "Response was blocked due to prohibited image content"
	case FinishReasonImageOther:
		return "Image generation stopped for an unspecified reason"
	case FinishReasonNoImage:
		return "No image was generated when one was expected"
	case FinishReasonImageRecitation:
		return "Image was blocked due to potential recitation of copyrighted material"
	case FinishReasonUnexpectedToolCall:
		return "Response was blocked due to unexpected tool call"
	case FinishReasonTooManyToolCalls:
		return "Response was blocked due to too many tool calls"
	case FinishReasonModelArmor:
		return "Response was blocked by Model Armor protection"
	default:
		return fmt.Sprintf("Generation stopped with reason: %s", finishReason)
	}
}
