import type React from 'react';
import { useEffect, useRef } from 'react';
import { apiFetch } from '../api/apiClient';
import { useSetAtom } from 'jotai';
import { globalPromptsAtom, selectedGlobalPromptAtom } from '../atoms/chatAtoms';
import { PredefinedPrompt } from './SystemPromptEditor';
import { useChatSession } from '../hooks/useChatSession';
import { useSessionManagerContext } from '../hooks/SessionManagerContext';
import { getSessionId } from '../utils/sessionStateHelpers';
import useEscToCancel from '../hooks/useEscToCancel';
import { useWorkspaces } from '../hooks/WorkspaceContext';
import ChatArea from './ChatArea';
import Sidebar from './Sidebar';
import ToastMessage from './ToastMessage';
import { isTextInputKey } from '../utils/navigationKeys';
interface ChatLayoutProps {
  children?: React.ReactNode;
}

const ChatLayout: React.FC<ChatLayoutProps> = ({ children }) => {
  // Use shared sessionManager from context
  const sessionManager = useSessionManagerContext();
  const chatSessionId = getSessionId(sessionManager.sessionState);

  const setGlobalPrompts = useSetAtom(globalPromptsAtom);
  const setSelectedGlobalPrompt = useSetAtom(selectedGlobalPromptAtom);

  const { workspaces, refreshWorkspaces } = useWorkspaces();
  const chatInputRef = useRef<HTMLTextAreaElement>(null);
  const chatAreaRef = useRef<HTMLDivElement>(null);
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
  } = useChatSession();

  const { toastMessage, setToastMessage } = useEscToCancel({
    isProcessing,
    onCancel: cancelStreamingCall,
  });

  useEffect(() => {
    // Apply focus logic only when ChatArea is rendered directly
    if (!children) {
      if (chatSessionId === null || chatSessionId === undefined) {
        // For /new or /w/:workspaceId/new paths
        chatInputRef.current?.focus();
      } else {
        // For other paths (existing sessions)
        chatAreaRef.current?.focus();
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
      <Sidebar workspaces={workspaces} refreshWorkspaces={refreshWorkspaces} />

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
        />
      )}
      <ToastMessage message={toastMessage} onClose={() => setToastMessage(null)} />
    </div>
  );
};

export default ChatLayout;
