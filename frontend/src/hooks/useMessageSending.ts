import { useNavigate, useParams } from 'react-router-dom';
import { apiFetch } from '../api/apiClient';
import { useSetAtom } from 'jotai';
import type { ChatMessage, FileAttachment } from '../types/chat';
import { convertFilesToAttachments } from '../utils/fileHandler';
import { processStreamResponse, type StreamEventHandlers, sendMessage } from '../utils/messageHandler';
import {
  addErrorMessageAtom,
  addMessageAtom,
  chatSessionIdAtom,
  inputMessageAtom,
  processingStartTimeAtom,
  isSystemPromptEditingAtom,
  lastAutoDisplayedThoughtIdAtom,
  selectedFilesAtom,
  setSessionNameAtom,
  sessionsAtom,
  systemPromptAtom,
  primaryBranchIdAtom,
  updateAgentMessageAtom,
  updateUserMessageIdAtom,
  updateMessageTokenCountAtom,
} from '../atoms/chatAtoms';
import { ModelInfo } from '../api/models';

interface UseMessageSendingProps {
  inputMessage: string;
  selectedFiles: File[];
  chatSessionId: string | null;
  systemPrompt: string;
  primaryBranchId: string;
  selectedModel: ModelInfo | null;
}

export const useMessageSending = ({
  inputMessage,
  selectedFiles,
  chatSessionId,
  systemPrompt,
  primaryBranchId,
  selectedModel,
}: UseMessageSendingProps) => {
  const navigate = useNavigate();
  const { workspaceId } = useParams<{ workspaceId?: string }>();

  const setChatSessionId = useSetAtom(chatSessionIdAtom);
  const setInputMessage = useSetAtom(inputMessageAtom);
  const setProcessingStartTime = useSetAtom(processingStartTimeAtom);
  const setIsSystemPromptEditing = useSetAtom(isSystemPromptEditingAtom);
  const setLastAutoDisplayedThoughtId = useSetAtom(lastAutoDisplayedThoughtIdAtom);
  const setSelectedFiles = useSetAtom(selectedFilesAtom);
  const setSessionName = useSetAtom(setSessionNameAtom);
  const setSessions = useSetAtom(sessionsAtom);
  const setSystemPrompt = useSetAtom(systemPromptAtom);
  const setPrimaryBranchId = useSetAtom(primaryBranchIdAtom);
  const addMessage = useSetAtom(addMessageAtom);
  const updateAgentMessage = useSetAtom(updateAgentMessageAtom);
  const updateUserMessageId = useSetAtom(updateUserMessageIdAtom);
  const updateMessageTokenCount = useSetAtom(updateMessageTokenCountAtom);
  const addErrorMessage = useSetAtom(addErrorMessageAtom);

  const handleSendMessage = async () => {
    if (!inputMessage.trim() && selectedFiles.length === 0) return;

    setProcessingStartTime(performance.now());

    try {
      const attachments: FileAttachment[] = await convertFilesToAttachments(selectedFiles);

      const temporaryUserMessageId = crypto.randomUUID();
      const userMessage: ChatMessage = {
        id: temporaryUserMessageId,
        role: 'user',
        parts: [{ text: inputMessage }],
        type: 'user',
        attachments: attachments,
        model: selectedModel?.name,
      };
      addMessage(userMessage);
      setInputMessage('');
      setSelectedFiles([]);

      if (chatSessionId === null) {
        setIsSystemPromptEditing(false);
      }

      const response = await sendMessage(
        inputMessage,
        attachments,
        chatSessionId,
        systemPrompt,
        workspaceId,
        primaryBranchId,
        selectedModel?.name,
      );

      if (response.status === 401) {
        window.location.reload(); // Reload the page on 401 to re-check login status
        return;
      }

      if (!response.ok) {
        const errorMessage =
          response.status === 499 ? 'Request cancelled by user.' : 'Failed to send message or receive stream.';
        addErrorMessage(errorMessage);
        return;
      }

      const handlers: StreamEventHandlers = {
        onMessage: (messageId: string, text: string) => {
          updateAgentMessage({ messageId, text, modelName: selectedModel?.name });
          setLastAutoDisplayedThoughtId(null);
        },
        onThought: (messageId: string, thoughtText: string) => {
          addMessage({
            id: messageId,
            role: 'model',
            parts: [{ text: thoughtText }],
            type: 'thought',
          } as ChatMessage);
          setLastAutoDisplayedThoughtId(messageId);
        },
        onFunctionCall: (messageId: string, functionName: string, functionArgs: any) => {
          const message: ChatMessage = {
            id: messageId,
            role: 'model',
            parts: [{ functionCall: { name: functionName, args: functionArgs } }],
            type: 'function_call',
          };
          addMessage(message);
          setLastAutoDisplayedThoughtId(null);
        },
        onFunctionResponse: (messageId: string, functionName: string, functionResponse: any) => {
          const message: ChatMessage = {
            id: messageId,
            role: 'user',
            parts: [{ functionResponse: { name: functionName, response: functionResponse } }],
            type: 'function_response',
            model: selectedModel?.name,
          };
          addMessage(message);
          setLastAutoDisplayedThoughtId(null);
        },
        onSessionStart: (sessionId: string, systemPrompt: string, primaryBranchId: string) => {
          setChatSessionId(sessionId);
          setSystemPrompt(systemPrompt);
          setPrimaryBranchId(primaryBranchId);
          // Add the new session to the sessionsAtom with a temporary name
          setSessions((prevSessions) => [
            { id: sessionId, name: '', isEditing: false, last_updated_at: new Date().toISOString() },
            ...prevSessions,
          ]);
          navigate(workspaceId ? `/w/${workspaceId}/${sessionId}` : `/${sessionId}`, { replace: true });
        },
        onSessionNameUpdate: (sessionId: string, newName: string) => {
          setSessionName({ sessionId, name: newName });
        },
        onEnd: () => {
          setLastAutoDisplayedThoughtId(null);
          setProcessingStartTime(null);
        },
        onError: (errorData: string) => {
          addErrorMessage(errorData);
        },
        onAcknowledge: (messageId: string) => {
          updateUserMessageId({ temporaryId: userMessage.id, newId: messageId });
        },
        onTokenCount: (messageId: string, cumulTokenCount: number) => {
          updateMessageTokenCount({ messageId, cumulTokenCount });
        },
      };

      const { qReceived, nReceived } = await processStreamResponse(response, handlers);

      if (!qReceived) {
        console.error('Backend bug: Stream ended without receiving both Q and N events.', { qReceived, nReceived });
        addErrorMessage('An unexpected error occurred: Stream did not finalize correctly.');
      }
    } catch (error) {
      console.error('Error sending message or receiving stream:', error);
      addErrorMessage('Error sending message or receiving stream.');
    } finally {
      setProcessingStartTime(null);
    }
  };

  const cancelStreamingCall = async () => {
    if (!chatSessionId) return;

    try {
      const response = await apiFetch(`/api/chat/${chatSessionId}/call`, {
        method: 'DELETE',
      });

      if (response.ok) {
        setProcessingStartTime(null);
        addErrorMessage('Request cancelled by user.');
      } else {
        console.error(
          `Failed to cancel streaming call for session ${chatSessionId}:`,
          response.status,
          response.statusText,
        );
        addErrorMessage(`Failed to cancel request: ${response.status} ${response.statusText}`);
      }
    } catch (error) {
      console.error(`Error cancelling streaming call for session ${chatSessionId}:`, error);
    }
  };

  return { handleSendMessage, cancelStreamingCall };
};
