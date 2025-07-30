export interface FileAttachment {
  fileName: string;
  mimeType: string;
  data: string;
}

export interface ChatMessage {
  id: string;
  role: string;
  parts: { text?: string; functionCall?: any; functionResponse?: any; }[];
  type?: "model" | "thought" | "system" | "user" | "function_call" | "function_response";
  attachments?: FileAttachment[];
}

export interface Session {
  id: string;
  last_updated_at: string;
  name?: string;
  isEditing?: boolean;
}