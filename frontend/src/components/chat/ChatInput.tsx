import type React from 'react';
import { useEffect, useRef, useState } from 'react';
import { FaPaperclip, FaPaperPlane, FaTimes } from 'react-icons/fa';
import { useAtom, useSetAtom } from 'jotai';
import { inputMessageAtom } from '../../atoms/chatAtoms';
import { selectedFilesAtom } from '../../atoms/fileAtoms';
import { statusMessageAtom } from '../../atoms/uiAtoms';
import { useCommandProcessor } from '../../hooks/useCommandProcessor';
import { useProcessingState } from '../../hooks/useProcessingState';
import { handleEnterKey } from '../../utils/enterKeyHandler';
import { handleNavigationKeys } from '../../utils/navigationKeys';

interface ChatInputProps {
  handleSendMessage: (message?: string) => void;
  onFilesSelected: (files: File[]) => void;
  handleCancelStreaming: () => void;
  inputRef: React.RefObject<HTMLTextAreaElement>;
  chatAreaRef?: React.RefObject<HTMLDivElement>;
  sessionId: string | null;
  isSendDisabledByResizing?: () => boolean;
  disabledBecause?: 'notauth' | 'archived';
}

const ChatInput: React.FC<ChatInputProps> = ({
  handleSendMessage,
  onFilesSelected,
  handleCancelStreaming,
  inputRef,
  chatAreaRef,
  sessionId,
  isSendDisabledByResizing,
  disabledBecause,
}) => {
  const { isProcessing } = useProcessingState();
  const [inputMessage] = useAtom(inputMessageAtom);
  const setInputMessage = useSetAtom(inputMessageAtom);

  // Local state for typing performance
  const [localInput, setLocalInput] = useState(inputMessage);
  const [selectedFiles] = useAtom(selectedFilesAtom);
  const [statusMessage, setStatusMessage] = useAtom(statusMessageAtom);

  const { runCommand } = useCommandProcessor(sessionId);

  const [isCommandMode, setIsCommandMode] = useState(false);
  const [commandPrefix, setCommandPrefix] = useState('');
  const [isMobile, setIsMobile] = useState(false);
  const [isComposing, setIsComposing] = useState(false);

  const fileInputRef = useRef<HTMLInputElement>(null);

  // Detect mobile screen size
  useEffect(() => {
    const checkMobile = () => {
      setIsMobile(window.innerWidth <= 768);
    };

    checkMobile();
    window.addEventListener('resize', checkMobile);
    return () => window.removeEventListener('resize', checkMobile);
  }, []);

  // Debounce utility function
  const debounce = (func: Function, delay: number) => {
    let timeout: ReturnType<typeof setTimeout>;
    return (...args: any[]) => {
      clearTimeout(timeout);
      timeout = setTimeout(() => func(...args), delay);
    };
  };

  // Debounced function for textarea height adjustment
  const debouncedAdjustTextareaHeight = useRef(
    debounce((target: HTMLTextAreaElement) => {
      target.style.height = 'auto';
      target.style.height = target.scrollHeight + 'px';
    }, 100), // 100ms debounce delay
  ).current;

  // Sync local input with atom when inputMessage changes (e.g., after sending message)
  useEffect(() => {
    setLocalInput(inputMessage);
  }, [inputMessage]);

  // Adjust textarea height when localInput changes
  useEffect(() => {
    if (inputRef.current) {
      debouncedAdjustTextareaHeight(inputRef.current);
    }
  }, [localInput]);

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // During IME composition, don't handle navigation keys or special keys
    // This prevents interference with IME input (Korean, Chinese, Japanese)
    if (isComposing) {
      return;
    }

    // Handle navigation keys first (Home/End/PgUp/PgDown without modifiers)
    const isNavigationHandled = handleNavigationKeys(e, inputRef, chatAreaRef);
    if (isNavigationHandled) {
      return;
    }

    // Use the common Enter key handler
    const isHandled = handleEnterKey(e, {
      onSendOrConfirm: handleSendOrRunCommand,
      value: localInput,
    });

    // If Enter key was handled, don't process other key handlers
    if (isHandled) {
      return;
    }

    // Handle other special keys
    handleSlashKey(e);
    handleBackspaceInCommandMode(e);
  };

  const handleSlashKey = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === '/' && inputRef.current?.selectionStart === 0 && !isCommandMode) {
      e.preventDefault();
      setIsCommandMode(true);
      setCommandPrefix('/');
      const end = inputRef.current?.selectionEnd || 0;
      const newInputValue = localInput.substring(end);
      setLocalInput(newInputValue);
      setTimeout(() => {
        if (inputRef.current) {
          inputRef.current.focus();
          inputRef.current.setSelectionRange(0, 0);
        }
      }, 0);
    }
  };

  const handleBackspaceInCommandMode = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (
      isCommandMode &&
      e.key === 'Backspace' &&
      inputRef.current?.selectionStart === 0 &&
      inputRef.current?.selectionEnd === 0
    ) {
      e.preventDefault();
      setIsCommandMode(false);
      setCommandPrefix('');
      setLocalInput('/' + localInput);
      setTimeout(() => {
        if (inputRef.current) {
          inputRef.current.focus();
          inputRef.current.setSelectionRange(1, 1);
        }
      }, 0);
    }
  };

  const handleFileChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    if (event.target.files) {
      onFilesSelected(Array.from(event.target.files));
      event.target.value = ''; // Clear the input after selection
    }
  };

  const triggerFileInput = () => {
    fileInputRef.current?.click();
  };

  const handleSendOrRunCommand = () => {
    if (isProcessing) {
      handleCancelStreaming();
      return;
    }

    if (isCommandMode) {
      const commandText = localInput.trim();
      if (commandText) {
        const parts = commandText.split(' ');
        const command = parts[0];
        const args = parts.slice(1).join(' ');
        runCommand(command, args);
      } else {
        setStatusMessage('Please enter a command.');
      }
      setInputMessage('');
      setLocalInput('');
      setIsCommandMode(false);
      setCommandPrefix('');
    } else {
      // Send message with local input directly
      handleSendMessage(localInput);
      setLocalInput('');
    }
  };

  const isSendButtonDisabled =
    !!disabledBecause ||
    (localInput.trim() === '' && selectedFiles.length === 0) ||
    (isSendDisabledByResizing && isSendDisabledByResizing());

  return (
    <div
      style={{
        padding: '10px',
        borderTop: '1px solid #ccc',
        display: 'grid',
        gridTemplateColumns: isMobile ? 'auto 1fr auto auto' : 'auto auto 1fr auto',
        gridTemplateRows: isMobile ? 'auto auto' : '1fr auto',
        gap: '0',
        alignItems: 'top',
        background: 'white',
      }}
    >
      <input
        type="file"
        multiple
        ref={fileInputRef}
        onChange={handleFileChange}
        style={{ display: 'none' }} // Hide the actual file input
      />

      {/* File attachment button */}
      <button
        onClick={triggerFileInput}
        disabled={!!disabledBecause}
        style={{
          height: '100%',
          minHeight: isMobile ? '1em' : 'auto',
          padding: isMobile ? '5px' : '10px',
          marginRight: isMobile ? '5px' : '10px',
          background: '#f0f0f0',
          border: '1px solid #ccc',
          borderRadius: '5px',
          cursor: disabledBecause ? 'not-allowed' : 'pointer',
          gridArea: isMobile ? '2 / 1' : '1 / 1',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          opacity: disabledBecause ? 0.5 : 1,
        }}
        aria-label="Attach files"
      >
        <FaPaperclip />
      </button>

      {/* Command prefix - Desktop only */}
      {!isMobile && commandPrefix && (
        <span
          style={{
            padding: '6px 5px 6px 0',
            fontFamily: 'monospace',
            gridArea: '1 / 2',
            color: isCommandMode ? '#1e7e34' : 'inherit',
            alignSelf: 'flex-start',
          }}
        >
          {commandPrefix}
        </span>
      )}

      {/* Main textarea area */}
      <textarea
        ref={inputRef}
        value={localInput}
        onChange={(e) => setLocalInput(e.target.value)}
        onInput={(e) => {
          setStatusMessage(null);
          debouncedAdjustTextareaHeight(e.target as HTMLTextAreaElement);
        }}
        onKeyDown={handleKeyDown}
        onCompositionStart={() => setIsComposing(true)}
        onCompositionEnd={() => setIsComposing(false)}
        onPaste={(e) => {
          if (e.clipboardData.files && e.clipboardData.files.length > 0) {
            onFilesSelected(Array.from(e.clipboardData.files));
          }
        }}
        placeholder={
          disabledBecause === 'notauth'
            ? 'Login required to send messages'
            : disabledBecause === 'archived'
              ? 'This session is archived and cannot be modified'
              : isCommandMode
                ? 'Enter a slash command...'
                : 'Enter your message...'
        }
        rows={isMobile ? 1 : 2}
        disabled={!!disabledBecause}
        style={{
          height: '100%',
          padding: '5px',
          border: '1px solid #ccc',
          borderRadius: '5px',
          resize: 'none',
          overflowY: 'hidden',
          gridArea: isMobile ? '1 / 1 / 1 / span 2' : '1 / 3',
          color: isCommandMode ? '#1e7e34' : 'inherit',
          fontSize: '16px', // Prevent zoom on iOS
          backgroundColor: disabledBecause ? '#f5f5f5' : 'white',
          cursor: disabledBecause ? 'not-allowed' : 'text',
        }}
        aria-label="Message input"
      />

      {/* Send/Cancel button */}
      <div style={{ gridArea: isMobile ? '1 / 3' : '1 / 4' }}>
        {isProcessing ? (
          <button
            onClick={handleCancelStreaming}
            style={{
              height: '100%',
              width: isMobile ? '40px' : 'auto',
              padding: isMobile ? '8px' : '10px 20px',
              marginLeft: isMobile ? '5px' : '10px',
              background: '#dc3545',
              color: 'white',
              border: 'none',
              borderRadius: '5px',
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
            }}
            aria-label="Cancel"
          >
            {isMobile ? <FaTimes /> : 'Cancel'}
          </button>
        ) : (
          <button
            onClick={handleSendOrRunCommand}
            disabled={isSendButtonDisabled}
            style={{
              height: '100%',
              width: isMobile ? '40px' : 'auto',
              padding: isMobile ? '8px' : '10px 20px',
              marginLeft: isMobile ? '5px' : '10px',
              background: isCommandMode ? '#28a745' : '#007bff',
              color: 'white',
              border: 'none',
              borderRadius: '5px',
              cursor: isSendButtonDisabled ? 'not-allowed' : 'pointer',
              opacity: isSendButtonDisabled ? 0.5 : 1,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
            }}
            aria-label={isCommandMode ? 'Run command' : 'Send message'}
          >
            {isMobile ? isCommandMode ? <FaPaperPlane /> : <FaPaperPlane /> : isCommandMode ? 'Run' : 'Send'}
          </button>
        )}
      </div>

      {/* Second row: Status message - smaller height */}
      <div
        style={{
          display: 'flex',
          justifyContent: 'flex-end',
          alignItems: 'center',
          gridArea: isMobile ? '2 / 2 / 2 / -1' : '2 / 1 / 2 / -1',
          padding: '2px 0',
        }}
      >
        <div
          style={{
            marginRight: 'auto',
            color: statusMessage === 'Invalid command.' ? 'red' : 'inherit',
            fontSize: '0.8em',
          }}
        >
          {statusMessage === null ? (isCommandMode ? '' : 'Type / to enter command mode') : statusMessage}
        </div>
      </div>
    </div>
  );
};

export default ChatInput;
