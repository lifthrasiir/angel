import React, { useRef, useEffect } from 'react';

interface SystemPromptEditorProps {
  systemPrompt: string;
  setSystemPrompt: React.Dispatch<React.SetStateAction<string>>;
  isSystemPromptEditing: boolean;
  setIsSystemPromptEditing: React.Dispatch<React.SetStateAction<boolean>>;
  messagesLength: number;
}

const SystemPromptEditor: React.FC<SystemPromptEditorProps> = ({
  systemPrompt,
  setSystemPrompt,
  isSystemPromptEditing,
  setIsSystemPromptEditing,
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
            {messagesLength === 0 && (
              <button
                onClick={() => setIsSystemPromptEditing(true)}
                style={{
                  marginTop: '10px',
                  padding: '5px 10px',
                  background: '#f0f0f0',
                  border: '1px solid #ccc',
                  borderRadius: '5px',
                  cursor: 'pointer',
                  marginLeft: 'auto',
                  display: 'block',
                }}
              >
                Edit System Prompt
              </button>
            )}
          </>
        )}
      </div>
    </div>
  );
};

export default SystemPromptEditor;
