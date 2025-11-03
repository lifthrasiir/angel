import { splitOnceByNewline } from '../utils/stringUtils';
import type { InitialState } from './chat';

// SSE Event Types
//
// Sending initial messages: A -> 0 -> any number of T/M/F/R/C/I -> P or (Q -> N) or E
// Sending subsequent messages: any number of G -> A -> any number of T/M/F/R/C/I -> P/Q/E
// Loading messages and streaming current call: W -> 1 or (0 -> any number of T/M/F/R/C/I -> Q/E)
export const EventWorkspaceHint = 'W';
export const EventInitialState = '0';
export const EventInitialStateNoCall = '1';
export const EventAcknowledge = 'A';
export const EventThought = 'T';
export const EventModelMessage = 'M';
export const EventFunctionCall = 'F';
export const EventFunctionResponse = 'R';
export const EventInlineData = 'I';
export const EventComplete = 'Q';
export const EventSessionName = 'N';
export const EventCumulTokenCount = 'C';
export const EventPendingConfirmation = 'P';
export const EventGenerationChanged = 'G';
export const EventPing = '.';
export const EventError = 'E';

export type SseWorkspaceHint = {
  type: typeof EventWorkspaceHint;
  workspaceId: string;
};

// EventInitialState and EventInitialStateNoCall contain parsed InitialState data
export type SseInitialState = {
  type: typeof EventInitialState;
  initialState: InitialState;
};

export type SseInitialStateNoCall = {
  type: typeof EventInitialStateNoCall;
  initialState: InitialState;
};

export type SseAcknowledge = {
  type: typeof EventAcknowledge;
  messageId: string;
};

export type SseThought = {
  type: typeof EventThought;
  messageId: string;
  thoughtText: string;
};

export type SseModelMessage = {
  type: typeof EventModelMessage;
  messageId: string;
  text: string;
};

export type SseFunctionCall = {
  type: typeof EventFunctionCall;
  messageId: string;
  functionName: string;
  functionArgs: any;
};

export type SseFunctionResponse = {
  type: typeof EventFunctionResponse;
  messageId: string;
  functionName: string;
  response: any;
  attachments: any[];
};

export type SseInlineData = {
  type: typeof EventInlineData;
  messageId: string;
  attachments: any[];
};

export type SseComplete = {
  type: typeof EventComplete;
};

export type SseSessionName = {
  type: typeof EventSessionName;
  sessionId: string;
  newName: string;
};

export type SseCumulTokenCount = {
  type: typeof EventCumulTokenCount;
  messageId: string;
  cumulTokenCount: number;
};

export type SsePendingConfirmation = {
  type: typeof EventPendingConfirmation;
  data: string;
};

export type SseGenerationChanged = {
  type: typeof EventGenerationChanged;
  messageId: string;
  envChangedJson: string;
};

export type SsePing = {
  type: typeof EventPing;
};

export type SseError = {
  type: typeof EventError;
  error: string;
};

export type SseEvent =
  | SseWorkspaceHint
  | SseInitialState
  | SseInitialStateNoCall
  | SseAcknowledge
  | SseThought
  | SseModelMessage
  | SseFunctionCall
  | SseFunctionResponse
  | SseInlineData
  | SseComplete
  | SseSessionName
  | SseCumulTokenCount
  | SsePendingConfirmation
  | SseGenerationChanged
  | SsePing
  | SseError;

export function parseSseEvent(eventString: string): SseEvent {
  const [type, data] = splitOnceByNewline(eventString);

  switch (type) {
    case EventWorkspaceHint:
      return {
        type: EventWorkspaceHint,
        workspaceId: data,
      } as SseWorkspaceHint;

    case EventInitialState: {
      const initialState = JSON.parse(data) as InitialState;
      return {
        type: EventInitialState,
        initialState,
      } as SseInitialState;
    }

    case EventInitialStateNoCall: {
      const initialState = JSON.parse(data) as InitialState;
      return {
        type: EventInitialStateNoCall,
        initialState,
      } as SseInitialStateNoCall;
    }

    case EventAcknowledge:
      return {
        type: EventAcknowledge,
        messageId: data,
      } as SseAcknowledge;

    case EventThought:
      const [thoughtMessageId, thoughtText] = splitOnceByNewline(data);
      return {
        type: EventThought,
        messageId: thoughtMessageId,
        thoughtText,
      } as SseThought;

    case EventModelMessage:
      const [messageId, text] = splitOnceByNewline(data);
      return {
        type: EventModelMessage,
        messageId,
        text,
      } as SseModelMessage;

    case EventFunctionCall:
      const [fcMessageId, rest] = splitOnceByNewline(data);
      const [functionName, functionArgsJson] = splitOnceByNewline(rest);
      const functionArgs = JSON.parse(functionArgsJson);
      return {
        type: EventFunctionCall,
        messageId: fcMessageId,
        functionName,
        functionArgs,
      } as SseFunctionCall;

    case EventFunctionResponse:
      const [frMessageId, frRest] = splitOnceByNewline(data);
      const [frFunctionName, payloadJsonString] = splitOnceByNewline(frRest);
      const { response, attachments } = JSON.parse(payloadJsonString);
      return {
        type: EventFunctionResponse,
        messageId: frMessageId,
        functionName: frFunctionName,
        response,
        attachments,
      } as SseFunctionResponse;

    case EventInlineData:
      const { messageId: inlineMessageId, attachments: inlineAttachments } = JSON.parse(data);
      return {
        type: EventInlineData,
        messageId: inlineMessageId,
        attachments: inlineAttachments,
      } as SseInlineData;

    case EventComplete:
      return {
        type: EventComplete,
      } as SseComplete;

    case EventSessionName:
      const [sessionIdToUpdate, newName] = splitOnceByNewline(data);
      return {
        type: EventSessionName,
        sessionId: sessionIdToUpdate,
        newName,
      } as SseSessionName;

    case EventCumulTokenCount:
      const [ctcMessageId, cumulTokenCountStr] = splitOnceByNewline(data);
      return {
        type: EventCumulTokenCount,
        messageId: ctcMessageId,
        cumulTokenCount: parseInt(cumulTokenCountStr, 10),
      } as SseCumulTokenCount;

    case EventPendingConfirmation:
      return {
        type: EventPendingConfirmation,
        data,
      } as SsePendingConfirmation;

    case EventGenerationChanged:
      const [gcMessageId, envChangedJson] = splitOnceByNewline(data);
      return {
        type: EventGenerationChanged,
        messageId: gcMessageId,
        envChangedJson,
      } as SseGenerationChanged;

    case EventPing:
      return {
        type: EventPing,
      } as SsePing;

    case EventError:
      return {
        type: EventError,
        error: data,
      } as SseError;

    default:
      throw new Error(`Unknown SSE event type: ${type}`);
  }
}
