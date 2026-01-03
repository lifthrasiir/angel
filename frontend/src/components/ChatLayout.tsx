import type React from 'react';
import { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { apiFetch } from '../api/apiClient';
import { useAtom, useSetAtom } from 'jotai';
import { globalPromptsAtom, selectedGlobalPromptAtom } from '../atoms/modelAtoms';
import { toastMessageAtom } from '../atoms/uiAtoms';
import { sessionsAtom } from '../atoms/chatAtoms';
import { PredefinedPrompt } from './chat/SystemPromptEditor';
import { useChatSession } from '../hooks/useChatSession';
import { useSessionFSM } from '../hooks/useSessionFSM';
import { useSessionManagerContext } from '../hooks/SessionManagerContext';
import { getSessionId } from '../utils/sessionStateHelpers';
import useEscToCancel from '../hooks/useEscToCancel';
import { useWorkspaces } from '../hooks/WorkspaceContext';
import ChatArea from './chat/ChatArea';
import ChatHeader from './chat/ChatHeader';
import Sidebar from './sidebar/Sidebar';
import ToastMessage from './ToastMessage';
import { isTextInputKey } from '../utils/navigationKeys';
interface ChatLayoutProps {
  children?: React.ReactNode;
  isTemporary?: boolean;
}

const ChatLayout: React.FC<ChatLayoutProps> = ({ children, isTemporary = false }) => {
  const navigate = useNavigate();
  // Use shared sessionManager from context
  const sessionManager = useSessionManagerContext();
  const chatSessionId = getSessionId(sessionManager.sessionState);

  const setGlobalPrompts = useSetAtom(globalPromptsAtom);
  const setSelectedGlobalPrompt = useSetAtom(selectedGlobalPromptAtom);
  const [sessions, setSessions] = useAtom(sessionsAtom);

  const { workspaces, refreshWorkspaces } = useWorkspaces();
  const chatInputRef = useRef<HTMLTextAreaElement>(null);
  const chatAreaRef = useRef<HTMLDivElement>(null);

  // Mobile sidebar state
  const [isMobileSidebarOpen, setIsMobileSidebarOpen] = useState(false);

  // Get workspace info from sessionFSM
  const sessionFSM = useSessionFSM();
  const { workspaceId: sessionWorkspaceId } = sessionFSM;
  const {
    handleFilesSelected,
    handleRemoveFile,
    handleFileResizeStateChange,
    handleFileProcessingStateChange,
    handleFileResized,
    handleSendMessage,
    cancelStreamingCall,
    cancelActiveStreams,
    sendConfirmation,
    isProcessing,
    handleEditMessage,
    handleBranchSwitch,
    handleRetryMessage,
    handleRetryError,
    isSendDisabledByResizing,
  } = useChatSession(isTemporary);

  const [toastMessage, setToastMessage] = useAtom(toastMessageAtom);

  // Session menu handlers
  const handleSessionRename = (sessionId: string) => {
    const session = sessions.find((s) => s.id === sessionId);
    if (session) {
      const newName = window.prompt('Enter new session name:', session.name || '');
      if (newName !== null) {
        apiFetch(`/api/chat/${sessionId}/name`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name: newName }),
        })
          .then(() => {
            setSessions(sessions.map((s) => (s.id === sessionId ? { ...s, name: newName } : s)));
          })
          .catch((error) => {
            console.error('Error updating session name:', error);
          });
      }
    }
  };

  const handleSessionDelete = async (sessionId: string) => {
    if (window.confirm('Are you sure you want to delete this session?')) {
      try {
        await apiFetch(`/api/chat/${sessionId}`, { method: 'DELETE' });
        setSessions(sessions.filter((s) => s.id !== sessionId));
        if (chatSessionId === sessionId) {
          navigate(sessionWorkspaceId ? `/w/${sessionWorkspaceId}/new` : '/new');
        }
      } catch (error) {
        console.error('Error deleting session:', error);
      }
    }
  };

  useEscToCancel({
    isProcessing,
    onCancel: cancelStreamingCall,
  });

  useEffect(() => {
    // Apply focus logic only when ChatArea is rendered directly
    if (!children) {
      // Always focus on chat input for all sessions
      if (chatInputRef.current) {
        chatInputRef.current.focus();
      } else {
        // If ref is not ready, wait a bit and try again
        setTimeout(() => {
          chatInputRef.current?.focus();
        }, 100);
      }
    }

    const fetchGlobalPrompts = async () => {
      try {
        const response = await apiFetch('/api/systemPrompts');
        if (response.ok) {
          const data: PredefinedPrompt[] = await response.json();
          setGlobalPrompts(data);
          if (data.length > 0) {
            setSelectedGlobalPrompt(data[0].label); // Set initial active prompt label for display
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

  // Global keyboard event listener for auto-focusing chat input
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Only apply this logic when ChatArea is rendered (not children)
      if (children) return;

      // Check if the target is not a textarea or input
      const target = e.target as HTMLElement;
      if (target.tagName === 'TEXTAREA' || target.tagName === 'INPUT') {
        return;
      }

      // Check if this is a text input key (no modifiers)
      if (isTextInputKey(e)) {
        // Focus the chat input
        chatInputRef.current?.focus();
      }
    };

    // Add event listener to window
    window.addEventListener('keydown', handleKeyDown);

    // Clean up
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [children, chatInputRef]);

  return (
    <div style={{ display: 'flex', width: '100vw', height: '100vh', overflow: 'hidden' }}>
      <Sidebar
        workspaces={workspaces}
        refreshWorkspaces={refreshWorkspaces}
        isMobileSidebarOpen={isMobileSidebarOpen}
        onSetMobileSidebarOpen={setIsMobileSidebarOpen}
      />

      {children ? (
        <div
          style={{
            flexGrow: 1,
            display: 'flex',
            flexDirection: 'column',
            position: 'relative',
            marginLeft: '0', // No margin needed as sidebar handles positioning
          }}
        >
          {children}
        </div>
      ) : (
        <ChatArea
          handleSendMessage={handleSendMessage}
          onFilesSelected={handleFilesSelected}
          handleRemoveFile={handleRemoveFile}
          handleFileResizeStateChange={handleFileResizeStateChange}
          handleFileProcessingStateChange={handleFileProcessingStateChange}
          handleFileResized={handleFileResized}
          handleCancelStreaming={cancelStreamingCall}
          handleCancelMessageStreams={cancelActiveStreams}
          chatInputRef={chatInputRef}
          chatAreaRef={chatAreaRef}
          sendConfirmation={sendConfirmation}
          handleEditMessage={handleEditMessage}
          handleRetryMessage={handleRetryMessage}
          handleRetryError={handleRetryError}
          handleBranchSwitch={handleBranchSwitch}
          isSendDisabledByResizing={isSendDisabledByResizing}
          chatHeader={
            <ChatHeader
              workspaces={workspaces}
              onSessionRename={handleSessionRename}
              onSessionDelete={handleSessionDelete}
              onToggleSidebar={() => setIsMobileSidebarOpen(!isMobileSidebarOpen)}
            />
          }
        />
      )}
      <ToastMessage message={toastMessage} onClose={() => setToastMessage(null)} />
    </div>
  );
};

export default ChatLayout;
