import type React from 'react';
import { useEffect, useRef, useState } from 'react';
import { FaPaperclip, FaPaperPlane, FaTimes } from 'react-icons/fa';
import { useAtom, useSetAtom, useAtomValue } from 'jotai';
import {
  inputMessageAtom,
  processingStartTimeAtom,
  availableModelsAtom,
  selectedModelAtom,
  selectedFilesAtom,
  statusMessageAtom,
} from '../atoms/chatAtoms';
import { useCommandProcessor } from '../hooks/useCommandProcessor';

interface ChatInputProps {
  handleSendMessage: () => void;
  onFilesSelected: (files: File[]) => void;
  handleCancelStreaming: () => void;
  inputRef: React.RefObject<HTMLTextAreaElement>;
  sessionId: string | null; // Add sessionId prop
}

const ChatInput: React.FC<ChatInputProps> = ({
  handleSendMessage,
  onFilesSelected,
  handleCancelStreaming,
  inputRef,
  sessionId,
}) => {
  const [inputMessage] = useAtom(inputMessageAtom);
  const setInputMessage = useSetAtom(inputMessageAtom);
  const processingStartTime = useAtomValue(processingStartTimeAtom);
  const [availableModels] = useAtom(availableModelsAtom);
  const [selectedModel] = useAtom(selectedModelAtom);
  const setSelectedModel = useSetAtom(selectedModelAtom);
  const [selectedFiles] = useAtom(selectedFilesAtom);
  const [statusMessage, setStatusMessage] = useAtom(statusMessageAtom);

  const { runCommand } = useCommandProcessor(sessionId);

  const [isCommandMode, setIsCommandMode] = useState(false);
  const [commandPrefix, setCommandPrefix] = useState('');
  const [isMobile, setIsMobile] = useState(false);

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

  // Adjust textarea height when inputMessage changes (e.g., after sending message)
  useEffect(() => {
    if (inputRef.current) {
      inputRef.current.style.height = 'auto';
      inputRef.current.style.height = inputRef.current.scrollHeight + 'px';
    }
  }, [inputMessage]);

  const handleCtrlEnter = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && e.ctrlKey) {
      e.preventDefault();
      handleSendOrRunCommand();
    }
  };

  const handleSlashKey = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === '/' && inputRef.current?.selectionStart === 0 && !isCommandMode) {
      e.preventDefault();
      setIsCommandMode(true);
      setCommandPrefix('/');
      const end = inputRef.current?.selectionEnd || 0;
      const newInputValue = inputMessage.substring(end);
      setInputMessage(newInputValue);
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
      setInputMessage('/' + inputMessage);
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
    if (processingStartTime !== null) {
      handleCancelStreaming();
      return;
    }

    if (isCommandMode) {
      const commandText = inputMessage.trim();
      if (commandText) {
        const parts = commandText.split(' ');
        const command = parts[0];
        const args = parts.slice(1).join(' ');
        runCommand(command, args);
      } else {
        setStatusMessage('Please enter a command.');
      }
      setInputMessage('');
      setIsCommandMode(false);
      setCommandPrefix('');
    } else {
      handleSendMessage();
    }
  };

  const isSendButtonDisabled = inputMessage.trim() === '' && selectedFiles.length === 0;

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
        style={{
          height: '100%',
          minHeight: isMobile ? '1em' : 'auto',
          padding: isMobile ? '5px' : '10px',
          marginRight: isMobile ? '5px' : '10px',
          background: '#f0f0f0',
          border: '1px solid #ccc',
          borderRadius: '5px',
          cursor: 'pointer',
          gridArea: isMobile ? '2 / 1' : '1 / 1',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
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
        value={inputMessage}
        onChange={(e) => setInputMessage(e.target.value)}
        onInput={(e) => {
          setStatusMessage(null);
          debouncedAdjustTextareaHeight(e.target as HTMLTextAreaElement);
        }}
        onKeyDown={(e) => {
          handleCtrlEnter(e);
          handleSlashKey(e);
          handleBackspaceInCommandMode(e);
        }}
        onPaste={(e) => {
          if (e.clipboardData.files && e.clipboardData.files.length > 0) {
            onFilesSelected(Array.from(e.clipboardData.files));
          }
        }}
        placeholder={isCommandMode ? 'Enter a slash command...' : 'Enter your message...'}
        rows={isMobile ? 1 : 2}
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
        }}
        aria-label="Message input"
      />

      {/* Send/Cancel button */}
      <div style={{ gridArea: isMobile ? '1 / 3' : '1 / 4' }}>
        {processingStartTime !== null ? (
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

      {/* Second row: Status and Model selector - smaller height */}
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
        <label htmlFor="model-select" style={{ marginRight: '5px', fontSize: '0.9em' }}>
          Model:
        </label>
        <select
          id="model-select"
          value={selectedModel?.name || ''}
          onChange={(e) => {
            const selectedModelName = e.target.value;
            const model = availableModels.get(selectedModelName);
            if (model) {
              setSelectedModel(model);
            }
          }}
          style={{
            padding: '2px 6px',
            borderRadius: '4px',
            border: '1px solid #ccc',
            backgroundColor: '#fff',
            cursor: 'pointer',
            fontSize: '0.9em',
          }}
        >
          {Array.from(availableModels.values()).map((model) => (
            <option key={model.name} value={model.name}>
              {model.name}
            </option>
          ))}
        </select>
      </div>
    </div>
  );
};

export default ChatInput;
