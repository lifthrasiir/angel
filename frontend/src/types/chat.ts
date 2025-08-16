export interface FileAttachment {
  fileName: string;
  mimeType: string;
  hash?: string; // SHA-512/256 hash of the data (optional, filled by backend)
  data?: string; // Base64 encoded binary data, used for upload
}

export interface PossibleNextMessage {
  messageId: string;
  branchId: string;
}

export interface FunctionCall {
  name: string;
  args: Record<string, any>;
}

export interface FunctionResponse {
  name: string;
  response: any;
}

export interface ChatMessage {
  id: string;
  role: string;
  parts: { text?: string; functionCall?: FunctionCall; functionResponse?: FunctionResponse }[];
  type?:
    | 'model'
    | 'thought'
    | 'system'
    | 'system_prompt'
    | 'user'
    | 'function_call'
    | 'function_response'
    | 'model_error'
    | 'compression';
  attachments?: FileAttachment[];
  cumulTokenCount?: number | null;
  branchId?: string;
  parentMessageId?: string;
  chosenNextId?: string;
  possibleNextIds?: PossibleNextMessage[];
  model?: string; // New field for the model that generated the message
  sessionId?: string; // New field for the session ID
}

export interface InitialState {
  sessionId: string;
  history: ChatMessage[];
  systemPrompt: string;
  workspaceId: string;
  primaryBranchId: string;
}

export interface Session {
  id: string;
  last_updated_at: string;
  name?: string;
  isEditing?: boolean;
  workspace_id?: string;
}

export interface Workspace {
  id: string;
  name: string;
  default_system_prompt: string;
  created_at: string;
}

export interface WorkspaceWithSessions {
  workspace: Workspace;
  sessions: Session[];
}
