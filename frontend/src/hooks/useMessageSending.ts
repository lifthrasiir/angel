import { useNavigate, useParams } from 'react-router-dom';
import type { ChatMessage, FileAttachment } from '../types/chat';
import { convertFilesToAttachments } from '../utils/fileHandler';
import { processStreamResponse, type StreamEventHandlers, sendMessage } from '../utils/messageHandler';
import {
  ADD_ERROR_MESSAGE,
  ADD_MESSAGE,
  type ChatAction,
  SET_CHAT_SESSION_ID,
  SET_INPUT_MESSAGE,
  SET_IS_STREAMING,
  SET_IS_SYSTEM_PROMPT_EDITING,
  SET_LAST_AUTO_DISPLAYED_THOUGHT_ID,
  SET_SELECTED_FILES,
  SET_SESSION_NAME,
  SET_SYSTEM_PROMPT,
  SET_PRIMARY_BRANCH_ID,
  UPDATE_AGENT_MESSAGE,
  UPDATE_USER_MESSAGE_ID,
} from './chatReducer';

interface UseMessageSendingProps {
  inputMessage: string;
  selectedFiles: File[];
  chatSessionId: string | null;
  systemPrompt: string;
  dispatch: React.Dispatch<ChatAction>;
  handleLoginRedirect: () => void;
  primaryBranchId: string;
}

export const useMessageSending = ({
  inputMessage,
  selectedFiles,
  chatSessionId,
  systemPrompt,
  dispatch,
  handleLoginRedirect,
  primaryBranchId,
}: UseMessageSendingProps) => {
  const navigate = useNavigate();
  const { workspaceId } = useParams<{ workspaceId?: string }>();

  const handleSendMessage = async () => {
    if (!inputMessage.trim() && selectedFiles.length === 0) return;

    dispatch({ type: SET_IS_STREAMING, payload: true });

    try {
      const attachments: FileAttachment[] = await convertFilesToAttachments(selectedFiles);

      const temporaryUserMessageId = crypto.randomUUID();
      const userMessage: ChatMessage = {
        id: temporaryUserMessageId,
        role: 'user',
        parts: [{ text: inputMessage }],
        type: 'user',
        attachments: attachments,
      };
      dispatch({ type: ADD_MESSAGE, payload: userMessage });
      dispatch({ type: SET_INPUT_MESSAGE, payload: '' });
      dispatch({ type: SET_SELECTED_FILES, payload: [] });

      if (chatSessionId === null) {
        dispatch({ type: SET_IS_SYSTEM_PROMPT_EDITING, payload: false });
      }

      const response = await sendMessage(
        inputMessage,
        attachments,
        chatSessionId,
        systemPrompt,
        workspaceId,
        primaryBranchId,
      );

      if (response.status === 401) {
        handleLoginRedirect();
        return;
      }

      if (!response.ok) {
        const errorMessage =
          response.status === 499 ? 'Request cancelled by user.' : 'Failed to send message or receive stream.';
        dispatch({ type: ADD_ERROR_MESSAGE, payload: errorMessage });
        return;
      }

      const handlers: StreamEventHandlers = {
        onMessage: (messageId: string, text: string) => {
          dispatch({ type: UPDATE_AGENT_MESSAGE, payload: { messageId, text } });
          dispatch({ type: SET_LAST_AUTO_DISPLAYED_THOUGHT_ID, payload: null });
        },
        onThought: (messageId: string, thoughtText: string) => {
          dispatch({
            type: ADD_MESSAGE,
            payload: {
              id: messageId,
              role: 'model',
              parts: [{ text: thoughtText }],
              type: 'thought',
            } as ChatMessage,
          });
          dispatch({
            type: SET_LAST_AUTO_DISPLAYED_THOUGHT_ID,
            payload: messageId,
          });
        },
        onFunctionCall: (messageId: string, functionName: string, functionArgs: any) => {
          const message: ChatMessage = {
            id: messageId,
            role: 'model',
            parts: [{ functionCall: { name: functionName, args: functionArgs } }],
            type: 'function_call',
          };
          dispatch({ type: ADD_MESSAGE, payload: message });
          dispatch({ type: SET_LAST_AUTO_DISPLAYED_THOUGHT_ID, payload: null });
        },
        onFunctionResponse: (messageId: string, functionName: string, functionResponse: any) => {
          const message: ChatMessage = {
            id: messageId,
            role: 'user',
            parts: [{ functionResponse: { name: functionName, response: functionResponse } }],
            type: 'function_response',
          };
          dispatch({ type: ADD_MESSAGE, payload: message });
          dispatch({ type: SET_LAST_AUTO_DISPLAYED_THOUGHT_ID, payload: null });
        },
        onSessionStart: (sessionId: string, systemPrompt: string, primaryBranchId: string) => {
          dispatch({ type: SET_CHAT_SESSION_ID, payload: sessionId });
          dispatch({ type: SET_SYSTEM_PROMPT, payload: systemPrompt });
          dispatch({ type: SET_PRIMARY_BRANCH_ID, payload: primaryBranchId });
          navigate(workspaceId ? `/w/${workspaceId}/${sessionId}` : `/${sessionId}`, { replace: true });
        },
        onSessionNameUpdate: (sessionId: string, newName: string) => {
          dispatch({
            type: SET_SESSION_NAME,
            payload: { sessionId, name: newName },
          });
        },
        onEnd: () => {
          dispatch({ type: SET_LAST_AUTO_DISPLAYED_THOUGHT_ID, payload: null });
          dispatch({ type: SET_IS_STREAMING, payload: false });
        },
        onError: (errorData: string) => {
          dispatch({ type: ADD_ERROR_MESSAGE, payload: errorData });
        },
        onAcknowledge: (messageId: string) => {
          dispatch({ type: UPDATE_USER_MESSAGE_ID, payload: { temporaryId: userMessage.id, newId: messageId } });
        },
      };

      const { qReceived, nReceived } = await processStreamResponse(response, handlers);

      if (!qReceived || !nReceived) {
        // This indicates a backend bug or unexpected stream termination
        console.error('Backend bug: Stream ended without receiving both Q and N events.', { qReceived, nReceived });
        dispatch({
          type: ADD_ERROR_MESSAGE,
          payload: 'An unexpected error occurred: Stream did not finalize correctly.',
        });
      }
    } catch (error) {
      console.error('Error sending message or receiving stream:', error);
      dispatch({
        type: ADD_ERROR_MESSAGE,
        payload: 'Error sending message or receiving stream.',
      });
    } finally {
      dispatch({ type: SET_IS_STREAMING, payload: false });
    }
  };

  const cancelStreamingCall = async () => {
    if (!chatSessionId) return;

    try {
      const response = await fetch(`/api/chat/${chatSessionId}/call`, {
        method: 'DELETE',
      });

      if (response.ok) {
        console.log(`Streaming call for session ${chatSessionId} cancelled.`);
        dispatch({ type: SET_IS_STREAMING, payload: false });
        dispatch({
          type: ADD_ERROR_MESSAGE,
          payload: 'Request cancelled by user.',
        });
      } else {
        console.error(
          `Failed to cancel streaming call for session ${chatSessionId}:`,
          response.status,
          response.statusText,
        );
        dispatch({
          type: ADD_ERROR_MESSAGE,
          payload: `Failed to cancel request: ${response.status} ${response.statusText}`,
        });
      }
    } catch (error) {
      console.error(`Error cancelling streaming call for session ${chatSessionId}:`, error);
    }
  };

  return { handleSendMessage, cancelStreamingCall };
};
