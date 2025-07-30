import React from 'react';
import Sidebar from './Sidebar';
import ChatArea from './ChatArea';
import LogoAnimation from './LogoAnimation';
import { useChatSession } from '../hooks/useChatSession';

const ChatLayout: React.FC = () => {
  const {
    userEmail,
    chatSessionId,
    messages,
    inputMessage,
    sessions,
    setSessions,
    lastAutoDisplayedThoughtId,
    isStreaming,
    systemPrompt,
    isSystemPromptEditing,
    selectedFiles,
    setInputMessage,
    setSystemPrompt,
    setIsSystemPromptEditing,
    handleLogin,
    handleFilesSelected,
    handleRemoveFile,
    handleSendMessage,
    fetchSessions,
  } = useChatSession();

  return (
    <div style={{ display: 'flex', height: '100vh', overflow: 'hidden' }}>
      {userEmail ? (
        <>
          <Sidebar
            sessions={sessions}
            setSessions={setSessions}
            chatSessionId={chatSessionId}
            fetchSessions={fetchSessions}
          />

          <ChatArea
            isLoggedIn={!!userEmail}
            messages={messages}
            lastAutoDisplayedThoughtId={lastAutoDisplayedThoughtId}
            systemPrompt={systemPrompt}
            setSystemPrompt={setSystemPrompt}
            isSystemPromptEditing={isSystemPromptEditing}
            setIsSystemPromptEditing={setIsSystemPromptEditing}
            inputMessage={inputMessage}
            setInputMessage={setInputMessage}
            handleSendMessage={handleSendMessage}
            isStreaming={isStreaming}
            onFilesSelected={handleFilesSelected}
            selectedFiles={selectedFiles}
            handleRemoveFile={handleRemoveFile}
          />
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
