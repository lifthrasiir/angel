package main

// AuthType enum definition (matches AuthType in TypeScript)
type AuthType string

const (
	AuthTypeLoginWithGoogle AuthType = "oauth-personal"
	AuthTypeUseGemini       AuthType = "gemini-api-key"
	AuthTypeUseVertexAI     AuthType = "vertex-ai"
	AuthTypeCloudShell      AuthType = "cloud-shell"
)

// Define Go structs to match the structs defined in gemini-cli's converter.ts

type InlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type FunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type FunctionResponse struct {
	Name     string      `json:"name"`
	Response interface{} `json:"response"`
}

type FileData struct {
	MimeType string `json:"mimeType"`
	FileUri  string `json:"fileUri"`
}

type ExecutableCode struct {
	Language string `json:"language"`
	Code     string `json:"code"`
}

const (
	LanguagePython = "PYTHON"
)

type CodeExecutionResult struct {
	Outcome string `json:"outcome"`
	Output  string `json:"output,omitempty"`
}

const (
	OutcomeOk               = "OUTCOME_OK"
	OutcomeFailed           = "OUTCOME_FAILED"
	OutcomeDeadlineExceeded = "OUTCOME_DEADLINE_EXCEEDED"
)

type Part struct {
	Text                string               `json:"text,omitempty"`
	Thought             bool                 `json:"thought,omitempty"`
	ThoughtSignature    string               `json:"thoughtSignature,omitempty"`
	InlineData          *InlineData          `json:"inlineData,omitempty"`
	FunctionCall        *FunctionCall        `json:"functionCall,omitempty"`
	FunctionResponse    *FunctionResponse    `json:"functionResponse,omitempty"`
	FileData            *FileData            `json:"fileData,omitempty"`
	ExecutableCode      *ExecutableCode      `json:"executableCode,omitempty"`
	CodeExecutionResult *CodeExecutionResult `json:"codeExecutionResult,omitempty"`
}

type Content struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

type ThinkingConfig struct {
	IncludeThoughts bool `json:"includeThoughts,omitempty"`
}

type Type string

const (
	TypeUnspecified Type = "TYPE_UNSPECIFIED"
	TypeString      Type = "STRING"
	TypeNumber      Type = "NUMBER"
	TypeInteger     Type = "INTEGER"
	TypeBoolean     Type = "BOOLEAN"
	TypeArray       Type = "ARRAY"
	TypeObject      Type = "OBJECT"
	TypeNull        Type = "NULL"
)

type Schema struct {
	AnyOf            []*Schema          `json:"anyOf,omitempty"`
	Default          interface{}        `json:"default,omitempty"`
	Description      string             `json:"description,omitempty"`
	Enum             []string           `json:"enum,omitempty"`
	Example          interface{}        `json:"example,omitempty"`
	Format           string             `json:"format,omitempty"`
	Items            *Schema            `json:"items,omitempty"`
	MaxItems         string             `json:"maxItems,omitempty"`
	MaxLength        string             `json:"maxLength,omitempty"`
	MaxProperties    string             `json:"maxProperties,omitempty"`
	Maximum          *float64           `json:"maximum,omitempty"`
	MinItems         string             `json:"minItems,omitempty"`
	MinLength        string             `json:"minLength,omitempty"`
	MinProperties    string             `json:"minProperties,omitempty"`
	Minimum          *float64           `json:"minimum,omitempty"`
	Nullable         *bool              `json:"nullable,omitempty"`
	Pattern          string             `json:"pattern,omitempty"`
	Properties       map[string]*Schema `json:"properties,omitempty"`
	PropertyOrdering []string           `json:"propertyOrdering,omitempty"`
	Required         []string           `json:"required,omitempty"`
	Title            string             `json:"title,omitempty"`
	Type             Type               `json:"type,omitempty"`
}

type GenerationConfigRoutingConfigAutoRoutingMode string

const (
	GenerationConfigRoutingConfigAutoRoutingModeUnknown           GenerationConfigRoutingConfigAutoRoutingMode = "UNKNOWN"
	GenerationConfigRoutingConfigAutoRoutingModePrioritizeQuality GenerationConfigRoutingConfigAutoRoutingMode = "PRIORITIZE_QUALITY"
	GenerationConfigRoutingConfigAutoRoutingModeBalanced          GenerationConfigRoutingConfigAutoRoutingMode = "BALANCED"
	GenerationConfigRoutingConfigAutoRoutingModePrioritizeCost    GenerationConfigRoutingConfigAutoRoutingMode = "PRIORITIZE_COST"
)

type GenerationConfigRoutingConfigManualRoutingMode struct {
	ModelName string `json:"modelName,omitempty"`
}

type GenerationConfigRoutingConfig struct {
	AutoMode   *GenerationConfigRoutingConfigAutoRoutingMode   `json:"autoMode,omitempty"`
	ManualMode *GenerationConfigRoutingConfigManualRoutingMode `json:"manualMode,omitempty"`
}

type MediaResolution string

const (
	MediaResolutionUnspecified MediaResolution = "MEDIA_RESOLUTION_UNSPECIFIED"
	MediaResolutionLow         MediaResolution = "MEDIA_RESOLUTION_LOW"
	MediaResolutionMedium      MediaResolution = "MEDIA_RESOLUTION_MEDIUM"
	MediaResolutionHigh        MediaResolution = "MEDIA_RESOLUTION_HIGH"
)

type PrebuiltVoiceConfig struct {
	VoiceName string `json:"voiceName,omitempty"`
}

type VoiceConfig struct {
	PrebuiltVoiceConfig *PrebuiltVoiceConfig `json:"prebuiltVoiceConfig,omitempty"`
}

type SpeakerVoiceConfig struct {
	Speaker     string       `json:"speaker,omitempty"`
	VoiceConfig *VoiceConfig `json:"voiceConfig,omitempty"`
}

type MultiSpeakerVoiceConfig struct {
	SpeakerVoiceConfigs []*SpeakerVoiceConfig `json:"speakerVoiceConfigs,omitempty"`
}

type SpeechConfig struct {
	VoiceConfig             *VoiceConfig             `json:"voiceConfig,omitempty"`
	MultiSpeakerVoiceConfig *MultiSpeakerVoiceConfig `json:"multiSpeakerVoiceConfig,omitempty"`
	LanguageCode            string                   `json:"languageCode,omitempty"`
}

type FeatureSelectionPreference string

const (
	FeatureSelectionPreferenceUnspecified FeatureSelectionPreference = "FEATURE_SELECTION_PREFERENCE_UNSPECIFIED"
	PrioritizeQuality                     FeatureSelectionPreference = "PRIORITIZE_QUALITY"
	Balanced                              FeatureSelectionPreference = "BALANCED"
	PrioritizeCost                        FeatureSelectionPreference = "PRIORITIZE_COST"
)

type ModelSelectionConfig struct {
	FeatureSelectionPreference FeatureSelectionPreference `json:"featureSelectionPreference,omitempty"`
}

type GenerationConfig struct {
	ThinkingConfig       *ThinkingConfig                `json:"thinkingConfig,omitempty"`
	Temperature          *float32                       `json:"temperature,omitempty"`
	TopP                 *float32                       `json:"topP,omitempty"`
	TopK                 *int32                         `json:"topK,omitempty"`
	CandidateCount       *int32                         `json:"candidateCount,omitempty"`
	MaxOutputTokens      *int32                         `json:"maxOutputTokens,omitempty"`
	StopSequences        []string                       `json:"stopSequences,omitempty"`
	ResponseLogprobs     *bool                          `json:"responseLogprobs,omitempty"`
	Logprobs             *int32                         `json:"logprobs,omitempty"`
	PresencePenalty      *float32                       `json:"presencePenalty,omitempty"`
	FrequencyPenalty     *float32                       `json:"frequencyPenalty,omitempty"`
	Seed                 *int64                         `json:"seed,omitempty"`
	ResponseMimeType     string                         `json:"responseMimeType,omitempty"`
	ResponseSchema       *Schema                        `json:"responseSchema,omitempty"`
	RoutingConfig        *GenerationConfigRoutingConfig `json:"routingConfig,omitempty"`
	ModelSelectionConfig *ModelSelectionConfig          `json:"modelSelectionConfig,omitempty"`
	ResponseModalities   []string                       `json:"responseModalities,omitempty"`
	MediaResolution      MediaResolution                `json:"mediaResolution,omitempty"`
	SpeechConfig         *SpeechConfig                  `json:"speechConfig,omitempty"`
	AudioTimestamp       *bool                          `json:"audioTimestamp,omitempty"`
}

type Tool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations,omitempty"`
	URLContext           *URLContext           `json:"urlContext,omitempty"`
	CodeExecution        *CodeExecution        `json:"codeExecution,omitempty"`
}

type FunctionDeclaration struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Parameters  *Schema `json:"parameters,omitempty"`
}

type ToolConfig struct {
	FunctionCallingConfig *FunctionCallingConfig `json:"functionCallingConfig,omitempty"`
}

// URLContext is an empty struct for the url_context field in the API request.
type URLContext struct{}

// CodeExecution is an empty struct for the code_execution field in the API request.
type CodeExecution struct{}

type FunctionCallingConfig struct {
	Mode                 string   `json:"mode,omitempty"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

type SafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type VertexGenerateContentRequest struct {
	Contents          []Content         `json:"contents"`
	SystemInstruction *Content          `json:"systemInstruction,omitempty"`
	CachedContent     string            `json:"cachedContent,omitempty"`
	Tools             []Tool            `json:"tools,omitempty"`
	ToolConfig        *ToolConfig       `json:"toolConfig,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	SafetySettings    []SafetySetting   `json:"safetySettings,omitempty"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`
	SessionID         string            `json:"session_id,omitempty"`
}

type CAGenerateContentRequest struct {
	Model   string                       `json:"model"`
	Project string                       `json:"project,omitempty"`
	Request VertexGenerateContentRequest `json:"request"`
}

type PromptFeedback struct {
	BlockReason   string         `json:"blockReason,omitempty"`
	SafetyRatings []SafetyRating `json:"safetyRatings,omitempty"`
}

type SafetyRating struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type UsageMetadata struct {
	PromptTokenCount        int `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount    int `json:"candidatesTokenCount,omitempty"`
	TotalTokenCount         int `json:"totalTokenCount,omitempty"`
	ToolUsePromptTokenCount int `json:"toolUsePromptTokenCount,omitempty"`
	ThoughtsTokenCount      int `json:"thoughtsTokenCount,omitempty"`
}

type Candidate struct {
	Content                         Content             `json:"content"`
	AutomaticFunctionCallingHistory []Content           `json:"automaticFunctionCallingHistory,omitempty"`
	PromptFeedback                  *PromptFeedback     `json:"promptFeedback,omitempty"`
	UsageMetadata                   *UsageMetadata      `json:"usageMetadata,omitempty"`
	URLContextMetadata              *URLContextMetadata `json:"urlContextMetadata,omitempty"`
	GroundingMetadata               *GroundingMetadata  `json:"groundingMetadata,omitempty"`
}

type VertexGenerateContentResponse struct {
	Candidates         []Candidate         `json:"candidates"`
	PromptFeedback     *PromptFeedback     `json:"promptFeedback,omitempty"`
	UsageMetadata      *UsageMetadata      `json:"usageMetadata,omitempty"`
	URLContextMetadata *URLContextMetadata `json:"urlContextMetadata,omitempty"`
	GroundingMetadata  *GroundingMetadata  `json:"groundingMetadata,omitempty"`
}

type CaGenerateContentResponse struct {
	Response VertexGenerateContentResponse `json:"response"`
}

type VertexCountTokenRequest struct {
	Model    string    `json:"model"`
	Contents []Content `json:"contents"`
}

type CaCountTokenRequest struct {
	Request VertexCountTokenRequest `json:"request"`
}

type CaCountTokenResponse struct {
	TotalTokens int `json:"totalTokens"`
}

// URLContextMetadata matches URLContextMetadata in TypeScript
type URLContextMetadata struct {
	URLMetadata []URLMetadata `json:"urlMetadata,omitempty"`
}

// URLMetadata matches URLMetadata in TypeScript
type URLMetadata struct {
	RetrievedURL       string `json:"retrievedUrl,omitempty"`
	URLRetrievalStatus string `json:"urlRetrievalStatus,omitempty"`
}

// GroundingMetadata matches GroundingMetadata in TypeScript
type GroundingMetadata struct {
	GroundingChunks   []GroundingChunkItem   `json:"groundingChunks,omitempty"`
	GroundingSupports []GroundingSupportItem `json:"groundingSupports,omitempty"`
}

// GroundingChunkItem matches GroundingChunkItem in TypeScript
type GroundingChunkItem struct {
	Web *GroundingChunkWeb `json:"web,omitempty"`
}

// GroundingChunkWeb matches GroundingChunkWeb in TypeScript
type GroundingChunkWeb struct {
	URI   string `json:"uri,omitempty"`
	Title string `json:"title,omitempty"`
}

// GroundingSupportItem matches GroundingSupportItem in TypeScript
type GroundingSupportItem struct {
	Segment               *GroundingSupportSegment `json:"segment,omitempty"`
	GroundingChunkIndices []int                    `json:"groundingChunkIndices,omitempty"`
}

// GroundingSupportSegment matches GroundingSupportSegment in TypeScript
type GroundingSupportSegment struct {
	StartIndex int    `json:"startIndex,omitempty"`
	EndIndex   int    `json:"endIndex,omitempty"`
	Text       string `json:"text,omitempty"`
}

// Define ChatSession struct (used instead of genai.ChatSession)

// GeminiEventType enum definition (matches GeminiEventType in TypeScript)
type GeminiEventType string

const (
	GeminiEventTypeContent              GeminiEventType = "content"
	GeminiEventTypeToolCode             GeminiEventType = "tool_code"
	GeminiEventTypeToolCallConfirmation GeminiEventType = "tool_call_confirmation"
	GeminiEventTypeToolCallResponse     GeminiEventType = "tool_call_response"
	GeminiTypeError                     GeminiEventType = "error"
	GeminiTypeFinished                  GeminiEventType = "finished"
	GeminiEventTypeThought              GeminiEventType = "thought"
)

// ServerGeminiContentEvent matches ServerGeminiContentEvent in TypeScript
type ServerGeminiContentEvent struct {
	Type  GeminiEventType `json:"type"`
	Value Content         `json:"value"`
}

// ThoughtSummary matches ThoughtSummary in TypeScript
type ThoughtSummary struct {
	Subject     string `json:"subject"`
	Description string `json:"description"`
}

// ServerGeminiThoughtEvent matches ServerGeminiThoughtEvent in TypeScript
type ServerGeminiThoughtEvent struct {
	Type  GeminiEventType `json:"type"`
	Value ThoughtSummary  `json:"value"`
}

// ServerGeminiFinishedEvent matches ServerGeminiFinishedEvent in TypeScript
type ServerGeminiFinishedEvent struct {
	Type GeminiEventType `json:"type"`
}

// ServerGeminiErrorEvent matches ServerGeminiErrorEvent in TypeScript
type ServerGeminiErrorEvent struct {
	Type  GeminiEventType `json:"type"`
	Value struct {
		Message string `json:"message"`
	} `json:"value"`
}

type ClientMetadata struct {
	IdeType       string `json:"ideType,omitempty"`
	IdeVersion    string `json:"ideVersion,omitempty"`
	PluginVersion string `json:"pluginVersion,omitempty"`
	Platform      string `json:"platform,omitempty"`
	UpdateChannel string `json:"updateChannel,omitempty"`
	DuetProject   string `json:"duetProject,omitempty"`
	PluginType    string `json:"pluginType,omitempty"`
	IdeName       string `json:"ideName,omitempty"`
}

type LoadCodeAssistRequest struct {
	CloudaicompanionProject string          `json:"cloudaicompanionProject,omitempty"`
	Metadata                *ClientMetadata `json:"metadata,omitempty"`
}

type LoadCodeAssistResponse struct {
	CurrentTier             *GeminiUserTier   `json:"currentTier,omitempty"`
	AllowedTiers            []*GeminiUserTier `json:"allowedTiers,omitempty"`
	IneligibleTiers         []*IneligibleTier `json:"ineligibleTiers,omitempty"`
	CloudaicompanionProject string            `json:"cloudaicompanionProject,omitempty"`
}

type GeminiUserTier struct {
	ID                                 UserTierID     `json:"id"`
	Name                               string         `json:"name"`
	Description                        string         `json:"description"`
	UserDefinedCloudaicompanionProject *bool          `json:"userDefinedCloudaicompanionProject,omitempty"`
	IsDefault                          *bool          `json:"isDefault,omitempty"`
	PrivacyNotice                      *PrivacyNotice `json:"privacyNotice,omitempty"`
	HasAcceptedTos                     *bool          `json:"hasAcceptedTos,omitempty"`
	HasOnboardedPreviously             *bool          `json:"hasOnboardedPreviously,omitempty"`
}

type UserTierID string

const (
	UserTierIDFree     UserTierID = "free-tier"
	UserTierIDLegacy   UserTierID = "legacy-tier"
	UserTierIDStandard UserTierID = "standard-tier"
)

type IneligibleTier struct {
	ReasonCode    string     `json:"reasonCode"`
	ReasonMessage string     `json:"reasonMessage"`
	TierID        UserTierID `json:"tierId"`
	TierName      string     `json:"tierName"`
}

type PrivacyNotice struct {
	ShowNotice bool   `json:"showNotice"`
	NoticeText string `json:"noticeText,omitempty"`
}

type OnboardUserRequest struct {
	TierID                  *UserTierID     `json:"tierId,omitempty"`
	CloudaicompanionProject string          `json:"cloudaicompanionProject,omitempty"`
	Metadata                *ClientMetadata `json:"metadata,omitempty"`
}

type LongRunningOperationResponse struct {
	Name     string               `json:"name"`
	Done     *bool                `json:"done,omitempty"`
	Response *OnboardUserResponse `json:"response,omitempty"`
}

type OnboardUserResponse struct {
	CloudaicompanionProject *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"cloudaicompanionProject,omitempty"`
}

type SetCodeAssistGlobalUserSettingRequest struct {
	CloudaicompanionProject     string `json:"cloudaicompanionProject,omitempty"`
	FreeTierDataCollectionOptin bool   `json:"freeTierDataCollectionOptin"`
}

type CodeAssistGlobalUserSettingResponse struct {
	CloudaicompanionProject     string `json:"cloudaicompanionProject,omitempty"`
	FreeTierDataCollectionOptin bool   `json:"freeTierDataCollectionOptin"`
}
