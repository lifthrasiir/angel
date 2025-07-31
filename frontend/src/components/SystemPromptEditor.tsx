import React, { useRef, useEffect } from 'react';

interface SystemPromptEditorProps {
  systemPrompt: string;
  setSystemPrompt: (prompt: string) => void;
  isSystemPromptEditing: boolean;
  
  messagesLength: number;
}

const SystemPromptEditor: React.FC<SystemPromptEditorProps> = ({
  systemPrompt,
  setSystemPrompt,
  isSystemPromptEditing,
  
  messagesLength,
}) => {
  const systemPromptTextareaRef = useRef<HTMLTextAreaElement>(null);

  const adjustSystemPromptTextareaHeight = () => {
    if (systemPromptTextareaRef.current) {
      const textarea = systemPromptTextareaRef.current;
      textarea.style.height = 'auto';
      textarea.style.height = textarea.scrollHeight + 'px';
    }
  };

  useEffect(() => {
    adjustSystemPromptTextareaHeight();
  }, [systemPrompt, isSystemPromptEditing]);

  return (
    <div className="chat-message-container system-prompt-message">
      <div className="chat-bubble system-prompt-bubble">
        {isSystemPromptEditing && messagesLength === 0 ? (
          <>
            <textarea
              ref={systemPromptTextareaRef}
              value={systemPrompt}
              onChange={(e) => setSystemPrompt(e.target.value)}
              onInput={(e) => {
                const target = e.target as HTMLTextAreaElement;
                target.style.height = 'auto';
                target.style.height = target.scrollHeight + 'px';
              }}
              disabled={messagesLength !== 0}
              className={isSystemPromptEditing ? "system-prompt-textarea-editable" : ""}
              style={{ width: '100%', resize: 'none', border: 'none', background: 'transparent', outline: 'none' }}
            />
          </>
        ) : (
          <>
            <div className="system-prompt-display-non-editable" style={{ whiteSpace: 'pre-wrap' }}>{systemPrompt}</div>
            
          </>
        )}
      </div>
    </div>
  );
};

export default SystemPromptEditor;
