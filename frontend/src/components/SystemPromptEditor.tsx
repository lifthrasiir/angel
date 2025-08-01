import React, { useRef, useEffect, useState, useCallback } from 'react';
import { FaChevronDown, FaChevronUp } from 'react-icons/fa';

interface SystemPromptEditorProps {
  systemPrompt: string;
  setSystemPrompt: (prompt: string) => void;
  isSystemPromptEditing: boolean;
  messagesLength: number;
}

type PromptType = 'default' | 'empty' | 'custom';

const SystemPromptEditor: React.FC<SystemPromptEditorProps> = ({
  systemPrompt,
  setSystemPrompt,
  isSystemPromptEditing,
  messagesLength,
}) => {
  const systemPromptTextareaRef = useRef<HTMLTextAreaElement>(null);
  const [evaluatedPrompt, setEvaluatedPrompt] = useState<string>('');
  const [evaluationError, setEvaluationError] = useState<string | null>(null);
  const [promptType, setPromptType] = useState<PromptType>('default');
  const [isExpanded, setIsExpanded] = useState<boolean>(false);

  const adjustSystemPromptTextareaHeight = useCallback(() => {
    if (systemPromptTextareaRef.current) {
      const textarea = systemPromptTextareaRef.current;
      textarea.style.height = 'auto';
      textarea.style.height = textarea.scrollHeight + 'px';
    }
  }, []);

  // Determine if the controls should be read-only
  const isReadOnly = messagesLength !== 0;

  useEffect(() => {
    // Only adjust height if it's a custom prompt and not read-only
    if (promptType === 'custom' && isSystemPromptEditing && !isReadOnly) {
      adjustSystemPromptTextareaHeight();
    }
  }, [systemPrompt, isSystemPromptEditing, promptType, adjustSystemPromptTextareaHeight, isReadOnly]);

  // Function to evaluate the template
  const evaluateTemplate = async (template: string) => {
    try {
      const response = await fetch('/api/evaluate-prompt', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ template }),
      });

      if (!response.ok) {
        const errorData = await response.text();
        throw new Error(errorData || 'Failed to evaluate template');
      }

      const data = await response.json();
      setEvaluatedPrompt(data.evaluatedPrompt);
      setEvaluationError(null);
    } catch (error: any) {
      setEvaluatedPrompt('');
      setEvaluationError(error.message || 'Unknown error during template evaluation.');
    }
  };

  useEffect(() => {
    if (isSystemPromptEditing) {
      if (promptType === 'custom') {
        const handler = setTimeout(() => {
          evaluateTemplate(systemPrompt);
        }, 200); // 200ms debounce time

        return () => {
          clearTimeout(handler);
        };
      } else {
        evaluateTemplate(systemPrompt); // Evaluate as soon as possible
      }
    } else {
      // Clear evaluated prompt and error when not in editable mode
      setEvaluatedPrompt('');
      setEvaluationError(null);
    }
  }, [systemPrompt, isSystemPromptEditing]);

  const handlePromptTypeChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const newPromptType = e.target.value as PromptType;
    setPromptType(newPromptType);
    if (newPromptType === 'default') {
      setSystemPrompt('{{.Builtin.SystemPrompt}}');
    } else if (newPromptType === 'empty') {
      setSystemPrompt('');
    }
    // If 'custom', keep the current systemPrompt value
  };

  return (
    <div className="chat-message-container system-prompt-message">
      <div className={`chat-bubble system-prompt-bubble ${isExpanded || promptType === 'custom' ? 'expanded' : ''}`}>
        {isSystemPromptEditing ? (
          // Editable mode
          <div style={{ display: 'flex', flexDirection: 'column', gap: '10px', width: '100%' }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <select
                value={promptType}
                onChange={handlePromptTypeChange}
                disabled={isReadOnly}
                style={{ padding: '5px', borderRadius: '5px', border: '1px solid #ccc' }}
              >
                <option value="default">Default Prompt</option>
                <option value="empty">Empty Prompt</option>
                <option value="custom">Custom</option>
              </select>
              <button onClick={() => setIsExpanded(!isExpanded)} style={{ background: 'none', border: 'none', cursor: 'pointer', fontSize: '1.2em', color: 'var(--color-system-verydark)' }}>
                {isExpanded ? <FaChevronUp /> : <FaChevronDown />}</button>
            </div>

            {promptType === 'custom' && (
              <textarea
                ref={systemPromptTextareaRef}
                value={systemPrompt}
                onChange={(e) => setSystemPrompt(e.target.value)}
                onInput={(e) => {
                  const target = e.target as HTMLTextAreaElement;
                  target.style.height = 'auto';
                  target.style.height = target.scrollHeight + 'px';
                }}
                readOnly={isReadOnly}
                className={isSystemPromptEditing ? "system-prompt-textarea-editable" : ""}
                style={{ width: '100%', marginTop: '10px', resize: 'none', border: 'none', background: 'transparent', outline: 'none' }}
              />
            )}

            {isExpanded && (
              <div style={{ width: '100%' }}>
                {evaluationError ? (
                  <p style={{ color: 'red' }}>Error: {evaluationError}</p>
                ) : (
                  <pre style={{ whiteSpace: 'pre-wrap', background: '#f9f9f9', padding: '10px', borderRadius: '5px' }}>
                    {evaluatedPrompt}
                  </pre>
                )}
              </div>
            )}
          </div>
        ) : (
          // Read-only mode
          <div
            style={{ display: 'flex', flexDirection: 'column', gap: '10px', width: '100%', cursor: 'pointer' }}
            onClick={() => setIsExpanded(!isExpanded)}
          >
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <span style={{ fontWeight: 'bold', color: 'var(--color-system-verydark)' }}>View Prompt</span>
              {isExpanded ? <FaChevronUp style={{ fontSize: '1.2em', color: 'var(--color-system-verydark)' }} /> : <FaChevronDown style={{ fontSize: '1.2em', color: 'var(--color-system-verydark)' }} />}
            </div>
            {isExpanded && (
              <div style={{ width: '100%' }}>
                <pre style={{ whiteSpace: 'pre-wrap', background: '#f9f9f9', padding: '10px', borderRadius: '5px' }}>
                  {systemPrompt}
                </pre>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
};

export default SystemPromptEditor;
