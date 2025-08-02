export interface FileAttachment {
  fileName: string;
  mimeType: string;
  data: string;
}

export interface ChatMessage {
  id: string;
  role: string;
  parts: { text?: string; functionCall?: any; functionResponse?: any; }[];
  type?: "model" | "thought" | "system" | "user" | "function_call" | "function_response" | "model_error";
  attachments?: FileAttachment[];
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