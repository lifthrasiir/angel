import React from 'react';
import Sidebar from './Sidebar';
import ChatArea from './ChatArea';
import LogoAnimation from './LogoAnimation';
import ToastMessage from './ToastMessage'; // Import ToastMessage
import { useChatSession } from '../hooks/useChatSession';
import { useChat } from '../hooks/ChatContext';
import useEscToCancel from '../hooks/useEscToCancel'; // Import the new hook
import {
  SET_INPUT_MESSAGE,
  SET_SYSTEM_PROMPT,

} from '../hooks/chatReducer';

const ChatLayout: React.FC = () => {
  const { dispatch } = useChat();
  const {
    userEmail,
    chatSessionId,
    messages,
    inputMessage,
    sessions,
    lastAutoDisplayedThoughtId,
    isStreaming,
    systemPrompt,
    isSystemPromptEditing,
    selectedFiles,
    handleLogin,
    handleFilesSelected,
    handleRemoveFile,
    handleSendMessage,
    fetchSessions,
    cancelStreamingCall,
  } = useChatSession();

  const { toastMessage, setToastMessage } = useEscToCancel({
    isStreaming,
    onCancel: cancelStreamingCall,
  });

  return (
    <div style={{ display: 'flex', height: '100vh', overflow: 'hidden' }}>
      {userEmail ? (
        <>
          <Sidebar
            sessions={sessions}
            chatSessionId={chatSessionId}
            fetchSessions={fetchSessions}
          />

          <ChatArea
            isLoggedIn={!!userEmail}
            messages={messages}
            lastAutoDisplayedThoughtId={lastAutoDisplayedThoughtId}
            systemPrompt={systemPrompt}
            setSystemPrompt={(prompt) => dispatch({ type: SET_SYSTEM_PROMPT, payload: prompt })}
            isSystemPromptEditing={isSystemPromptEditing}
            chatSessionId={chatSessionId}
            
            inputMessage={inputMessage}
            setInputMessage={(message) => dispatch({ type: SET_INPUT_MESSAGE, payload: message })}
            handleSendMessage={handleSendMessage}
            isStreaming={isStreaming}
            onFilesSelected={handleFilesSelected}
            selectedFiles={selectedFiles}
            handleRemoveFile={handleRemoveFile}
            handleCancelStreaming={cancelStreamingCall}
          />
          <ToastMessage message={toastMessage} onClose={() => setToastMessage(null)} />
        </>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', justifyContent: 'center', alignItems: 'center', height: '100vh', width: '100%', fontSize: '1.2em' }}>
          <LogoAnimation width="100px" height="100px" color="#007bff" />
          <p style={{ marginTop: '20px' }}>Please log in to use the chat application.</p>
          <button onClick={handleLogin} style={{ padding: '10px 20px', fontSize: '1em', cursor: 'pointer' }}>Login</button>
        </div>
      )}
    </div>
  );
};

export default ChatLayout;
