import { useNavigate } from 'react-router-dom';
import { ChatMessage, FileAttachment, Session } from '../types/chat';
import { 
  updateMessagesState, 
  processStreamingMessage, 
  sendMessage, 
  processStreamResponse, 
  StreamEventHandlers 
} from '../utils/messageHandler';
import { convertFilesToAttachments } from '../utils/fileHandler';

interface UseMessageSendingProps {
  inputMessage: string;
  selectedFiles: File[];
  chatSessionId: string | null;
  systemPrompt: string;
  setInputMessage: (message: string) => void;
  setSelectedFiles: (files: File[]) => void;
  setIsStreaming: (streaming: boolean) => void;
  setMessages: (messages: ChatMessage[] | ((prev: ChatMessage[]) => ChatMessage[])) => void;
  setLastAutoDisplayedThoughtId: (id: string | null) => void;
  setChatSessionId: (id: string) => void;
  setSessions: (sessions: Session[] | ((prev: Session[]) => Session[])) => void;
  setIsSystemPromptEditing: (editing: boolean) => void;
  handleLoginRedirect: () => void;
  loadSessions: () => Promise<void>;
}

export const useMessageSending = ({
  inputMessage,
  selectedFiles,
  chatSessionId,
  systemPrompt,
  setInputMessage,
  setSelectedFiles,
  setIsStreaming,
  setMessages,
  setLastAutoDisplayedThoughtId,
  setChatSessionId,
  setSessions,
  setIsSystemPromptEditing,
  handleLoginRedirect,
  loadSessions,
}: UseMessageSendingProps) => {
  const navigate = useNavigate();

  const updateMessages = (
    newMessage: ChatMessage,
    options?: { replaceId?: string; insertBeforeAgentId?: string }
  ) => {
    setMessages((prev) => updateMessagesState(prev, newMessage, options));
  };

  const handleSendMessage = async () => {
    if (!inputMessage.trim() && selectedFiles.length === 0) return;

    setIsStreaming(true);

    try {
      const attachments: FileAttachment[] = await convertFilesToAttachments(selectedFiles);

      const userMessage: ChatMessage = { 
        id: crypto.randomUUID(), 
        role: 'user', 
        parts: [{ text: inputMessage }], 
        type: 'user', 
        attachments: attachments 
      };
      updateMessages(userMessage);
      setInputMessage('');
      setSelectedFiles([]);

      let agentMessageId = crypto.randomUUID();
      updateMessages({ id: agentMessageId, role: 'model', parts: [{ text: '' }], type: 'model' } as ChatMessage);

      if (chatSessionId === null) {
        setIsSystemPromptEditing(false);
      }

      const response = await sendMessage(inputMessage, attachments, chatSessionId, systemPrompt);

      if (response.status === 401) {
        handleLoginRedirect();
        return;
      }

      if (!response.ok) {
        updateMessages({
          id: agentMessageId,
          role: 'system',
          parts: [{ text: 'Failed to send message or receive stream.' }],
          type: 'system',
        } as ChatMessage, { replaceId: agentMessageId });
        return;
      }

      let currentAgentText = '';

      const handlers: StreamEventHandlers = {
        onMessage: (text: string) => {
          currentAgentText += text;
          setMessages((prev) => processStreamingMessage(prev, agentMessageId, currentAgentText));
          setLastAutoDisplayedThoughtId(null);
        },
        onThought: (thoughtText: string, thoughtId: string) => {
          updateMessages(
            { id: thoughtId, role: 'model', parts: [{ text: thoughtText }], type: 'thought' } as ChatMessage,
            { insertBeforeAgentId: agentMessageId }
          );
          setLastAutoDisplayedThoughtId(thoughtId);
        },
        onFunctionCall: (functionName: string, functionArgs: any) => {
          const message: ChatMessage = { 
            id: crypto.randomUUID(), 
            role: 'model', 
            parts: [{ functionCall: { name: functionName, args: functionArgs } }], 
            type: 'function_call' 
          };
          updateMessages(message, { insertBeforeAgentId: agentMessageId });
          setLastAutoDisplayedThoughtId(null);
        },
        onFunctionResponse: (functionResponse: any) => {
          const message: ChatMessage = { 
            id: crypto.randomUUID(), 
            role: 'user', 
            parts: [{ functionResponse: { response: functionResponse } }], 
            type: 'function_response' 
          };
          updateMessages(message, { insertBeforeAgentId: agentMessageId });
          setLastAutoDisplayedThoughtId(null);
        },
        onSessionUpdate: (newSessionId: string) => {
          setChatSessionId(newSessionId);
          navigate(`/${newSessionId}`, { replace: true });
        },
        onSessionNameUpdate: (sessionIdToUpdate: string, newName: string) => {
          setSessions(prevSessions =>
            prevSessions.map(s =>
              s.id === sessionIdToUpdate ? { ...s, name: newName } : s
            )
          );
        },
        onEnd: () => {
          setLastAutoDisplayedThoughtId(null);
        }
      };

      await processStreamResponse(response, handlers);
      loadSessions();
      setIsStreaming(false);
      setSelectedFiles([]);

    } catch (error) {
      console.error('Error sending message or receiving stream:', error);
      updateMessages({
        id: crypto.randomUUID(),
        role: 'system',
        parts: [{ text: 'Error sending message or receiving stream.' }],
        type: 'system',
      } as ChatMessage);
    } finally {
      setIsStreaming(false);
      loadSessions();
      setSelectedFiles([]);
    }
  };

  return { handleSendMessage };
};
