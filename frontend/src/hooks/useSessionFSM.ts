import { useCallback, useEffect, useMemo } from 'react';
import { useSetAtom, useAtomValue } from 'jotai';
import { useLocation } from 'react-router-dom';
import type { ChatMessage } from '../types/chat';
import { ModelInfo } from '../api/models';
import { convertFilesToAttachments } from '../utils/fileHandler';
import { useSessionManagerContext } from './SessionManagerContext';
import {
  addErrorMessageAtom,
  addMessageAtom,
  resetChatSessionStateAtom,
  isSystemPromptEditingAtom,
  messagesAtom,
  primaryBranchIdAtom,
  selectedFilesAtom,
  selectedModelAtom,
  systemPromptAtom,
  updateAgentMessageAtom,
  pendingConfirmationAtom,
  temporaryEnvChangeMessageAtom,
  preserveSelectedFilesAtom,
  setSessionNameAtom,
  inputMessageAtom,
} from '../atoms/chatAtoms';
import type { MessageSendParams, OperationEventHandlers } from '../managers/SessionOperationManager';
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
  type SseEvent,
} from '../types/events';

interface UseSessionFSMProps {
  onSessionSwitch?: () => void;
}

export const useSessionFSM = ({ onSessionSwitch }: UseSessionFSMProps = {}) => {
  // Get session manager context
  const sessionManager = useSessionManagerContext();
  const operationManager = sessionManager?.operationManager;
  const location = useLocation();

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
  const setSessionName = useSetAtom(setSessionNameAtom);
  const setInputMessage = useSetAtom(inputMessageAtom);

  const messages = useAtomValue(messagesAtom);
  const selectedModel = useAtomValue(selectedModelAtom);
  const systemPrompt = useAtomValue(systemPromptAtom);
  const primaryBranchId = useAtomValue(primaryBranchIdAtom);

  // Event handlers for operation manager
  const eventHandlers: OperationEventHandlers = useMemo(
    () => ({
      onInitialState: (data: any) => {
        console.log('Initial state received:', data);

        // Reset chat state for new session
        if (!data.isCallActive) {
          resetChatSessionState();
        }

        // Update messages from initial state
        // Only update messages if we're not actively streaming (to avoid overwriting streaming messages)
        setMessages((prevMessages) => {
          if (data.isCallActive && prevMessages.length > 0) {
            // Already streaming and have messages, don't overwrite
            console.log('Skipping message update - streaming in progress with', prevMessages.length, 'messages');
            return prevMessages;
          } else if (data.messages && data.messages.length > 0) {
            console.log('Setting messages:', data.messages.length, 'messages');
            return data.messages;
          } else {
            console.log('No messages to set');
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

        // Note: hasMoreMessages and processing state are now managed by SessionState

        // Handle pending confirmation
        if (data.pendingConfirmation) {
          setPendingConfirmation(data.pendingConfirmation);
        }

        // Handle temporary env change message
        if (data.temporaryEnvChangeMessage) {
          setTemporaryEnvChangeMessage(data.temporaryEnvChangeMessage);
        }

        // Note: Session load completion is now managed by SessionState
        console.log('Session load handled by SessionState');

        // If this is a new session creation (URL is /new or /temp), navigate to the session URL
        const pathname = location.pathname;
        const isNewSessionURL =
          pathname === '/new' || pathname === '/temp' || pathname.match(/^\/w\/[^\/]+\/(new|temp)$/) !== null;

        if (isNewSessionURL && data.sessionId && data.sessionId !== sessionManager.sessionId) {
          console.log('Navigating to new session:', data.sessionId);
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
            console.log('Workspace hint received:', (event as any).workspaceId);
            // Workspace ID is already set in the initial state, so we don't need to do anything here
            break;

          case EventAcknowledge:
            // Handle message acknowledgement (message was sent successfully)
            console.log('Message acknowledged:', (event as any).messageId);
            // No UI update needed - message already added optimistically
            break;

          case EventComplete:
            // Handle stream completion
            console.log('Stream completed');
            // Note: Processing state is now managed by SessionState
            break;

          case EventThought:
            // Handle thought message
            addMessage({
              id: (event as any).messageId || `thought-${Date.now()}`,
              role: 'model',
              parts: [{ text: (event as any).thoughtText }],
              type: 'thought',
              timestamp: new Date().toISOString(),
            } as ChatMessage);
            break;

          case EventModelMessage:
            // Handle model message
            if ((event as any).text) {
              updateAgentMessage({
                messageId: (event as any).messageId,
                text: (event as any).text,
                modelName: (event as any).modelName,
              });
            }
            break;

          case EventFunctionCall:
            // Handle function call
            addMessage({
              id: (event as any).messageId || `function-${Date.now()}`,
              role: 'model',
              parts: [
                {
                  functionCall: {
                    name: (event as any).functionName,
                    args: (event as any).functionArgs,
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
              id: (event as any).messageId || `function-response-${Date.now()}`,
              role: 'model',
              parts: [
                {
                  functionResponse: {
                    name: (event as any).functionName,
                    response: (event as any).functionResponse,
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
              id: (event as any).messageId || `inline-${Date.now()}`,
              role: 'model',
              parts: [],
              type: 'model',
              timestamp: new Date().toISOString(),
              attachments: (event as any).attachments,
            } as ChatMessage);
            break;

          case EventCumulTokenCount:
            // Handle cumulative token count update
            setMessages((prevMessages) => {
              const messageId = (event as any).messageId;
              const cumulTokenCount = (event as any).cumulTokenCount;
              return prevMessages.map((msg) => (msg.id === messageId ? { ...msg, cumulTokenCount } : msg));
            });
            break;

          case EventPendingConfirmation:
            // Handle pending confirmation (tool approval needed)
            const confirmationData = JSON.parse((event as any).data);
            setPendingConfirmation(confirmationData);
            break;

          case EventGenerationChanged:
            // Handle generation changed (environment changes)
            const envChanged = JSON.parse((event as any).envChangedJson);
            setTemporaryEnvChangeMessage(envChanged);
            break;

          case EventSessionName:
            // Handle session name update
            if ((event as any).newName) {
              setSessionName((event as any).newName);
            }
            break;

          case EventPing:
            // Handle ping (keepalive) - no action needed
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
    [location, sessionManager, onSessionSwitch],
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

  // Auto-load session from URL
  useEffect(() => {
    // Check if URL contains sessionId (/:sessionId pattern)
    const pathname = location.pathname;
    const sessionIdMatch = pathname.match(/^\/([^\/]+)$/);
    const urlSessionId = sessionIdMatch ? sessionIdMatch[1] : null;

    console.log('URL effect triggered:', { pathname, urlSessionId, sessionManagerId: sessionManager.sessionId });

    // Reset chat state only when explicitly on /new or /temp WITHOUT a session
    // This prevents resetting during URL transition after session creation
    if (urlSessionId === 'new' || urlSessionId === 'temp') {
      // Only reset if we don't have a session yet (to avoid resetting during URL transition)
      if (sessionManager.sessionId === null) {
        console.log('Resetting chat state for new session');
        resetChatSessionState();
      }
      return;
    }

    // Only proceed if we have a sessionId and it's different from current session
    if (urlSessionId && urlSessionId !== sessionManager.sessionId) {
      console.log('Loading session from URL:', urlSessionId);
      loadSession(urlSessionId);
    }
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
        if ((event as any).type === 'EARLIER_MESSAGES_LOADED' && (event as any).data) {
          const data = (event as any).data;

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
      primaryBranchId?: string,
      initialRoots?: string[],
      beforeMessageId?: string,
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
        primaryBranchId,
        initialRoots,
        beforeMessageId,
        isTemporary,
      };

      // Get current session ID from session manager
      const sessionId = sessionManager.sessionId;

      // Add user message to state
      addMessage({
        id: `user-${Date.now()}`,
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
      operationManager.handleMessageSend(params, sessionId, eventHandlers);
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
      operationManager.handleBranchSwitch(sessionId, branchId);
    },
    [sessionManager, operationManager, addErrorMessage],
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

      // Find the original message to get its content
      const messageToRetry = messages.find((msg) => msg.id === originalMessageId);
      if (!messageToRetry) {
        addErrorMessage('Original message not found');
        return;
      }

      const content = messageToRetry.parts.find((part) => part.text)?.text || '';
      sessionManager.dispatch({
        type: 'SEND_MESSAGE',
        content,
        attachments: [],
        model: selectedModel,
        systemPrompt,
      });
      await operationManager.handleMessageRetry(
        sessionId,
        originalMessageId,
        content,
        selectedModel,
        systemPrompt,
        eventHandlers,
      );
    },
    [sessionManager, operationManager, addErrorMessage, messages, selectedModel, systemPrompt, eventHandlers],
  );

  const editMessage = useCallback(
    async (originalMessageId: string, editedText: string) => {
      if (!operationManager) return;

      const sessionId = sessionManager.sessionId;
      if (!sessionId) {
        addErrorMessage('No session to edit message');
        return;
      }

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
    [sessionManager, operationManager, addErrorMessage, selectedModel, systemPrompt, eventHandlers],
  );

  const retryError = useCallback(
    async (_errorMessageId: string) => {
      if (!operationManager) return;

      const sessionId = sessionManager.sessionId;
      if (!sessionId) {
        addErrorMessage('No session to retry error');
        return;
      }

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
    [sessionManager, operationManager, addErrorMessage, eventHandlers, setMessages],
  );

  const cancelCurrentOperation = useCallback(() => {
    if (operationManager) {
      operationManager.cancelCurrentOperation();
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
    cancelCurrentOperation,

    // Navigation
    navigateToNewSession: sessionManager.navigateToNewSession,
    navigateToTemporarySession: sessionManager.navigateToTemporarySession,
    navigateToSession: sessionManager.navigateToSession,

    // Utilities
    resetSession: sessionManager.resetSession,
    handleError: sessionManager.handleError,
  };
};
