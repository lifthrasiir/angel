import { useCallback, useEffect, useMemo, useRef } from 'react';
import { useSetAtom, useAtomValue } from 'jotai';
import { useLocation } from 'react-router-dom';
import type { ChatMessage } from '../types/chat';
import { ModelInfo } from '../api/models';
import { convertFilesToAttachments } from '../utils/fileHandler';
import { useSessionManagerContext } from './SessionManagerContext';
import { parseURLPath } from '../utils/urlSessionMapping';
import {
  addErrorMessageAtom,
  addMessageAtom,
  resetChatSessionStateAtom,
  messagesAtom,
  primaryBranchIdAtom,
  sessionsAtom,
  systemPromptAtom,
  updateAgentMessageAtom,
  updateUserMessageIdAtom,
  setSessionNameAtom,
  inputMessageAtom,
  currentSessionNameAtom,
} from '../atoms/chatAtoms';
import { pendingConfirmationAtom, temporaryEnvChangeMessageAtom } from '../atoms/confirmationAtoms';
import { isSystemPromptEditingAtom, editingMessageIdAtom } from '../atoms/uiAtoms';
import { selectedFilesAtom, preserveSelectedFilesAtom } from '../atoms/fileAtoms';
import { selectedModelAtom } from '../atoms/modelAtoms';
import type { InitialStateData, MessageSendParams, OperationEventHandlers } from '../managers/SessionOperationManager';
import {
  EventThought,
  EventModelMessage,
  EventFunctionCall,
  EventFunctionResponse,
  EventSessionName,
  EventWorkspaceHint,
  EventComplete,
  EventAcknowledge,
  EventCumulTokenCount,
  EventInlineData,
  EventPendingConfirmation,
  EventGenerationChanged,
  EventPing,
  EventFinish,
  type SseEvent,
  EARLIER_MESSAGES_LOADED,
} from '../types/events';

interface UseSessionFSMProps {
  onSessionSwitch?: () => void;
}

export const useSessionFSM = ({ onSessionSwitch }: UseSessionFSMProps = {}) => {
  // Get session manager context
  const sessionManager = useSessionManagerContext();
  const operationManager = sessionManager?.operationManager;
  const location = useLocation();

  // Ref to track previous pathname to detect actual URL transitions
  const prevPathnameRef = useRef<string | null>(null);
  // Ref to track the original message ID that should be updated with new ID from EventAcknowledge
  // Used for edit/retry operations where the original message gets a new ID in a new branch
  const pendingMessageIdToUpdate = useRef<string | null>(null);

  // Jotai atoms
  const setMessages = useSetAtom(messagesAtom);
  const addMessage = useSetAtom(addMessageAtom);
  const updateAgentMessage = useSetAtom(updateAgentMessageAtom);
  const addErrorMessage = useSetAtom(addErrorMessageAtom);
  const resetChatSessionState = useSetAtom(resetChatSessionStateAtom);
  const setIsSystemPromptEditing = useSetAtom(isSystemPromptEditingAtom);
  const setPrimaryBranchId = useSetAtom(primaryBranchIdAtom);
  const setSelectedFiles = useSetAtom(selectedFilesAtom);
  const setSystemPrompt = useSetAtom(systemPromptAtom);
  const setPendingConfirmation = useSetAtom(pendingConfirmationAtom);
  const setTemporaryEnvChangeMessage = useSetAtom(temporaryEnvChangeMessageAtom);
  const setPreserveSelectedFiles = useSetAtom(preserveSelectedFilesAtom);
  const setSessionNameInList = useSetAtom(setSessionNameAtom);
  const setCurrentSessionName = useSetAtom(currentSessionNameAtom);
  const setInputMessage = useSetAtom(inputMessageAtom);
  const setEditingMessageId = useSetAtom(editingMessageIdAtom);
  const updateUserMessageId = useSetAtom(updateUserMessageIdAtom);

  const messages = useAtomValue(messagesAtom);
  const selectedModel = useAtomValue(selectedModelAtom);
  const sessions = useAtomValue(sessionsAtom);
  const systemPrompt = useAtomValue(systemPromptAtom);
  const primaryBranchId = useAtomValue(primaryBranchIdAtom);

  // Event handlers for operation manager
  const eventHandlers: OperationEventHandlers = useMemo(
    () => ({
      onInitialState: (data: InitialStateData) => {
        // Reset chat state for new session
        if (!data.isCallActive) {
          resetChatSessionState();
        }

        // Update messages from initial state
        // Only update messages if we're not actively streaming (to avoid overwriting streaming messages)
        setMessages((prevMessages) => {
          if (data.isCallActive && prevMessages.length > 0) {
            // Already streaming and have messages, don't overwrite
            return prevMessages;
          } else if (data.messages && data.messages.length > 0) {
            return data.messages;
          } else {
            return [];
          }
        });

        // Set system prompt
        if (data.systemPrompt) {
          setSystemPrompt(data.systemPrompt);
          setIsSystemPromptEditing(false);
        }

        // Set primary branch ID
        if (data.primaryBranchId) {
          setPrimaryBranchId(data.primaryBranchId);
        }

        // Set workspace ID, just in case (should have been already set by EventWorkspaceHint)
        sessionManager.setSessionWorkspaceId(data.workspaceId!);

        // Set current session name (for both temporary and regular sessions)
        setCurrentSessionName(data.name);

        // Handle pending confirmation
        if (data.pendingConfirmation) {
          setPendingConfirmation(data.pendingConfirmation);
        }

        // Handle temporary env change message
        if (data.temporaryEnvChangeMessage) {
          setTemporaryEnvChangeMessage(data.temporaryEnvChangeMessage);
        }

        // Note: Session load completion is now managed by SessionState

        // If this is a new session creation (URL is /new or /temp), navigate to the session URL
        const pathname = location.pathname;
        const isNewSessionURL =
          pathname === '/new' || pathname === '/temp' || pathname.match(/^\/w\/[^\/]+\/(new|temp)$/) !== null;

        if (isNewSessionURL && data.sessionId && data.sessionId !== sessionManager.sessionId) {
          // Navigate will trigger the URL effect, but it's idempotent (won't reload if sessionId matches)
          sessionManager.navigateToSession(data.sessionId);
        }

        // Call session switch callback
        onSessionSwitch?.();
      },

      onEvent: (event: SseEvent) => {
        switch (event.type) {
          // Note: EventInitialState and EventInitialStateNoCall are handled by onInitialState callback
          // to avoid double-processing and message overwrites

          case EventWorkspaceHint:
            // Handle workspace hint event
            console.log('Workspace hint received:', event.workspaceId);
            // Set the workspace ID in session manager
            sessionManager.setSessionWorkspaceId(event.workspaceId);
            break;

          case EventAcknowledge:
            // Handle message acknowledgement (message was sent successfully)
            console.log('Message acknowledged:', event.messageId);
            // Update temporary message ID with actual server ID
            const tempId = (event as any).temporaryMessageId;
            if (tempId && event.messageId) {
              // New message: temporary ID -> actual server ID
              updateUserMessageId({ temporaryId: tempId, newId: event.messageId });
            } else if (pendingMessageIdToUpdate.current && event.messageId) {
              // Edit/retry: original message ID -> new message ID in new branch
              updateUserMessageId({ temporaryId: pendingMessageIdToUpdate.current, newId: event.messageId });
              pendingMessageIdToUpdate.current = null; // Clear after update
            }
            break;

          case EventComplete:
            // Handle stream completion
            console.log('Stream completed');
            // Note: Processing state is now managed by SessionState
            break;

          case EventThought:
            // Handle thought message
            addMessage({
              id: event.messageId || `thought-${Date.now()}`,
              role: 'model',
              parts: [{ text: event.thoughtText }],
              type: 'thought',
              timestamp: new Date().toISOString(),
            } as ChatMessage);
            break;

          case EventModelMessage:
            // Handle model message
            if (event.text) {
              updateAgentMessage({
                messageId: event.messageId,
                text: event.text,
                // TODO: modelName
              });
            }
            break;

          case EventFunctionCall:
            // Handle function call
            addMessage({
              id: event.messageId || `function-${Date.now()}`,
              role: 'model',
              parts: [
                {
                  functionCall: {
                    name: event.functionName,
                    args: event.functionArgs,
                  },
                },
              ],
              type: 'function_call',
              timestamp: new Date().toISOString(),
            } as ChatMessage);
            break;

          case EventFunctionResponse:
            // Handle function response
            addMessage({
              id: event.messageId || `function-response-${Date.now()}`,
              role: 'model',
              parts: [
                {
                  functionResponse: {
                    name: event.functionName,
                    response: event.response,
                  },
                },
              ],
              type: 'function_response',
              timestamp: new Date().toISOString(),
            } as ChatMessage);
            break;

          case EventInlineData:
            // Handle inline data (attachments) - add as new message
            addMessage({
              id: event.messageId || `inline-${Date.now()}`,
              role: 'model',
              parts: [],
              type: 'model',
              timestamp: new Date().toISOString(),
              attachments: event.attachments,
            } as ChatMessage);
            break;

          case EventCumulTokenCount:
            // Handle cumulative token count update
            setMessages((prevMessages) => {
              const messageId = event.messageId;
              const cumulTokenCount = event.cumulTokenCount;
              return prevMessages.map((msg) => (msg.id === messageId ? { ...msg, cumulTokenCount } : msg));
            });
            break;

          case EventPendingConfirmation:
            // Handle pending confirmation (tool approval needed)
            setPendingConfirmation(event.data);
            break;

          case EventGenerationChanged:
            // Handle generation changed (environment changes)
            const envChanged = JSON.parse(event.envChangedJson);
            setTemporaryEnvChangeMessage(envChanged);
            break;

          case EventSessionName:
            // Update current session name (for both temporary and regular sessions)
            setCurrentSessionName(event.newName);
            setSessionNameInList({ sessionId: event.sessionId, name: event.newName });
            break;

          case EventPing:
            // Handle ping (keepalive) - no action needed
            break;

          case EventFinish:
            // Handle stream finish - no more events will be sent
            console.log('Stream finished');
            break;

          default:
            console.log('Unhandled SSE event:', event);
        }
      },

      onComplete: () => {
        // Handle stream completion
        // Note: Processing state is now managed by SessionState
      },

      onError: (error: Error | Event | any) => {
        // Handle error
        console.error('SSE error:', error);
        addErrorMessage(`Stream error: ${error?.message || error?.toString() || 'Unknown error'}`);
      },
    }),
    [location, sessionManager, onSessionSwitch, sessions],
  );

  // Action handlers
  const loadSession = useCallback(
    (sessionId: string) => {
      if (!operationManager) return;

      sessionManager.dispatch({ type: 'LOAD_SESSION', sessionId });
      operationManager.handleSessionLoad(sessionId, 50, eventHandlers);
    },
    [sessionManager, operationManager, eventHandlers],
  );

  // Auto-load session from URL (or clear for new/temp pages)
  useEffect(() => {
    const pathname = location.pathname;
    const urlPath = parseURLPath(pathname);

    // For /new or /temp pages, clear chat state (like loading an empty session)
    // But ONLY when actually transitioning from an existing session to /new
    if (urlPath.type === 'new_session') {
      const prevPathname = prevPathnameRef.current;
      const isTransitioningFromSession = prevPathname && parseURLPath(prevPathname).type === 'existing_session';

      // Only reset if we're actually transitioning from a session to /new
      if (isTransitioningFromSession) {
        resetChatSessionState();
      }
      prevPathnameRef.current = pathname;
      return;
    }

    // For existing sessions, load if different from current
    if (urlPath.type === 'existing_session' && urlPath.sessionId && urlPath.sessionId !== sessionManager.sessionId) {
      loadSession(urlPath.sessionId);
    }

    prevPathnameRef.current = pathname;
  }, [location.pathname, sessionManager.sessionId, loadSession, resetChatSessionState]);

  // Computed values (moved before useEffects that use them)
  const isLoading = sessionManager.isLoading;
  const isStreaming = sessionManager.isStreaming;
  const hasMoreMessages = sessionManager.hasMoreMessages;
  const canLoadEarlier = sessionManager.canLoadEarlier;
  const error = sessionManager.error;
  const activeOperation = sessionManager.activeOperation;

  const loadEarlierMessages = useCallback(() => {
    if (!operationManager) return;

    const sessionId = sessionManager.sessionId;
    if (!sessionId) {
      addErrorMessage('No session to load earlier messages');
      return;
    }

    // Get the ID of the earliest message using the current messages state
    // We access messages directly here (not from dependency) to get the current value
    const currentMessages = messages;
    const beforeMessageId = currentMessages.length > 0 ? currentMessages[0].id : '';
    if (!beforeMessageId) {
      console.warn('No messages available to determine beforeMessageId');
      return;
    }

    sessionManager.dispatch({ type: 'LOAD_EARLIER_MESSAGES' });
    operationManager.handleEarlierMessagesLoad(sessionId, beforeMessageId, 50, {
      ...eventHandlers,
      onEvent: (event: SseEvent) => {
        // Handle earlier messages loaded event
        if (event.type === EARLIER_MESSAGES_LOADED && event.data) {
          const data = event.data;

          // Prepend new messages to the existing ones
          setMessages((prevMessages) => [...(data.history || []), ...prevMessages]);

          // Note: hasMore flag is now managed by SessionState
          // The SessionState will be updated by the EARLIER_MESSAGES_LOADED action in the reducer
          return;
        }

        // Handle other events normally
        eventHandlers.onEvent?.(event);
      },
    });
  }, [sessionManager, operationManager, addErrorMessage, eventHandlers, setMessages]);

  const sendMessage = useCallback(
    async (
      content: string,
      files: File[],
      model: ModelInfo | null,
      systemPrompt?: string,
      workspaceId?: string,
      initialRoots?: string[],
      isTemporary?: boolean,
    ) => {
      if (!operationManager) return;

      const attachments = await convertFilesToAttachments(files);
      const params: MessageSendParams = {
        content,
        attachments,
        model,
        systemPrompt,
        workspaceId,
        initialRoots,
        isTemporary,
      };

      // Get current session ID from session manager
      const sessionId = sessionManager.sessionId;

      // Generate UUID for temporary message ID
      const temporaryMessageId = crypto.randomUUID();

      // Add user message to state
      addMessage({
        id: temporaryMessageId,
        role: 'user',
        parts: [{ text: content }],
        type: 'user',
        timestamp: new Date().toISOString(),
        attachments,
      } as ChatMessage);

      // Clear input
      setInputMessage('');
      setSelectedFiles([]);
      setPreserveSelectedFiles([]);

      // Delegate to operation manager
      operationManager.handleMessageSend(params, sessionId, eventHandlers, temporaryMessageId);
    },
    [
      sessionManager,
      operationManager,
      eventHandlers,
      addMessage,
      setInputMessage,
      setSelectedFiles,
      setPreserveSelectedFiles,
    ],
  );

  const switchBranch = useCallback(
    (branchId: string) => {
      if (!operationManager) return;

      const sessionId = sessionManager.sessionId;
      if (!sessionId) {
        addErrorMessage('No session to switch branch');
        return;
      }

      sessionManager.dispatch({ type: 'SWITCH_BRANCH', branchId });
      operationManager.handleBranchSwitch(sessionId, branchId, eventHandlers);
    },
    [sessionManager, operationManager, addErrorMessage, eventHandlers],
  );

  const confirmTool = useCallback(
    (branchId: string, modifiedData?: Record<string, any>) => {
      if (!operationManager) return;

      const sessionId = sessionManager.sessionId;
      if (!sessionId) {
        addErrorMessage('No session to confirm tool');
        return;
      }

      sessionManager.dispatch({ type: 'CONFIRM_TOOL', branchId });
      operationManager.handleToolConfirmation(sessionId, branchId, modifiedData, eventHandlers);
    },
    [sessionManager, operationManager, addErrorMessage, eventHandlers],
  );

  const retryMessage = useCallback(
    async (originalMessageId: string) => {
      if (!operationManager) return;

      const sessionId = sessionManager.sessionId;
      if (!sessionId) {
        addErrorMessage('No session to retry message');
        return;
      }

      setEditingMessageId(null); // Exit editing mode if active

      // Store the original message ID for update when EventAcknowledge arrives
      pendingMessageIdToUpdate.current = originalMessageId;

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

      sessionManager.dispatch({
        type: 'SEND_MESSAGE',
        content: '',
        attachments: [],
        model: selectedModel,
        systemPrompt,
      });
      await operationManager.handleMessageRetry(sessionId, originalMessageId, eventHandlers);
    },
    [
      sessionManager,
      operationManager,
      addErrorMessage,
      selectedModel,
      systemPrompt,
      eventHandlers,
      setEditingMessageId,
      setMessages,
    ],
  );

  const editMessage = useCallback(
    async (originalMessageId: string, editedText: string) => {
      if (!operationManager) return;

      const sessionId = sessionManager.sessionId;
      if (!sessionId) {
        addErrorMessage('No session to edit message');
        return;
      }

      setEditingMessageId(null); // Exit editing mode

      // Store the original message ID for update when EventAcknowledge arrives
      pendingMessageIdToUpdate.current = originalMessageId;

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

      sessionManager.dispatch({
        type: 'SEND_MESSAGE',
        content: editedText,
        attachments: [],
        model: selectedModel,
        systemPrompt,
      });
      await operationManager.handleMessageEdit(
        sessionId,
        originalMessageId,
        editedText,
        selectedModel,
        systemPrompt,
        eventHandlers,
      );
    },
    [
      sessionManager,
      operationManager,
      addErrorMessage,
      selectedModel,
      systemPrompt,
      eventHandlers,
      setEditingMessageId,
      setMessages,
    ],
  );

  const retryError = useCallback(
    async (_errorMessageId: string) => {
      if (!operationManager) return;

      const sessionId = sessionManager.sessionId;
      if (!sessionId) {
        addErrorMessage('No session to retry error');
        return;
      }

      setEditingMessageId(null); // Exit editing mode if active

      // Get current branch ID
      const currentBranchId = primaryBranchId;

      if (!currentBranchId) {
        addErrorMessage('No branch ID available for error retry');
        return;
      }

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

      // Note: Processing state is now managed by SessionState via SEND_MESSAGE action
      sessionManager.dispatch({
        type: 'SEND_MESSAGE',
        content: '',
        attachments: [],
        model: selectedModel,
        systemPrompt,
      });
      await operationManager.handleErrorRetry(sessionId, currentBranchId, eventHandlers);
    },
    [
      sessionManager,
      operationManager,
      addErrorMessage,
      eventHandlers,
      setMessages,
      setEditingMessageId,
      primaryBranchId,
      selectedModel,
      systemPrompt,
    ],
  );

  const updateMessage = useCallback(
    async (messageId: string, editedText: string) => {
      if (!operationManager) return;

      const sessionId = sessionManager.sessionId;
      if (!sessionId) {
        addErrorMessage('No session to update message');
        return;
      }

      setEditingMessageId(null); // Exit editing mode

      // Update message content in the frontend
      setMessages((prevMessages) => {
        const updatedMessages = [...prevMessages];
        const messageIndex = updatedMessages.findIndex((msg) => msg.id === messageId);

        if (messageIndex !== -1) {
          // Update message content
          updatedMessages[messageIndex] = {
            ...updatedMessages[messageIndex],
            parts: [{ text: editedText }],
            aux: {
              ...updatedMessages[messageIndex].aux,
              beforeUpdate: true, // Mark as updated
            },
          };
        }
        return updatedMessages;
      });

      await operationManager.handleMessageUpdate(sessionId, messageId, editedText, eventHandlers);
    },
    [sessionManager, operationManager, addErrorMessage, eventHandlers, setEditingMessageId, setMessages],
  );

  const continueMessage = useCallback(
    async (modelMessageId: string) => {
      if (!operationManager) return;

      const sessionId = sessionManager.sessionId;
      if (!sessionId) {
        addErrorMessage('No session to continue message');
        return;
      }

      setEditingMessageId(null); // Exit editing mode if active

      // Remove messages after the model message being continued
      setMessages((prevMessages) => {
        const updatedMessages = [...prevMessages];
        const messageIndex = updatedMessages.findIndex((msg) => msg.id === modelMessageId);

        if (messageIndex !== -1) {
          // Remove all messages after the continue message
          return updatedMessages.slice(0, messageIndex + 1);
        }
        return prevMessages;
      });

      sessionManager.dispatch({
        type: 'SEND_MESSAGE',
        content: '',
        attachments: [],
        model: selectedModel,
        systemPrompt,
      });
      await operationManager.handleMessageContinue(sessionId, modelMessageId, eventHandlers);
    },
    [
      sessionManager,
      operationManager,
      addErrorMessage,
      selectedModel,
      systemPrompt,
      eventHandlers,
      setEditingMessageId,
      setMessages,
    ],
  );

  const cancelCurrentOperation = useCallback(() => {
    if (operationManager) {
      operationManager.cancelCurrentOperation();
    }
  }, [operationManager]);

  const cancelActiveCall = useCallback(async () => {
    if (operationManager) {
      await operationManager.cancelActiveCall();
    }
  }, [operationManager]);

  return {
    // State
    sessionState: sessionManager.sessionState,
    sessionId: sessionManager.sessionId,
    workspaceId: sessionManager.workspaceId,
    isLoading,
    isStreaming,
    hasMoreMessages,
    canLoadEarlier,
    error,
    activeOperation,
    messages,

    // Actions
    loadSession,
    sendMessage,
    switchBranch,
    confirmTool,
    loadEarlierMessages,
    retryMessage,
    editMessage,
    retryError,
    updateMessage,
    continueMessage,
    cancelCurrentOperation,
    cancelActiveCall,

    // Navigation
    navigateToNewSession: sessionManager.navigateToNewSession,
    navigateToTemporarySession: sessionManager.navigateToTemporarySession,
    navigateToSession: sessionManager.navigateToSession,

    // Utilities
    resetSession: sessionManager.resetSession,
    handleError: sessionManager.handleError,
  };
};
