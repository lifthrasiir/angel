package types

type EventType rune

const (
	// SSE Event Types
	//
	// Sending initial messages: A -> 0 -> any number of T/M/F/R/C/I -> P or (Q -> N) or E
	// Sending subsequent messages: any number of G -> A -> any number of T/M/F/R/C/I -> P/Q/E
	// Loading messages and streaming current call: W -> 1 or (0 -> any number of T/M/F/R/C/I -> Q/E)
	//
	// Several events have payloads, described in brackets after the event type.
	// Multiple comma-separated items in the payload should be separated by newlines.
	// Note that some but not all items are encoded in JSON.
	EventWorkspaceHint       EventType = 'W' // Workspace ID hint (sent before initial state)                          [Workspace ID]
	EventInitialState        EventType = '0' // Initial state with history (for active call)                      [InitialState JSON]
	EventInitialStateNoCall  EventType = '1' // Initial state with history (for load session when no active call) [InitialState JSON]
	EventAcknowledge         EventType = 'A' // Acknowledge message ID                                                   [Message ID]
	EventThought             EventType = 'T' // Thought process                                                  [Title, description]
	EventModelMessage        EventType = 'M' // Model message                                                      [Message ID, text]
	EventFunctionCall        EventType = 'F' // Function call                                         [Function name, arguments JSON]
	EventFunctionResponse    EventType = 'R' // Function response                       [Function name, FunctionResponsePayload JSON]
	EventInlineData          EventType = 'I' // Inline file/image data with hash keys                        [InlineDataPayload JSON]
	EventComplete            EventType = 'Q' // Query complete
	EventSessionName         EventType = 'N' // Session name inferred/updated                                      [New session name]
	EventCumulTokenCount     EventType = 'C' // Cumulative token count update                                       [New token count]
	EventPendingConfirmation EventType = 'P' // Pending confirmation, following EventFunctionCall msg [tool.PendingConfirmation JSON]
	EventGenerationChanged   EventType = 'G' // Generation changed event                                        [env.EnvChanged JSON]
	EventPing                EventType = '.' // Ping message for connection keep-alive
	EventError               EventType = 'E' // Error message                                                     [Error description]
)

// FunctionResponsePayload defines the structure for the EventFunctionResponse payload
type FunctionResponsePayload struct {
	Response    map[string]interface{} `json:"response"`
	Attachments []FileAttachment       `json:"attachments,omitempty"`
}

// InlineDataPayload defines the structure for the EventInlineData payload
type InlineDataPayload struct {
	MessageId   string           `json:"messageId"`
	Attachments []FileAttachment `json:"attachments"`
}

type EventWriter interface {
	Acquire()
	Release()
	Send(eventType EventType, data string)
	Broadcast(eventType EventType, data string)
	Close()
}
