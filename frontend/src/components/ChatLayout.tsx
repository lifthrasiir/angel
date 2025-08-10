import type React from 'react';
import { useEffect, useRef } from 'react';
import { useAtomValue, useSetAtom } from 'jotai'; // Changed useAtom to useAtomValue
import {
  userEmailAtom,
  chatSessionIdAtom,
  globalPromptsAtom,
  selectedGlobalPromptAtom,
  systemPromptAtom,
} from '../atoms/chatAtoms'; // Removed sessionsAtom, workspaceIdAtom, workspaceNameAtom
import { PredefinedPrompt } from './SystemPromptEditor';
import { useChatSession } from '../hooks/useChatSession';
import useEscToCancel from '../hooks/useEscToCancel';
import { useWorkspaces } from '../hooks/WorkspaceContext';
import ChatArea from './ChatArea';
import LogoAnimation from './LogoAnimation';
import Sidebar from './Sidebar';
import ToastMessage from './ToastMessage';

interface ChatLayoutProps {
  children?: React.ReactNode;
}

const ChatLayout: React.FC<ChatLayoutProps> = ({ children }) => {
  const userEmail = useAtomValue(userEmailAtom); // Changed
  const chatSessionId = useAtomValue(chatSessionIdAtom); // Changed

  const setGlobalPrompts = useSetAtom(globalPromptsAtom);
  const setSelectedGlobalPrompt = useSetAtom(selectedGlobalPromptAtom);
  const setSystemPrompt = useSetAtom(systemPromptAtom);

  const { workspaces, refreshWorkspaces } = useWorkspaces();
  const chatInputRef = useRef<HTMLTextAreaElement>(null);
  const chatAreaRef = useRef<HTMLDivElement>(null);
  const { handleLogin, handleFilesSelected, handleRemoveFile, handleSendMessage, cancelStreamingCall, isStreaming } =
    useChatSession();

  const { toastMessage, setToastMessage } = useEscToCancel({
    isStreaming,
    onCancel: cancelStreamingCall,
  });

  useEffect(() => {
    // ChatArea가 직접 렌더링될 때만 포커스 로직을 적용
    if (!children) {
      if (chatSessionId === null || chatSessionId === undefined) {
        // /new 또는 /w/:workspaceId/new 경로
        chatInputRef.current?.focus();
      } else {
        // 그 외의 경로 (기존 세션)
        chatAreaRef.current?.focus();
      }
    }

    const fetchGlobalPrompts = async () => {
      try {
        const response = await fetch('/api/systemPrompts');
        if (response.ok) {
          const data: PredefinedPrompt[] = await response.json();
          setGlobalPrompts(data);
          if (data.length > 0) {
            setSelectedGlobalPrompt(data[0].label); // Set initial active prompt label for display
            setSystemPrompt(data[0].value); // Set the actual system prompt value
          }
        } else {
          console.error('Failed to fetch global prompts:', response.status, response.statusText);
        }
      } catch (error) {
        console.error('Error fetching global prompts:', error);
      }
    };
    fetchGlobalPrompts();
  }, [chatSessionId, children, chatInputRef, chatAreaRef]);

  return (
    <div style={{ display: 'flex', width: '100vw', height: '100vh', overflow: 'hidden' }}>
      {userEmail ? (
        <>
          <Sidebar workspaces={workspaces} refreshWorkspaces={refreshWorkspaces} />

          {children ? (
            <div
              style={{
                flexGrow: 1,
                display: 'flex',
                flexDirection: 'column',
                position: 'relative',
              }}
            >
              {children}
            </div>
          ) : (
            <ChatArea
              handleSendMessage={handleSendMessage}
              onFilesSelected={handleFilesSelected}
              handleRemoveFile={handleRemoveFile}
              handleCancelStreaming={cancelStreamingCall}
              chatInputRef={chatInputRef}
              chatAreaRef={chatAreaRef}
            />
          )}
          <ToastMessage message={toastMessage} onClose={() => setToastMessage(null)} />
        </>
      ) : (
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            justifyContent: 'center',
            alignItems: 'center',
            height: '100vh',
            width: '100%',
            fontSize: '1.2em',
          }}
        >
          <LogoAnimation width="100px" height="100px" color="#007bff" />
          <p style={{ marginTop: '20px' }}>Please log in to use the chat application.</p>
          <button
            onClick={handleLogin}
            style={{ padding: '10px 20px', fontSize: '1em', cursor: 'pointer' }}
            aria-label="Login to the chat application"
          >
            Login
          </button>
        </div>
      )}
    </div>
  );
};

export default ChatLayout;
