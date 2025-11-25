package gemini

type InlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`

	// Only in Vertex API:
	DisplayName string `json:"displayName,omitempty"`
}

type FunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`

	// Only in Gemini API:
	Id string `json:"id,omitempty"`
}

type FunctionResponse struct {
	Name     string                 `json:"name"`
	Response interface{}            `json:"response"`
	Parts    []FunctionResponsePart `json:"parts,omitempty"`

	// Only in Gemini API:
	Id           string `json:"id,omitempty"`
	WillContinue bool   `json:"willContinue,omitempty"`
	Scheduling   string `json:"scheduling,omitempty"`
}

const (
	SchedulingSilent    = "SILENT"
	SchedulingWhenIdle  = "WHEN_IDLE"
	SchedulingInterrupt = "INTERRUPT"
)

type FunctionResponsePart struct {
	InlineData *FunctionResponseInlineData `json:"inlineData,omitempty"`

	// Only in Vertex API:
	FileData *FunctionResponseFileData `json:"fileData,omitempty"`
}

type FunctionResponseInlineData = InlineData
type FunctionResponseFileData = FileData

type FileData struct {
	MimeType string `json:"mimeType"`
	FileUri  string `json:"fileUri"`

	// Only in Vertex API:
	DisplayName string `json:"displayName,omitempty"`
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

type VideoMetadata struct {
	StartOffset string  `json:"startOffset,omitempty"`
	EndOffset   string  `json:"endOffset,omitempty"`
	Fps         float32 `json:"fps,omitempty"`
}

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
	VideoMetadata       *VideoMetadata       `json:"videoMetadata,omitempty"`
	MediaResolution     string               `json:"mediaResolution,omitempty"`

	// Only in Gemini API:
	PartMetadata map[string]interface{} `json:"partMetadata,omitempty"`
}

const PlaceholderThoughtSignature = "context_engineering_is_the_way_to_go"

type Content struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

type ThinkingConfig struct {
	IncludeThoughts bool   `json:"includeThoughts,omitempty"`
	ThinkingBudget  int    `json:"thinkingBudget,omitempty"` // Gemini 2.5
	ThinkingLevel   string `json:"thinkingLevel,omitempty"`  // Gemini 3
}

const (
	ThinkingLevelLow    = "low"
	ThinkingLevelMedium = "medium"
	ThinkingLevelHigh   = "high"
)

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

type AutoRoutingMode struct {
	ModelRoutingPreference string `json:"mode,omitempty"`
}

const (
	AutoRoutingPreferenceUnknown = "UNKNOWN"
	AutoRoutingPrioritizeQuality = "PRIORITIZE_QUALITY"
	AutoRoutingBalanced          = "BALANCED"
	AutoRoutingPrioritizeCost    = "PRIORITIZE_COST"
)

type ManualRoutingMode struct {
	ModelName string `json:"modelName,omitempty"`
}

type RoutingConfig struct {
	AutoMode   *AutoRoutingMode   `json:"autoMode,omitempty"`
	ManualMode *ManualRoutingMode `json:"manualMode,omitempty"`
}

const (
	MediaResolutionUnspecified = "MEDIA_RESOLUTION_UNSPECIFIED"
	MediaResolutionLow         = "MEDIA_RESOLUTION_LOW"
	MediaResolutionMedium      = "MEDIA_RESOLUTION_MEDIUM"
	MediaResolutionHigh        = "MEDIA_RESOLUTION_HIGH"
)

type ImageConfig struct {
	AspectRatio string `json:"aspectRatio,omitempty"`

	// Only in Vertex API:
	ImageOutputOptions *ImageOutputOptions `json:"imageOutputOptions,omitempty"`
	PersonGeneration   string              `json:"personGeneration,omitempty"`
}

type ImageOutputOptions struct {
	MimeType           string `json:"mimeType,omitempty"`
	CompressionQuality int    `json:"compressionQuality,omitempty"`
}

const (
	PersonGenerationUnspecified = "PERSON_GENERATION_UNSPECIFIED"
	PersonGenerationAllowAll    = "ALLOW_ALL"
	PersonGenerationAllowAdult  = "ALLOW_ADULT"
	PersonGenerationAllowNone   = "ALLOW_NONE"
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
	VoiceConfig             VoiceConfig              `json:"voiceConfig"`
	MultiSpeakerVoiceConfig *MultiSpeakerVoiceConfig `json:"multiSpeakerVoiceConfig,omitempty"`
	LanguageCode            string                   `json:"languageCode,omitempty"`
}

const (
	FeatureSelectionPreferenceUnspecified = "FEATURE_SELECTION_PREFERENCE_UNSPECIFIED"
	FeatureSelectionPrioritizeQuality     = "PRIORITIZE_QUALITY"
	FeatureSelectionBalanced              = "BALANCED"
	FeatureSelectionPrioritizeCost        = "PRIORITIZE_COST"
)

type ModelSelectionConfig struct {
	FeatureSelectionPreference string `json:"featureSelectionPreference,omitempty"`
}

type GenerationConfig struct {
	ThinkingConfig       *ThinkingConfig       `json:"thinkingConfig,omitempty"`
	Temperature          *float32              `json:"temperature,omitempty"`
	TopP                 *float32              `json:"topP,omitempty"`
	TopK                 *int32                `json:"topK,omitempty"`
	CandidateCount       *int32                `json:"candidateCount,omitempty"`
	MaxOutputTokens      *int32                `json:"maxOutputTokens,omitempty"`
	StopSequences        []string              `json:"stopSequences,omitempty"`
	ResponseLogprobs     *bool                 `json:"responseLogprobs,omitempty"`
	Logprobs             *int32                `json:"logprobs,omitempty"`
	PresencePenalty      *float32              `json:"presencePenalty,omitempty"`
	FrequencyPenalty     *float32              `json:"frequencyPenalty,omitempty"`
	Seed                 *int64                `json:"seed,omitempty"`
	ResponseMimeType     string                `json:"responseMimeType,omitempty"`
	ResponseSchema       *Schema               `json:"responseSchema,omitempty"`
	ResponseJsonSchema   interface{}           `json:"responseJsonSchema,omitempty"`
	ModelSelectionConfig *ModelSelectionConfig `json:"modelSelectionConfig,omitempty"`
	ResponseModalities   []string              `json:"responseModalities,omitempty"`
	MediaResolution      string                `json:"mediaResolution,omitempty"`
	SpeechConfig         *SpeechConfig         `json:"speechConfig,omitempty"`
	ImageConfig          *ImageConfig          `json:"imageConfig,omitempty"`

	// Only in Gemini API:
	EnableEnhancedCivicAnswers *bool `json:"enableEnhancedCivicAnswers,omitempty"`

	// Only in Vertex API:
	RoutingConfig         *RoutingConfig `json:"routingConfig,omitempty"`
	AudioTimestamp        *bool          `json:"audioTimestamp,omitempty"`
	EnableAffectiveDialog *bool          `json:"enableAffectiveDialog,omitempty"`
}

type Tool struct {
	FunctionDeclarations  []FunctionDeclaration  `json:"functionDeclarations,omitempty"`
	GoogleSearchRetrieval *GoogleSearchRetrieval `json:"googleSearchRetrieval,omitempty"`
	CodeExecution         *CodeExecution         `json:"codeExecution,omitempty"`
	GoogleSearch          *GoogleSearch          `json:"googleSearch,omitempty"`
	ComputerUse           *ComputerUse           `json:"computerUse,omitempty"`
	URLContext            *URLContext            `json:"urlContext,omitempty"`
	GoogleMaps            *GoogleMaps            `json:"googleMaps,omitempty"`

	// Only in Vertex API:
	Retrieval           *Retrieval           `json:"retrieval,omitempty"`
	EnterpriseWebSearch *EnterpriseWebSearch `json:"enterpriseWebSearch,omitempty"`

	// Only in Gemini API:
	FileSearch *FileSearch `json:"fileSearch,omitempty"`
}

type FunctionDeclaration struct {
	Name                 string      `json:"name"`
	Description          string      `json:"description,omitempty"`
	Parameters           *Schema     `json:"parameters,omitempty"`
	ParametersJsonSchema interface{} `json:"parametersJsonSchema,omitempty"`
	Response             *Schema     `json:"response,omitempty"`
	ResponseJsonSchema   interface{} `json:"responseJsonSchema,omitempty"`
}

type GoogleSearch struct {
	// Only in Gemini API:
	TimeRangeFilter *Interval `json:"timeRangeFilter,omitempty"`

	// Only in Vertex API:
	ExcludedDomains    []string `json:"excludedDomains,omitempty"`
	BlockingConfidence string   `json:"blockingConfidence,omitempty"`
}

type EnterpriseWebSearch struct {
	ExcludedDomains    []string `json:"excludedDomains,omitempty"`
	BlockingConfidence string   `json:"blockingConfidence,omitempty"`
}

const (
	PhishBlockThresholdUnspecified = "PHISH_BLOCK_THRESHOLD_UNSPECIFIED"
	PhishBlockLowAndAbove          = "BLOCK_LOW_AND_ABOVE"
	PhishBlockMediumAndAbove       = "BLOCK_MEDIUM_AND_ABOVE"
	PhishBlockHighAndAbove         = "BLOCK_HIGH_AND_ABOVE"
	PhishBlockHigherAndAbove       = "BLOCK_HIGHER_AND_ABOVE"
	PhishBlockVeryHighAndAbove     = "BLOCK_VERY_HIGH_AND_ABOVE"
	PhishBlockOnlyExtremelyHigh    = "BLOCK_ONLY_EXTREMELY_HIGH"
)

type Interval struct {
	StartTime string `json:"startTime,omitempty"`
	EndTime   string `json:"endTime,omitempty"`
}

type GoogleSearchRetrieval struct {
	DynamicRetrievalConfig *DynamicRetrievalConfig `json:"dynamicRetrievalConfig,omitempty"`
}

type DynamicRetrievalConfig struct {
	Mode             string  `json:"mode,omitempty"`
	DynamicThreshold float32 `json:"dynamicThreshold,omitempty"`
}

const (
	DynamicRetrievalModeUnspecified = "MODE_UNSPECIFIED"
	DynamicRetrievalModeDynamic     = "DYNAMIC"
)

type GoogleMaps struct {
	EnableWidget bool `json:"enableWidget,omitempty"`
}

// URLContext is an empty struct for the url_context field in the API request.
type URLContext struct{}

// CodeExecution is an empty struct for the code_execution field in the API request.
type CodeExecution struct{}

type Retrieval struct {
	VertexAiSearch *VertexAISearch `json:"vertexAiSearch,omitempty"`
	VertexRagStore *VertexRagStore `json:"vertexRagStore,omitempty"`
	ExternalApi    *ExternalApi    `json:"externalApi,omitempty"`
}

type VertexAISearch struct {
	Datastore      string          `json:"datastore,omitempty"`
	Engine         string          `json:"engine,omitempty"`
	MaxResults     int             `json:"maxResults,omitempty"`
	Filter         string          `json:"filter,omitempty"`
	DataStoreSpecs []DataStoreSpec `json:"dataStoreSpecs,omitempty"`
}

type DataStoreSpec struct {
	DataStore string `json:"dataStore"`
	Filter    string `json:"filter,omitempty"`
}

type VertexRagStore struct {
	RagResources       []RagResource       `json:"ragResources,omitempty"`
	RagRetrievalConfig *RagRetrievalConfig `json:"ragRetrievalConfig,omitempty"`
}

type RagResource struct {
	RagCorpus  string   `json:"ragCorpus,omitempty"`
	RagFileIds []string `json:"ragFileIds,omitempty"`
}

type RagRetrievalConfig struct {
	TopK    int      `json:"topK,omitempty"`
	Filter  *Filter  `json:"filter,omitempty"`
	Ranking *Ranking `json:"ranking,omitempty"`
}

type Filter struct {
	MetadataFilter            string   `json:"metadataFilter,omitempty"`
	VectorDistanceThreshold   *float32 `json:"vectorDistanceThreshold,omitempty"`
	VectorSimilarityThreshold *float32 `json:"vectorSimilarityThreshold,omitempty"`
}

type Ranking struct {
	RankService *RankService `json:"rankService,omitempty"`
	LlmRanker   *LlmRanker   `json:"llmRanker,omitempty"`
}

type RankService struct {
	ModelName string `json:"modelName,omitempty"`
}

type LlmRanker struct {
	ModelName string `json:"modelName,omitempty"`
}

type ExternalApi struct {
	ApiSpec             string               `json:"apiSpec"`
	Endpoint            string               `json:"endpoint"`
	AuthConfig          AuthConfig           `json:"authConfig"`
	SimpleSearchParams  *SimpleSearchParams  `json:"simpleSearchParams,omitempty"`
	ElasticSearchParams *ElasticSearchParams `json:"elasticSearchParams,omitempty"`
}

const (
	ApiSpecSimpleSearch  = "SIMPLE_SEARCH"
	ApiSpecElasticSearch = "ELASTIC_SEARCH"
)

type SimpleSearchParams struct{}

type ElasticSearchParams struct {
	Index          string `json:"index"`
	SearchTemplate string `json:"searchTemplate"`
	NumHits        int    `json:"numHits,omitempty"`
}

type AuthConfig struct {
	AuthType                   string                      `json:"authType"`
	ApiKeyConfig               *ApiKeyConfig               `json:"apiKeyConfig,omitempty"`
	HttpBasicAuthConfig        *HttpBasicAuthConfig        `json:"httpBasicAuthConfig,omitempty"`
	GoogleServiceAccountConfig *GoogleServiceAccountConfig `json:"googleServiceAccountConfig,omitempty"`
	OauthConfig                *OauthConfig                `json:"oauthConfig,omitempty"`
	OidcConfig                 *OidcConfig                 `json:"oidcConfig,omitempty"`
}

const (
	AuthTypeNoAuth                   = "NO_AUTH"
	AuthTypeApiKeyAuth               = "API_KEY_AUTH"
	AuthTypeHttpBasicAuth            = "HTTP_BASIC_AUTH"
	AuthTypeGoogleServiceAccountAuth = "GOOGLE_SERVICE_ACCOUNT_AUTH"
	AuthTypeOauth                    = "OAUTH"
	AuthTypeOidcAuth                 = "OIDC_AUTH"
)

type ApiKeyConfig struct {
	Name                string `json:"name,omitempty"`
	ApiKeySecret        string `json:"apiKeySecret,omitempty"`
	ApiKeyString        string `json:"apiKeyString,omitempty"`
	HttpElementLocation string `json:"httpElementLocation,omitempty"`
}

const (
	HttpInQuery  = "HTTP_IN_QUERY"
	HttpInHeader = "HTTP_IN_HEADER"
	HttpInPath   = "HTTP_IN_PATH"
	HttpInBody   = "HTTP_IN_BODY"
	HttpInCookie = "HTTP_IN_COOKIE"
)

type HttpBasicAuthConfig struct {
	CredentialSecret string `json:"credentialSecret"`
}

type GoogleServiceAccountConfig struct {
	ServiceAccount string `json:"serviceAccount,omitempty"`
}

type OauthConfig struct {
	AccessToken    *string `json:"accessToken,omitempty"`
	ServiceAccount *string `json:"serviceAccount,omitempty"`
}

type OidcConfig struct {
	IdToken        *string `json:"idToken,omitempty"`
	ServiceAccount *string `json:"serviceAccount,omitempty"`
}

type ComputerUse struct {
	Environment                 string   `json:"environment,omitempty"`
	ExcludedPredefinedFunctions []string `json:"excludedPredefinedFunctions,omitempty"`
}

const (
	ComputerUseEnvironmentUnspecified = "ENVIRONMENT_UNSPECIFIED"
	ComputerUseEnvironmentBrowser     = "ENVIRONMENT_BROWSER"
)

type FileSearch struct {
	RetrievalResources []*RetrievalResource `json:"retrievalResources"`
	RetrievalConfig    *RetrievalConfig     `json:"retrievalConfig,omitempty"`
}

type RetrievalResource struct {
	RagStoreName string `json:"ragStoreName"`
}

type RetrievalConfig struct {
	MetadataFilter string `json:"metadataFilter,omitempty"`
	TopK           int    `json:"topK,omitempty"`
}

type FunctionCallingConfig struct {
	Mode                 string   `json:"mode,omitempty"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

const (
	FunctionCallingModeAuto      = "AUTO"
	FunctionCallingModeAny       = "ANY"
	FunctionCallingModeNone      = "NONE"
	FunctionCallingModeValidated = "VALIDATED"
)

type RetrievalToolConfig struct {
	LatLng       *LatLng `json:"latLng,omitempty"`
	LanguageCode string  `json:"languageCode,omitempty"`
}

type LatLng struct {
	Latitude  float32 `json:"latitude,omitempty"`
	Longitude float32 `json:"longitude,omitempty"`
}

type ToolConfig struct {
	FunctionCallingConfig *FunctionCallingConfig `json:"functionCallingConfig,omitempty"`
	RetrievalConfig       *RetrievalToolConfig   `json:"retrievalConfig,omitempty"`
}

type SafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type ModelArmorConfig struct {
	PromptTemplateName   string `json:"promptTemplateName,omitempty"`
	ResponseTemplateName string `json:"responseTemplateName,omitempty"`
}

type GenerateContentRequest struct {
	Contents          []Content         `json:"contents"`
	SystemInstruction *Content          `json:"systemInstruction,omitempty"`
	CachedContent     string            `json:"cachedContent,omitempty"`
	Tools             []Tool            `json:"tools,omitempty"`
	ToolConfig        *ToolConfig       `json:"toolConfig,omitempty"`
	SafetySettings    []SafetySetting   `json:"safetySettings,omitempty"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`

	// Only in Vertex API:
	Labels           map[string]string `json:"labels,omitempty"`
	ModelArmorConfig *ModelArmorConfig `json:"modelArmorConfig,omitempty"`
}

type CAGenerateContentRequest struct {
	Model   string                 `json:"model"`
	Project string                 `json:"project,omitempty"`
	Request GenerateContentRequest `json:"request"`
}

type PromptFeedback struct {
	BlockReason   string         `json:"blockReason,omitempty"`
	SafetyRatings []SafetyRating `json:"safetyRatings,omitempty"`

	// Only in Vertex API:
	BlockReasonMessage string `json:"blockReasonMessage,omitempty"`
}

const (
	BlockReasonSafety            = "SAFETY"
	BlockReasonOther             = "OTHER"
	BlockReasonBlocklist         = "BLOCKLIST"
	BlockReasonProhibitedContent = "PROHIBITED_CONTENT"
	BlockReasonImageSafety       = "IMAGE_SAFETY"

	// Only in Vertex API:
	BlockReasonModelArmor = "MODEL_ARMOR"
	BlockReasonJailbreak  = "JAILBREAK"
)

type SafetyRating struct {
	Category    string `json:"category"`
	Probability string `json:"probability"`
	Blocked     bool   `json:"blocked"`

	// Only in Vertex API:
	ProbabilityScore     float32 `json:"probabilityScore,omitempty"`
	Severity             string  `json:"severity,omitempty"`
	SeverityScore        float32 `json:"severityScore,omitempty"`
	OverwrittenThreshold string  `json:"overwrittenThreshold,omitempty"`
}

const (
	HarmCategoryUnspecified      = "HARM_CATEGORY_UNSPECIFIED"
	HarmCategoryDerogatory       = "HARM_CATEGORY_DEROGATORY"
	HarmCategoryToxicity         = "HARM_CATEGORY_TOXICITY"
	HarmCategoryViolence         = "HARM_CATEGORY_VIOLENCE"
	HarmCategorySexual           = "HARM_CATEGORY_SEXUAL"
	HarmCategoryMedical          = "HARM_CATEGORY_MEDICAL"
	HarmCategoryDangeous         = "HARM_CATEGORY_DANGEROUS"
	HarmCategoryHarassment       = "HARM_CATEGORY_HARASSMENT"
	HarmCategoryHateSpeech       = "HARM_CATEGORY_HATE_SPEECH"
	HarmCategorySexuallyExplicit = "HARM_CATEGORY_SEXUALLY_EXPLICIT"
	HarmCategoryDangerousContent = "HARM_CATEGORY_DANGEROUS_CONTENT"
)

const (
	HarmProbabilityNegligible = "NEGLIGIBLE"
	HarmProbabilityLow        = "LOW"
	HarmProbabilityMedium     = "MEDIUM"
	HarmProbabilityHigh       = "HIGH"
)

const (
	HarmSeverityNegligible = "NEGLIGIBLE"
	HarmSeverityLow        = "LOW"
	HarmSeverityMedium     = "MEDIUM"
	HarmSeverityHigh       = "HIGH"
)

const (
	HarmBlockThresholdUnspecified = "HARM_BLOCK_THRESHOLD_UNSPECIFIED"
	HarmBlockLowAndAbove          = "BLOCK_LOW_AND_ABOVE"
	HarmBlockMediumAndAbove       = "BLOCK_MEDIUM_AND_ABOVE"
	HarmBlockOnlyHigh             = "BLOCK_ONLY_HIGH"
	HarmBlockNone                 = "BLOCK_NONE"
	HarmBlockOff                  = "OFF"
)

type UsageMetadata struct {
	PromptTokenCount           int                  `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount       int                  `json:"candidatesTokenCount,omitempty"`
	TotalTokenCount            int                  `json:"totalTokenCount,omitempty"`
	ToolUsePromptTokenCount    int                  `json:"toolUsePromptTokenCount,omitempty"`
	ThoughtsTokenCount         int                  `json:"thoughtsTokenCount,omitempty"`
	CachedContentTokenCount    int                  `json:"cachedContentTokenCount,omitempty"`
	PromptTokensDetails        []ModalityTokenCount `json:"promptTokensDetails,omitempty"`
	CacheTokensDetails         []ModalityTokenCount `json:"cacheTokensDetails,omitempty"`
	CandiatesTokensDetails     []ModalityTokenCount `json:"candidatesTokensDetails,omitempty"`
	ToolUsePromptTokensDetails []ModalityTokenCount `json:"toolUsePromptTokensDetails,omitempty"`

	// Only in Vertex API:
	TrafficType string `json:"trafficType,omitempty"`
}

const (
	TrafficTypeUnspecified       = "TRAFFIC_TYPE_UNSPECIFIED"
	TrafficOnDemand              = "ON_DEMAND"
	TrafficProvisionedThroughput = "PROVISIONED_THROUGHPUT"
)

type ModalityTokenCount struct {
	Modality   string `json:"modality"`
	TokenCount int    `json:"tokenCount"`
}

const (
	ModalityUnspecified = "MODALITY_UNSPECIFIED"
	ModalityText        = "TEXT"
	ModalityImage       = "IMAGE"
	ModalityVideo       = "VIDEO"
	ModalityAudio       = "AUDIO"
	ModalityDocument    = "DOCUMENT"
)

type Candidate struct {
	Index              int                 `json:"index"`
	Content            Content             `json:"content"`
	FinishReason       string              `json:"finishReason,omitempty"`
	SafetyRatings      []SafetyRating      `json:"safetyRatings,omitempty"`
	CitationMetadata   CitationMetadata    `json:"citationMetadata,omitempty"`
	GroundingMetadata  *GroundingMetadata  `json:"groundingMetadata,omitempty"`
	URLContextMetadata *URLContextMetadata `json:"urlContextMetadata,omitempty"`
	AvgLogprobs        float32             `json:"avgLogprobs,omitempty"`
	LogprobsResult     *LogprobsResult     `json:"logprobsResult,omitempty"`
	FinishMessage      string              `json:"finishMessage,omitempty"`

	// Only in Gemini API:
	GroundingAttributions []GroundingAttribution `json:"groundingAttributions,omitempty"`
	TokenCount            int                    `json:"tokenCount,omitempty"`
}

const (
	FinishReasonStop                   = "STOP"
	FinishReasonMaxTokens              = "MAX_TOKENS"
	FinishReasonSafety                 = "SAFETY"
	FinishReasonRecitation             = "RECITATION"
	FinishReasonLanguage               = "LANGUAGE"
	FinishReasonOther                  = "OTHER"
	FinishReasonBlocklist              = "BLOCKLIST"
	FinishReasonProhibitedContent      = "PROHIBITED_CONTENT"
	FinishReasonSpii                   = "SPII"
	FinishReasonMalformedFunctionCall  = "MALFORMED_FUNCTION_CALL"
	FinishReasonImageSafety            = "IMAGE_SAFETY"
	FinishReasonImageProhibitedContent = "IMAGE_PROHIBITED_CONTENT"
	FinishReasonImageOther             = "IMAGE_OTHER"
	FinishReasonNoImage                = "NO_IMAGE"
	FinishReasonImageRecitation        = "IMAGE_RECITATION"
	FinishReasonUnexpectedToolCall     = "UNEXPECTED_TOOL_CALL"

	// Only in Gemini API:
	FinishReasonTooManyToolCalls = "TOO_MANY_TOOL_CALLS"

	// Only in Vertex API:
	FinishReasonModelArmor = "MODEL_ARMOR"
)

type CitationMetadata struct {
	// Only in Gemini API:
	CitationSources []CitationSource `json:"citationSources,omitempty"`

	// Only in Vertex API:
	Citations []CitationSource `json:"citations,omitempty"`
}

type CitationSource struct {
	StartIndex int    `json:"startIndex,omitempty"`
	EndIndex   int    `json:"endIndex,omitempty"`
	URI        string `json:"uri,omitempty"`
	License    string `json:"license,omitempty"`

	// Only in Vertex API:
	Title           string `json:"title,omitempty"`
	PublicationDate struct {
		Year  int `json:"year"`
		Month int `json:"month"`
		Day   int `json:"day"`
	} `json:"publicationDate"`
}

type GroundingAttribution struct {
	SourceId AttributionSourceId `json:"sourceId"`
	Content  Content             `json:"content"`
}

type AttributionSourceId struct {
	GroundingPassage       *GroundingPassageId     `json:"groundingPassage,omitempty"`
	SemanticRetrieverChunk *SemanticRetrieverChunk `json:"semanticRetrieverChunk,omitempty"`
}

type GroundingPassageId struct {
	PassageId string `json:"passageId"`
	PartIndex int    `json:"partIndex"`
}

type SemanticRetrieverChunk struct {
	Source string `json:"source"`
	Chunk  string `json:"chunk"`
}

type GroundingMetadata struct {
	GroundingChunks              []GroundingChunk   `json:"groundingChunks,omitempty"`
	GroundingSupports            []GroundingSupport `json:"groundingSupports,omitempty"`
	WebSearchQueries             []string           `json:"webSearchQueries,omitempty"`
	SearchEntryPoint             *SearchEntryPoint  `json:"searchEntryPoint,omitempty"`
	RetrievalMetadata            *RetrievalMetadata `json:"retrievalMetadata,omitempty"`
	GoogleMapsWidgetContextToken string             `json:"googleMapsWidgetContextToken,omitempty"`

	// Only in Vertex API:
	SourceFlaggingUris []SourceFlaggingUri `json:"sourceFlaggingUris,omitempty"`
}

type GroundingChunk struct {
	Web              *GroundingChunkWeb              `json:"web,omitempty"`
	RetrievedContext *GroundingChunkRetrievedContext `json:"retrievedContext,omitempty"`
	Maps             *GroundingChunkMaps             `json:"maps,omitempty"`
}

type GroundingChunkWeb struct {
	URI   string `json:"uri"`
	Title string `json:"title"`

	// Only in Vertex API:
	Domain string `json:"domain,omitempty"`
}

type GroundingChunkRetrievedContext struct {
	URI   string `json:"uri,omitempty"`
	Title string `json:"title,omitempty"`
	Text  string `json:"text,omitempty"`

	// Only in Vertex API:
	RagChunk     *RagChunk `json:"ragChunk,omitempty"`
	DocumentName string    `json:"documentName,omitempty"`
}

type RagChunk struct {
	Text     string    `json:"text"`
	PageSpan *PageSpan `json:"pageSpan,omitempty"`
}

type PageSpan struct {
	FirstPage int `json:"firstPage"`
	LastPage  int `json:"lastPage"`
}

type GroundingChunkMaps struct {
	URI                string             `json:"uri"`
	Title              string             `json:"title"`
	Text               string             `json:"text"`
	PlaceId            string             `json:"placeId"`
	PlaceAnswerSources PlaceAnswerSources `json:"placeAnswerSources"`
}

type PlaceAnswerSources struct {
	ReviewSnippets []ReviewSnippet `json:"reviewSnippets,omitempty"`
}

type ReviewSnippet struct {
	ReviewId      string `json:"reviewId"`
	GoogleMapsURI string `json:"googleMapsUri"`
	Title         string `json:"title"`
}

type GroundingSupport struct {
	Segment               *GroundingSupportSegment `json:"segment,omitempty"`
	GroundingChunkIndices []int                    `json:"groundingChunkIndices,omitempty"`
	ConfidenceScores      []float32                `json:"confidenceScores,omitempty"`
}

type GroundingSupportSegment struct {
	PartIndex  int    `json:"partIndex,omitempty"`
	StartIndex int    `json:"startIndex,omitempty"`
	EndIndex   int    `json:"endIndex,omitempty"`
	Text       string `json:"text,omitempty"`
}

type SearchEntryPoint struct {
	RenderedContent string `json:"renderedContent,omitempty"`
	SdkBlob         string `json:"sdkBlob,omitempty"`
}

type RetrievalMetadata struct {
	GoogleSearchDynamicRetrievalScore float32 `json:"googleSearchDynamicRetrievalScore,omitempty"`
}

type SourceFlaggingUri struct {
	SourceId       string `json:"sourceId"`
	FlagContentUri string `json:"flagContentUri"`
}

type URLContextMetadata struct {
	URLMetadata []URLMetadata `json:"urlMetadata,omitempty"`
}

type URLMetadata struct {
	RetrievedURL       string `json:"retrievedUrl,omitempty"`
	URLRetrievalStatus string `json:"urlRetrievalStatus,omitempty"`
}

const (
	URLRetrievalStatusSuccess = "SUCCESS"
	URLRetrievalStatusError   = "ERROR"

	// Only in Gemini API:
	URLRetrievalStatusPaywall = "PAYWALL"
	URLRetrievalStatusUnsafe  = "UNSAFE"
)

type LogprobsResult struct {
	TopCandidates    []TopCandidates `json:"topCandidates"`
	ChosenCandidates []Candidate     `json:"chosenCandidates"`

	// Only in Gemini API:
	LogProbabilitySum float32 `json:"logProbabilitySum"`
}

type TopCandidates struct {
	Candidates []TopCandidate `json:"candidates"`
}

type TopCandidate struct {
	Token          string  `json:"token"`
	TokenId        int     `json:"tokenId"`
	LogProbability float32 `json:"logProbability"`
}

type GenerateContentResponse struct {
	Candidates     []Candidate     `json:"candidates"`
	PromptFeedback *PromptFeedback `json:"promptFeedback,omitempty"`
	UsageMetadata  *UsageMetadata  `json:"usageMetadata,omitempty"`
	ModelVersion   string          `json:"modelVersion,omitempty"`
	ResponseId     string          `json:"responseId,omitempty"`

	// Only in Vertex API:
	CreateTime string `json:"createTime,omitempty"`
}

type CaGenerateContentResponse struct {
	Response GenerateContentResponse `json:"response"`
}

type CountTokenRequest struct {
	Model    string    `json:"model"`
	Contents []Content `json:"contents"`
}

type CaCountTokenRequest struct {
	Request CountTokenRequest `json:"request"`
}

type CaCountTokenResponse struct {
	TotalTokens int `json:"totalTokens"`
}

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
