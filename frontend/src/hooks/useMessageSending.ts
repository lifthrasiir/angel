import { useNavigate, useParams } from 'react-router-dom';
import { apiFetch } from '../api/apiClient';
import { useSetAtom, useAtomValue } from 'jotai'; // Added useAtomValue
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
  pendingConfirmationAtom,
  temporaryEnvChangeMessageAtom,
  pendingRootsAtom, // Add this import
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
  const setPendingConfirmation = useSetAtom(pendingConfirmationAtom);
  const setTemporaryEnvChangeMessage = useSetAtom(temporaryEnvChangeMessageAtom);
  const pendingRoots = useAtomValue(pendingRootsAtom); // Add this line
  const setPendingRoots = useSetAtom(pendingRootsAtom); // Add this line for setter

  const commonHandlers = {
    onMessage: (messageId: string, text: string) => {
      updateAgentMessage({ messageId, text, modelName: selectedModel?.name });
      setLastAutoDisplayedThoughtId(null);
    },
    onThought: (messageId: string, thoughtText: string) => {
      addMessage({
        id: messageId,
        parts: [{ text: thoughtText }],
        type: 'thought',
      } as ChatMessage);
      setLastAutoDisplayedThoughtId(null);
    },
    onFunctionCall: (messageId: string, functionName: string, functionArgs: any) => {
      const message: ChatMessage = {
        id: messageId,
        parts: [{ functionCall: { name: functionName, args: functionArgs } }],
        type: 'function_call',
      };
      addMessage(message);
      setLastAutoDisplayedThoughtId(null);
    },
    onFunctionResponse: (messageId: string, functionName: string, functionResponse: any) => {
      const message: ChatMessage = {
        id: messageId,
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
      setSessions((prevSessions) => {
        // Check if the session already exists
        const existingSessionIndex = prevSessions.findIndex((s) => s.id === sessionId);

        if (existingSessionIndex !== -1) {
          // If it exists, update it (e.g., last_updated_at)
          const updatedSessions = [...prevSessions];
          updatedSessions[existingSessionIndex] = {
            ...updatedSessions[existingSessionIndex],
            last_updated_at: new Date().toISOString(),
            name: updatedSessions[existingSessionIndex].name || '',
          };
          return updatedSessions;
        } else {
          // If it doesn't exist, add it
          return [
            { id: sessionId, name: '', isEditing: false, last_updated_at: new Date().toISOString() },
            ...prevSessions,
          ];
        }
      });
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
    // onAcknowledge is handled separately as it depends on userMessage.id
    onAcknowledge: () => {},
    onTokenCount: (messageId: string, cumulTokenCount: number) => {
      updateMessageTokenCount({ messageId, cumulTokenCount });
    },
    onPendingConfirmation: (data: string) => {
      setPendingConfirmation(data);
      setProcessingStartTime(null);
    },
    onEnvChanged: (messageId: string, envChanged: string) => {
      addMessage({
        id: messageId,
        parts: [{ text: envChanged }],
        type: 'env_changed',
      } as ChatMessage);
      setTemporaryEnvChangeMessage(null);
    },
  };

  const handleSendMessage = async () => {
    if (!inputMessage.trim() && selectedFiles.length === 0) return;

    setProcessingStartTime(performance.now());

    try {
      const attachments: FileAttachment[] = await convertFilesToAttachments(selectedFiles);

      const temporaryUserMessageId = crypto.randomUUID();
      const userMessage: ChatMessage = {
        id: temporaryUserMessageId,
        parts: [{ text: inputMessage }],
        type: 'user',
        attachments: attachments,
        model: selectedModel?.name,
      };
      addMessage(userMessage);
      setInputMessage('');
      setSelectedFiles([]);
      setTemporaryEnvChangeMessage(null);

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
        chatSessionId === null ? pendingRoots : undefined,
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
        ...commonHandlers,
        onAcknowledge: (messageId: string) => {
          updateUserMessageId({ temporaryId: userMessage.id, newId: messageId });
        },
      };

      await processStreamResponse(response, handlers);
    } catch (error) {
      console.error('Error sending message or receiving stream:', error);
      addErrorMessage('Error sending message or receiving stream.');
    } finally {
      setProcessingStartTime(null);
      setPendingRoots([]);
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

  const sendConfirmation = async (
    approved: boolean,
    sessionId: string,
    branchId: string,
    modifiedData?: Record<string, any>,
  ) => {
    setProcessingStartTime(performance.now());
    setPendingConfirmation(null); // Clear pending confirmation immediately

    try {
      const requestBody: { approved: boolean; modifiedData?: Record<string, any> } = { approved };
      if (modifiedData) {
        requestBody.modifiedData = modifiedData;
      }

      const response = await apiFetch(`/api/chat/${sessionId}/branch/${branchId}/confirm`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody),
      });

      if (response.status === 401) {
        window.location.reload();
        return;
      }

      if (!response.ok) {
        const errorMessage = `Failed to send confirmation: ${response.status} ${response.statusText}`;
        addErrorMessage(errorMessage);
        return;
      }

      const handlers: StreamEventHandlers = {
        ...commonHandlers,
        onAcknowledge: () => {
          // No user message to acknowledge for confirmation flow
        },
      };

      await processStreamResponse(response, handlers);
    } catch (error) {
      console.error('Error sending confirmation:', error);
      addErrorMessage('Error sending confirmation.');
    } finally {
      setProcessingStartTime(null);
    }
  };

  return { handleSendMessage, cancelStreamingCall, sendConfirmation };
};
