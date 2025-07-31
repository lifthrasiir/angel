import React, { useRef, useEffect } from 'react';
import { FaPaperclip } from 'react-icons/fa';

interface ChatInputProps {
  inputMessage: string;
  setInputMessage: (message: string) => void;
  handleSendMessage: () => void;
  isStreaming: boolean;
  onFilesSelected: (files: File[]) => void; // New prop for file selection
}

const ChatInput: React.FC<ChatInputProps> = ({
  inputMessage,
  setInputMessage,
  handleSendMessage,
  isStreaming,
  onFilesSelected,
}) => {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Debounce utility function
  const debounce = (func: Function, delay: number) => {
    let timeout: NodeJS.Timeout;
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
    }, 100) // 100ms debounce delay
  ).current;

  // Adjust textarea height when inputMessage changes (e.g., after sending message)
  useEffect(() => {
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto';
      textareaRef.current.style.height = textareaRef.current.scrollHeight + 'px';
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
    <div style={{ padding: '10px 20px', borderTop: '1px solid #ccc', display: 'flex', alignItems: 'center', position: 'sticky', bottom: 0, background: 'white' }}>
      <input
        type="file"
        multiple
        ref={fileInputRef}
        onChange={handleFileChange}
        style={{ display: 'none' }} // Hide the actual file input
      />
      <button onClick={triggerFileInput} style={{ padding: '10px', marginRight: '10px', background: '#f0f0f0', border: '1px solid #ccc', borderRadius: '5px', cursor: 'pointer' }}>
        <FaPaperclip />
      </button>
      <textarea
        ref={textareaRef}
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
        style={{ flexGrow: 1, padding: '10px', marginRight: '10px', border: '1px solid #eee', borderRadius: '5px', resize: 'none', overflowY: 'hidden' }}
      />
      <button onClick={handleSendMessage} disabled={isStreaming} style={{ padding: '10px 20px', background: '#007bff', color: 'white', border: 'none', borderRadius: '5px', cursor: isStreaming ? 'not-allowed' : 'pointer', opacity: isStreaming ? 0.5 : 1 }}>
        Send
      </button>
    </div>
  );
};

export default ChatInput;
