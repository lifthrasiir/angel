import { useNavigate, useParams } from 'react-router-dom';
import { useEffect, useRef } from 'react';
import { apiFetch, switchBranch } from '../api/apiClient';
import { useSetAtom, useAtomValue } from 'jotai';
import type { ChatMessage, FileAttachment } from '../types/chat';
import { convertFilesToAttachments } from '../utils/fileHandler';
import { processStreamResponse, type StreamEventHandlers, sendMessage } from '../utils/messageHandler';
import {
  addErrorMessageAtom,
  addMessageAtom,
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
  pendingRootsAtom,
  compressAbortControllerAtom,
  editingMessageIdAtom,
  messagesAtom,
} from '../atoms/chatAtoms';
import { ModelInfo } from '../api/models';
import { SessionManager } from './useSessionManager';

interface UseMessageSendingProps {
  inputMessage: string;
  selectedFiles: File[];
  chatSessionId: string | null;
  systemPrompt: string;
  primaryBranchId: string;
  selectedModel: ModelInfo | null;
  sessionManager: SessionManager;
}

export const useMessageSending = ({
  inputMessage,
  selectedFiles,
  chatSessionId,
  systemPrompt,
  primaryBranchId,
  selectedModel,
  sessionManager,
}: UseMessageSendingProps) => {
  const navigate = useNavigate();
  const { workspaceId } = useParams<{ workspaceId?: string }>();

  // sessionId is now managed by FSM
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
  const pendingRoots = useAtomValue(pendingRootsAtom);
  const setPendingRoots = useSetAtom(pendingRootsAtom);
  const compressAbortController = useAtomValue(compressAbortControllerAtom);
  const setCompressAbortController = useSetAtom(compressAbortControllerAtom);
  const setEditingMessageId = useSetAtom(editingMessageIdAtom);
  const setMessages = useSetAtom(messagesAtom);
  // sessionId is now managed by FSM - use chatSessionId prop instead

  // Store the sessionId from onSessionStart for immediate use in onInlineData
  const latestSessionIdRef = useRef<string | null>(null);

  // Store AbortController to cancel active streams when switching sessions
  const streamAbortControllerRef = useRef<AbortController | null>(null);

  // Update messages without sessionId when they exist and chatSessionId is available
  useEffect(() => {
    if (chatSessionId) {
      setMessages((prevMessages) => {
        // Check if there are any messages without sessionId
        const needsUpdate = prevMessages.some((message) => !message.sessionId);
        if (!needsUpdate) {
          return prevMessages; // No need to create new array
        }

        console.log('useEffect: Found messages without sessionId, updating them');
        return prevMessages.map((message) => {
          if (!message.sessionId) {
            console.log('useEffect: Updating sessionId for message:', message.id, '-> sessionId:', chatSessionId);
            return { ...message, sessionId: chatSessionId };
          }
          return message;
        });
      });
    }
  }, [chatSessionId, setMessages]);

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
      setLastAutoDisplayedThoughtId(messageId);
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
    onFunctionResponse: (
      messageId: string,
      functionName: string,
      functionResponse: any,
      attachments: FileAttachment[],
    ) => {
      const message: ChatMessage = {
        id: messageId,
        parts: [{ functionResponse: { name: functionName, response: functionResponse } }],
        type: 'function_response',
        model: selectedModel?.name,
        attachments: attachments,
      };
      addMessage(message);
      setLastAutoDisplayedThoughtId(null);
    },
    onInlineData: (messageId: string, attachments: FileAttachment[]) => {
      const message: ChatMessage = {
        id: messageId,
        parts: [], // Empty parts for inline data messages
        type: 'model',
        model: selectedModel?.name,
        attachments: attachments,
        sessionId: latestSessionIdRef.current || undefined,
      };
      addMessage(message);
      setLastAutoDisplayedThoughtId(null);
    },
    onSessionStart: (sessionId: string, systemPrompt: string, primaryBranchId: string) => {
      // Store sessionId immediately for use in other handlers in the same stream
      latestSessionIdRef.current = sessionId;

      // sessionId is now managed by FSM
      setSystemPrompt(systemPrompt);
      setPrimaryBranchId(primaryBranchId);

      // Update existing messages that don't have sessionId yet (for new sessions)
      setMessages((prevMessages) => {
        // Check if there are any messages without sessionId
        const needsUpdate = prevMessages.some((message) => !message.sessionId);
        if (!needsUpdate) {
          return prevMessages;
        }

        return prevMessages.map((message) => {
          if (!message.sessionId) {
            return { ...message, sessionId };
          }
          return message;
        });
      });

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
      setLastAutoDisplayedThoughtId(null);
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

  const handleSendMessage = async (message?: string) => {
    const messageToSend = message || inputMessage;
    if (!messageToSend.trim() && selectedFiles.length === 0) return;

    setProcessingStartTime(performance.now());

    try {
      const attachments: FileAttachment[] = await convertFilesToAttachments(selectedFiles);

      const temporaryUserMessageId = crypto.randomUUID();
      const userMessage: ChatMessage = {
        id: temporaryUserMessageId,
        parts: [{ text: messageToSend }],
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
        messageToSend,
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

      // Create new AbortController for this stream
      streamAbortControllerRef.current = new AbortController();
      // Register with global stream registry
      if (streamAbortControllerRef.current && sessionManager) {
        sessionManager.setActiveAbortController(streamAbortControllerRef.current);
      }
      await processStreamResponse(response, handlers, streamAbortControllerRef.current.signal);
      // Clear from global registry
      if (streamAbortControllerRef.current && sessionManager) {
        sessionManager.setActiveAbortController(null);
      }
      streamAbortControllerRef.current = null;
    } catch (error) {
      console.error('Error sending message or receiving stream:', error);
      // Don't show error message if the stream was intentionally aborted
      if (error instanceof DOMException && error.name === 'AbortError') {
        console.log('🚫 Stream was intentionally aborted (normal behavior)');
        // Re-throw to ensure proper stream termination
        throw error;
      } else {
        addErrorMessage('Error sending message or receiving stream.');
      }
    } finally {
      setProcessingStartTime(null);
      setPendingRoots([]);
    }
  };

  const cancelActiveStreams = () => {
    // Force abort any active stream immediately
    if (streamAbortControllerRef.current) {
      streamAbortControllerRef.current.abort();
      streamAbortControllerRef.current = null;
    }

    // Reset processing state immediately
    setProcessingStartTime(null);
  };

  const cancelStreamingCall = async () => {
    if (!chatSessionId) return;

    // If a compress operation is ongoing, abort it instead of the streaming call
    if (compressAbortController) {
      compressAbortController.abort();
      setCompressAbortController(null);
      setProcessingStartTime(null);
      addErrorMessage('Compression cancelled by user.');
      return;
    }

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

      // Create new AbortController for this stream
      streamAbortControllerRef.current = new AbortController();
      // Register with global stream registry
      if (streamAbortControllerRef.current && sessionManager) {
        sessionManager.setActiveAbortController(streamAbortControllerRef.current);
      }
      await processStreamResponse(response, handlers, streamAbortControllerRef.current.signal);
      // Clear from global registry
      if (streamAbortControllerRef.current && sessionManager) {
        sessionManager.setActiveAbortController(null);
      }
      streamAbortControllerRef.current = null;
    } catch (error) {
      console.error('Error sending confirmation:', error);
      // Don't show error message if the stream was intentionally aborted
      if (error instanceof DOMException && error.name === 'AbortError') {
        console.log('🚫 Confirmation stream was intentionally aborted (normal behavior)');
        // Re-throw to ensure proper stream termination
        throw error;
      } else {
        addErrorMessage('Error sending confirmation.');
      }
    } finally {
      setProcessingStartTime(null);
    }
  };

  const handleEditMessage = async (originalMessageId: string, editedText: string) => {
    if (!chatSessionId) {
      addErrorMessage('Cannot edit message: Session ID is missing.');
      return;
    }

    setProcessingStartTime(performance.now());
    setEditingMessageId(null); // Exit editing mode

    // Update message content and remove subsequent messages on the frontend
    setMessages((prevMessages) => {
      const updatedMessages = [...prevMessages];
      const messageIndex = updatedMessages.findIndex((msg) => msg.id === originalMessageId);

      if (messageIndex !== -1) {
        // Update message content
        updatedMessages[messageIndex] = {
          ...updatedMessages[messageIndex],
          parts: [{ text: editedText }],
        };
        // Remove all messages after the edited message
        return updatedMessages.slice(0, messageIndex + 1);
      }
      return prevMessages; // If message not found, return previous state
    });

    try {
      const requestBody = {
        updatedMessageId: parseInt(originalMessageId, 10), // Convert message ID to integer
        newMessageText: editedText,
      };

      const response = await apiFetch(`/api/chat/${chatSessionId}/branch`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody),
      });

      if (response.status === 401) {
        window.location.reload();
        return;
      }

      if (!response.ok) {
        const errorMessage = `Failed to edit message: ${response.status} ${response.statusText}`;
        addErrorMessage(errorMessage);
        return;
      }

      const handlers: StreamEventHandlers = {
        ...commonHandlers,
        onAcknowledge: (messageId: string) => {
          // When a message is edited, the backend might send an acknowledge with the new message ID.
          // We need to update the original message's ID to the new one.
          // This assumes the backend sends the new ID via onAcknowledge.
          // The originalMessageId is captured in the closure.
          // This will update the ID of the message that was just edited.
          updateUserMessageId({ temporaryId: originalMessageId, newId: messageId });
        },
      };

      // Create new AbortController for this stream
      streamAbortControllerRef.current = new AbortController();
      // Register with global stream registry
      if (streamAbortControllerRef.current && sessionManager) {
        sessionManager.setActiveAbortController(streamAbortControllerRef.current);
      }
      await processStreamResponse(response, handlers, streamAbortControllerRef.current.signal);
      // Clear from global registry
      if (streamAbortControllerRef.current && sessionManager) {
        sessionManager.setActiveAbortController(null);
      }
      streamAbortControllerRef.current = null;
    } catch (error) {
      console.error('Error editing message:', error);
      // Don't show error message if the stream was intentionally aborted
      if (error instanceof DOMException && error.name === 'AbortError') {
        console.log('🚫 Edit message stream was intentionally aborted (normal behavior)');
        // Re-throw to ensure proper stream termination
        throw error;
      } else {
        addErrorMessage('Error editing message.');
      }
    } finally {
      setProcessingStartTime(null);
    }
  };

  const handleBranchSwitch = async (newBranchId: string) => {
    if (!chatSessionId) {
      addErrorMessage('Cannot switch branch: Session ID is missing.');
      return;
    }

    try {
      setProcessingStartTime(performance.now());

      // Call the API to switch branches
      await switchBranch(chatSessionId, newBranchId);

      // Reload the page to reflect the branch change
      window.location.reload();
    } catch (error) {
      console.error('Failed to switch branch:', error);
      addErrorMessage('Failed to switch branch. Please try again.');
    } finally {
      setProcessingStartTime(null);
    }
  };

  const handleRetryMessage = async (originalMessageId: string) => {
    if (!chatSessionId) {
      addErrorMessage('Cannot retry message: Session ID is missing.');
      return;
    }

    setProcessingStartTime(performance.now());
    setEditingMessageId(null); // Exit editing mode if active

    // Remove this message and all subsequent messages on the frontend
    setMessages((prevMessages) => {
      const updatedMessages = [...prevMessages];
      const messageIndex = updatedMessages.findIndex((msg) => msg.id === originalMessageId);

      if (messageIndex !== -1) {
        // Remove all messages after the retry message
        return updatedMessages.slice(0, messageIndex + 1);
      }
      return prevMessages; // If message not found, return previous state
    });

    try {
      const requestBody = {
        updatedMessageId: parseInt(originalMessageId, 10), // Convert message ID to integer
        newMessageText: '', // Empty text for retry (server will get original text)
      };

      const response = await apiFetch(`/api/chat/${chatSessionId}/branch?retry=1`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody),
      });

      if (response.status === 401) {
        window.location.reload();
        return;
      }

      if (!response.ok) {
        const errorMessage = `Failed to retry message: ${response.status} ${response.statusText}`;
        addErrorMessage(errorMessage);
        return;
      }

      const handlers: StreamEventHandlers = {
        ...commonHandlers,
        onAcknowledge: (messageId: string) => {
          // When a message is retried, the backend might send an acknowledge with the new message ID.
          // We need to update the original message's ID to the new one.
          updateUserMessageId({ temporaryId: originalMessageId, newId: messageId });
        },
      };

      // Create new AbortController for this stream
      streamAbortControllerRef.current = new AbortController();
      // Register with global stream registry
      if (streamAbortControllerRef.current && sessionManager) {
        sessionManager.setActiveAbortController(streamAbortControllerRef.current);
      }
      await processStreamResponse(response, handlers, streamAbortControllerRef.current.signal);
      // Clear from global registry
      if (streamAbortControllerRef.current && sessionManager) {
        sessionManager.setActiveAbortController(null);
      }
      streamAbortControllerRef.current = null;
    } catch (error) {
      console.error('Error retrying message:', error);
      // Don't show error message if the stream was intentionally aborted
      if (error instanceof DOMException && error.name === 'AbortError') {
        console.log('🚫 Retry message stream was intentionally aborted (normal behavior)');
        // Re-throw to ensure proper stream termination
        throw error;
      } else {
        addErrorMessage('Error retrying message.');
      }
    } finally {
      setProcessingStartTime(null);
    }
  };

  const handleRetryError = async (_errorMessageId: string) => {
    if (!chatSessionId) {
      addErrorMessage('Cannot retry error: Session ID is missing.');
      return;
    }

    if (!primaryBranchId) {
      addErrorMessage('Cannot retry error: Branch ID is missing.');
      return;
    }

    setProcessingStartTime(performance.now());
    setEditingMessageId(null); // Exit editing mode if active

    // Remove consecutive error messages from the end of the message list
    setMessages((prevMessages) => {
      const updatedMessages = [...prevMessages];
      let removeCount = 0;

      // Count consecutive error messages from the end
      for (let i = updatedMessages.length - 1; i >= 0; i--) {
        const message = updatedMessages[i];
        if (message.type === 'model_error') {
          removeCount++;
        } else {
          break; // Stop at first non-error message
        }
      }

      // Remove the error messages
      if (removeCount > 0) {
        console.log(`Removing ${removeCount} error messages before retry`);
        return updatedMessages.slice(0, -removeCount);
      }

      return prevMessages;
    });

    try {
      const response = await apiFetch(`/api/chat/${chatSessionId}/branch/${primaryBranchId}/retry-error`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}), // Empty body as the endpoint doesn't need additional data
      });

      if (response.status === 401) {
        window.location.reload();
        return;
      }

      if (!response.ok) {
        const errorMessage = `Failed to retry error: ${response.status} ${response.statusText}`;
        addErrorMessage(errorMessage);
        return;
      }

      const handlers: StreamEventHandlers = {
        ...commonHandlers,
        onAcknowledge: () => {
          // No user message to acknowledge for retry-error flow
          // The backend will handle error message deletion
        },
      };

      // Create new AbortController for this stream
      streamAbortControllerRef.current = new AbortController();
      // Register with global stream registry
      if (streamAbortControllerRef.current && sessionManager) {
        sessionManager.setActiveAbortController(streamAbortControllerRef.current);
      }
      await processStreamResponse(response, handlers, streamAbortControllerRef.current.signal);
      // Clear from global registry
      if (streamAbortControllerRef.current && sessionManager) {
        sessionManager.setActiveAbortController(null);
      }
      streamAbortControllerRef.current = null;
    } catch (error) {
      console.error('Error retrying error message:', error);
      // Don't show error message if the stream was intentionally aborted
      if (error instanceof DOMException && error.name === 'AbortError') {
        console.log('🚫 Retry error stream was intentionally aborted (normal behavior)');
        // Re-throw to ensure proper stream termination
        throw error;
      } else {
        addErrorMessage('Error retrying error message.');
      }
    } finally {
      setProcessingStartTime(null);
    }
  };

  return {
    handleSendMessage,
    cancelStreamingCall,
    cancelActiveStreams,
    sendConfirmation,
    handleEditMessage,
    handleBranchSwitch,
    handleRetryMessage,
    handleRetryError,
  };
};
