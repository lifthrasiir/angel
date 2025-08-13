import type React from 'react';
import { useEffect, useRef, useState } from 'react';
import { FaPaperclip } from 'react-icons/fa';
import { useAtom, useSetAtom } from 'jotai';
import {
  inputMessageAtom,
  isStreamingAtom,
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
  const [isStreaming] = useAtom(isStreamingAtom);
  const [availableModels] = useAtom(availableModelsAtom);
  const [selectedModel] = useAtom(selectedModelAtom);
  const setSelectedModel = useSetAtom(selectedModelAtom);
  const [selectedFiles] = useAtom(selectedFilesAtom);
  const [statusMessage, setStatusMessage] = useAtom(statusMessageAtom);

  const { runCommand } = useCommandProcessor(sessionId);

  const [isCommandMode, setIsCommandMode] = useState(false);
  const [commandPrefix, setCommandPrefix] = useState('');

  const fileInputRef = useRef<HTMLInputElement>(null);

  // Debounce utility function
  const debounce = (func: Function, delay: number) => {
    let timeout: number;
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
    if (isStreaming) {
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
        gridTemplateColumns: 'auto auto 1fr auto',
        gridTemplateRows: '1fr auto',
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
      <button
        onClick={triggerFileInput}
        style={{
          height: '100%',
          padding: '10px',
          marginRight: '10px',
          background: '#f0f0f0',
          border: '1px solid #ccc',
          borderRadius: '5px',
          cursor: 'pointer',
          gridArea: '1 / 1',
        }}
        aria-label="Attach files"
      >
        <FaPaperclip />
      </button>
      <span
        style={{
          padding: commandPrefix ? '6px 5px 6px 0' : '0',
          fontFamily: 'monospace',
          gridArea: '1 / 2',
          color: isCommandMode ? '#1e7e34' : 'inherit',
        }}
      >
        {commandPrefix}
      </span>
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
        rows={2}
        style={{
          height: '100%',
          padding: '5px',
          border: '1px solid #ccc',
          borderRadius: '5px',
          resize: 'none',
          overflowY: 'hidden',
          display: 'flex',
          flexDirection: 'column',
          gridArea: '1 / 3',
          color: isCommandMode ? '#1e7e34' : 'inherit',
        }}
        aria-label="Message input"
      />
      <div
        style={{
          display: 'flex',
          justifyContent: 'flex-end',
          gridArea: '2 / 2 / 2 / span 2',
        }}
      >
        <div
          style={{
            marginRight: 'auto',
            color: statusMessage === 'Invalid command.' ? 'red' : 'inherit',
          }}
        >
          {statusMessage === null ? (isCommandMode ? '' : 'Type / to enter command mode') : statusMessage}
        </div>
        <label htmlFor="model-select" style={{ marginRight: '10px' }}>
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
            padding: '0 8px',
            borderRadius: '5px',
            border: '1px solid #ccc',
            backgroundColor: '#fff',
            cursor: 'pointer',
          }}
        >
          {Array.from(availableModels.values()).map((model) => (
            <option key={model.name} value={model.name}>
              {model.name}
            </option>
          ))}
        </select>
      </div>
      {isStreaming ? (
        <button
          onClick={handleCancelStreaming}
          style={{
            height: '100%',
            padding: '10px 20px',
            marginLeft: '10px',
            background: '#dc3545',
            color: 'white',
            border: 'none',
            borderRadius: '5px',
            cursor: 'pointer',
            gridArea: '1 / 4',
          }}
        >
          Cancel
        </button>
      ) : (
        <button
          onClick={handleSendOrRunCommand}
          disabled={isSendButtonDisabled}
          style={{
            height: '100%',
            padding: '10px 20px',
            marginLeft: '10px',
            background: isCommandMode ? '#28a745' : '#007bff',
            color: 'white',
            border: 'none',
            borderRadius: '5px',
            cursor: isSendButtonDisabled ? 'not-allowed' : 'pointer',
            opacity: isSendButtonDisabled ? 0.5 : 1,
            gridArea: '1 / 4',
          }}
        >
          {isCommandMode ? 'Run' : 'Send'}
        </button>
      )}
    </div>
  );
};

export default ChatInput;
