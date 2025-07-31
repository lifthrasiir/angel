import { useNavigate } from 'react-router-dom';
import { ChatMessage, FileAttachment } from '../types/chat';
import { 
  sendMessage, 
  processStreamResponse, 
  StreamEventHandlers 
} from '../utils/messageHandler';
import { convertFilesToAttachments } from '../utils/fileHandler';
import { 
  SET_INPUT_MESSAGE,
  SET_SELECTED_FILES,
  SET_IS_STREAMING,
  SET_LAST_AUTO_DISPLAYED_THOUGHT_ID,
  SET_CHAT_SESSION_ID,
  
  SET_IS_SYSTEM_PROMPT_EDITING,
  ADD_MESSAGE,
  UPDATE_AGENT_MESSAGE,
} from './chatReducer';
import { ChatAction } from './chatReducer';

interface UseMessageSendingProps {
  inputMessage: string;
  selectedFiles: File[];
  chatSessionId: string | null;
  systemPrompt: string;
  dispatch: React.Dispatch<ChatAction>;
  handleLoginRedirect: () => void;
  loadSessions: () => Promise<void>;
}

export const useMessageSending = ({
  inputMessage,
  selectedFiles,
  chatSessionId,
  systemPrompt,
  dispatch,
  handleLoginRedirect,
  loadSessions,
}: UseMessageSendingProps) => {
  const navigate = useNavigate();

  

  const handleSendMessage = async () => {
    if (!inputMessage.trim() && selectedFiles.length === 0) return;

    dispatch({ type: SET_IS_STREAMING, payload: true });

    try {
      const attachments: FileAttachment[] = await convertFilesToAttachments(selectedFiles);

      const userMessage: ChatMessage = { 
        id: crypto.randomUUID(), 
        role: 'user', 
        parts: [{ text: inputMessage }], 
        type: 'user', 
        attachments: attachments 
      };
      dispatch({ type: ADD_MESSAGE, payload: userMessage });
      dispatch({ type: SET_INPUT_MESSAGE, payload: '' });
      dispatch({ type: SET_SELECTED_FILES, payload: [] });

      let agentMessageId = crypto.randomUUID();
      dispatch({ type: ADD_MESSAGE, payload: { id: agentMessageId, role: 'model', parts: [{ text: '' }], type: 'model' } as ChatMessage });

      if (chatSessionId === null) {
        dispatch({ type: SET_IS_SYSTEM_PROMPT_EDITING, payload: false });
      }

      const response = await sendMessage(inputMessage, attachments, chatSessionId, systemPrompt);

      if (response.status === 401) {
        handleLoginRedirect();
        return;
      }

      if (!response.ok) {
        dispatch({ type: UPDATE_AGENT_MESSAGE, payload: {
          id: agentMessageId,
          role: 'model',
          parts: [{ text: 'Failed to send message or receive stream.' }],
          type: 'model_error',
        } as ChatMessage });
        return;
      }

      let currentAgentText = '';

      const handlers: StreamEventHandlers = {
        onMessage: (text: string) => {
          currentAgentText += text;
          dispatch({ type: UPDATE_AGENT_MESSAGE, payload: { id: agentMessageId, role: 'model', parts: [{ text: currentAgentText }], type: 'model' } as ChatMessage });
          dispatch({ type: SET_LAST_AUTO_DISPLAYED_THOUGHT_ID, payload: null });
        },
        onThought: (thoughtText: string, thoughtId: string) => {
          dispatch({ type: ADD_MESSAGE, payload: { id: thoughtId, role: 'model', parts: [{ text: thoughtText }], type: 'thought' } as ChatMessage });
          dispatch({ type: SET_LAST_AUTO_DISPLAYED_THOUGHT_ID, payload: thoughtId });
        },
        onFunctionCall: (functionName: string, functionArgs: any) => {
          const message: ChatMessage = { 
            id: crypto.randomUUID(), 
            role: 'model', 
            parts: [{ functionCall: { name: functionName, args: functionArgs } }], 
            type: 'function_call' 
          };
          dispatch({ type: ADD_MESSAGE, payload: message });
          dispatch({ type: SET_LAST_AUTO_DISPLAYED_THOUGHT_ID, payload: null });
        },
        onFunctionResponse: (functionResponse: any) => {
          const message: ChatMessage = { 
            id: crypto.randomUUID(), 
            role: 'user', 
            parts: [{ functionResponse: { response: functionResponse } }], 
            type: 'function_response' 
          };
          dispatch({ type: ADD_MESSAGE, payload: message });
          dispatch({ type: SET_LAST_AUTO_DISPLAYED_THOUGHT_ID, payload: null });
        },
        onSessionUpdate: (newSessionId: string) => {
          dispatch({ type: SET_CHAT_SESSION_ID, payload: newSessionId });
          navigate(`/${newSessionId}`, { replace: true });
        },
        onSessionNameUpdate: () => {
          loadSessions();
        },
        onEnd: () => {
          dispatch({ type: SET_LAST_AUTO_DISPLAYED_THOUGHT_ID, payload: null });
        }
      };

      await processStreamResponse(response, handlers);
      loadSessions();
      dispatch({ type: SET_IS_STREAMING, payload: false });
      dispatch({ type: SET_SELECTED_FILES, payload: [] });

    } catch (error) {
      console.error('Error sending message or receiving stream:', error);
      dispatch({ type: ADD_MESSAGE, payload: {
        id: crypto.randomUUID(),
        role: 'model',
        parts: [{ text: 'Error sending message or receiving stream.' }],
        type: 'model_error',
      } as ChatMessage });
    } finally {
      dispatch({ type: SET_IS_STREAMING, payload: false });
      loadSessions();
      dispatch({ type: SET_SELECTED_FILES, payload: [] });
    }
  };

  return { handleSendMessage };
};
