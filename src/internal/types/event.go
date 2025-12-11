package types

type EventType rune

const (
	// SSE Event Types
	//
	// Sending initial messages: A -> 0 -> any number of T/M/F/R/C/I -> P or (Q -> N) or E
	// Sending subsequent messages: any number of G -> A -> any number of T/M/F/R/C/I -> P/Q/E
	// Loading messages and streaming current call: W -> 1 or (0 -> any number of T/M/F/R/C/I -> Q/E)
	EventWorkspaceHint       EventType = 'W' // Workspace ID hint (sent before initial state)
	EventInitialState        EventType = '0' // Initial state with history (for active call)
	EventInitialStateNoCall  EventType = '1' // Initial state with history (for load session when no active call)
	EventAcknowledge         EventType = 'A' // Acknowledge message ID
	EventThought             EventType = 'T' // Thought process
	EventModelMessage        EventType = 'M' // Model message (text)
	EventFunctionCall        EventType = 'F' // Function call
	EventFunctionResponse    EventType = 'R' // Function response
	EventInlineData          EventType = 'I' // Inline file/image data with hash keys
	EventComplete            EventType = 'Q' // Query complete
	EventSessionName         EventType = 'N' // Session name inferred/updated
	EventCumulTokenCount     EventType = 'C' // Cumulative token count update
	EventPendingConfirmation EventType = 'P' // Pending confirmation exists for the last message which is EventFunctionCall
	EventGenerationChanged   EventType = 'G' // Generation changed event
	EventPing                EventType = '.' // Ping message for connection keep-alive
	EventError               EventType = 'E' // Error message
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
