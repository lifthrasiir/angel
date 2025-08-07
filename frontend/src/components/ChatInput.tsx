import type React from 'react';
import { useEffect, useRef } from 'react';
import { FaPaperclip } from 'react-icons/fa';

interface ChatInputProps {
  inputMessage: string;
  setInputMessage: (message: string) => void;
  handleSendMessage: () => void;
  isStreaming: boolean;
  onFilesSelected: (files: File[]) => void; // New prop for file selection
  handleCancelStreaming: () => void; // New prop for canceling streaming
  inputRef: React.RefObject<HTMLTextAreaElement>;
}

const ChatInput: React.FC<ChatInputProps> = ({
  inputMessage,
  setInputMessage,
  handleSendMessage,
  isStreaming,
  onFilesSelected,
  handleCancelStreaming,
  inputRef,
}) => {
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

  const handleFileChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    if (event.target.files) {
      onFilesSelected(Array.from(event.target.files));
      event.target.value = ''; // Clear the input after selection
    }
  };

  const triggerFileInput = () => {
    fileInputRef.current?.click();
  };

  return (
    <div
      style={{
        padding: '10px 20px',
        borderTop: '1px solid #ccc',
        display: 'flex',
        alignItems: 'center',
        position: 'sticky',
        bottom: 0,
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
          padding: '10px',
          marginRight: '10px',
          background: '#f0f0f0',
          border: '1px solid #ccc',
          borderRadius: '5px',
          cursor: 'pointer',
        }}
      >
        <FaPaperclip />
      </button>
      <textarea
        ref={inputRef}
        value={inputMessage}
        onChange={(e) => setInputMessage(e.target.value)}
        onInput={(e) => {
          debouncedAdjustTextareaHeight(e.target as HTMLTextAreaElement);
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && e.ctrlKey && !isStreaming) {
            e.preventDefault();
            handleSendMessage();
          }
        }}
        placeholder="Enter your message..."
        rows={1}
        style={{
          flexGrow: 1,
          padding: '10px',
          marginRight: '10px',
          border: '1px solid #eee',
          borderRadius: '5px',
          resize: 'none',
          overflowY: 'hidden',
        }}
      />
      {isStreaming ? (
        <button
          onClick={handleCancelStreaming}
          style={{
            padding: '10px 20px',
            background: '#dc3545',
            color: 'white',
            border: 'none',
            borderRadius: '5px',
            cursor: 'pointer',
          }}
        >
          Cancel
        </button>
      ) : (
        <button
          onClick={handleSendMessage}
          style={{
            padding: '10px 20px',
            background: '#007bff',
            color: 'white',
            border: 'none',
            borderRadius: '5px',
            cursor: 'pointer',
          }}
        >
          Send
        </button>
      )}
    </div>
  );
};

export default ChatInput;
