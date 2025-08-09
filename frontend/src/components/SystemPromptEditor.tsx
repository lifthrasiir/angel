import type React from 'react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { FaChevronDown, FaChevronUp } from 'react-icons/fa';
import { useAtom, useSetAtom } from 'jotai';
import { systemPromptAtom, isSystemPromptEditingAtom, messagesAtom } from '../atoms/chatAtoms';

interface SystemPromptEditorProps {
  // No props needed, all state managed by Jotai
}

type PromptType = 'default' | 'empty' | 'custom';

const SystemPromptEditor: React.FC<SystemPromptEditorProps> = () => {
  const [systemPrompt] = useAtom(systemPromptAtom);
  const setSystemPrompt = useSetAtom(systemPromptAtom);
  const [isSystemPromptEditing] = useAtom(isSystemPromptEditingAtom);
  const [messages] = useAtom(messagesAtom);
  const messagesLength = messages.length;

  const systemPromptTextareaRef = useRef<HTMLTextAreaElement>(null);
  const evaluatedPromptRef = useRef<HTMLPreElement>(null); // 변경: HTMLDivElement -> HTMLPreElement
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
      const response = await fetch('/api/evaluatePrompt', {
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
  }, [systemPrompt, isSystemPromptEditing, evaluateTemplate, promptType]);

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

  const clickAnywhereToExpand = !isSystemPromptEditing && !isExpanded;

  // 최상위 렌더링 요소를 조건부로 할당
  const RootElement = clickAnywhereToExpand ? 'button' : 'div';

  return (
    <div className="chat-message-container system-prompt-message">
      <RootElement
        className={`chat-bubble system-prompt-bubble ${isExpanded || promptType === 'custom' ? 'expanded' : ''}`}
        style={{
          cursor: clickAnywhereToExpand ? 'pointer' : '',
        }}
        onClick={
          clickAnywhereToExpand
            ? () => {
                setIsExpanded(true);
                setTimeout(() => {
                  if (!isSystemPromptEditing) {
                    evaluatedPromptRef.current?.focus();
                  }
                }, 0);
              }
            : undefined
        } // onClick 핸들러 조건부 할당
        aria-label={clickAnywhereToExpand ? 'Expand system prompt' : undefined}
        tabIndex={clickAnywhereToExpand ? undefined : -1} // tabIndex 조건부 할당
        role={clickAnywhereToExpand ? undefined : isSystemPromptEditing ? undefined : 'button'} // role 조건부 할당
      >
        {isSystemPromptEditing ? (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              gap: '10px',
              width: '100%',
            }}
          >
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
              }}
            >
              <select
                value={promptType}
                onChange={handlePromptTypeChange}
                disabled={isReadOnly}
                style={{
                  padding: '5px',
                  borderRadius: '5px',
                  border: '1px solid #ccc',
                }}
              >
                <option value="default">Default Prompt</option>
                <option value="empty">Empty Prompt</option>
                <option value="custom">Custom</option>
              </select>
              <button
                onClick={() => setIsExpanded(!isExpanded)}
                style={{
                  background: 'none',
                  border: 'none',
                  cursor: 'pointer',
                  fontSize: '1.2em',
                  marginLeft: '0.8ex',
                  color: 'var(--color-system-verydark)',
                }}
                aria-label={isExpanded ? 'Collapse prompt preview' : 'Expand prompt preview'}
              >
                {isExpanded ? <FaChevronUp /> : <FaChevronDown />}
              </button>
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
                className={isSystemPromptEditing ? 'system-prompt-textarea-editable' : ''}
                style={{
                  width: '100%',
                  marginTop: '10px',
                  resize: 'none',
                  border: 'none',
                  background: 'transparent',
                  outline: 'none',
                }}
                aria-label="System prompt editor"
              />
            )}

            {isExpanded && (
              <div style={{ width: '100%' }}>
                {evaluationError ? (
                  <p style={{ color: 'red' }}>Error: {evaluationError}</p>
                ) : (
                  <pre
                    style={{
                      whiteSpace: 'pre-wrap',
                      background: '#f9f9f9',
                      padding: '10px',
                      borderRadius: '5px',
                    }}
                  >
                    {evaluatedPrompt}
                  </pre>
                )}
              </div>
            )}
          </div>
        ) : (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              gap: '10px',
              width: '100%',
            }}
          >
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
              }}
            >
              <span
                style={{
                  fontWeight: 'bold',
                  color: 'var(--color-system-verydark)',
                }}
              >
                View Prompt
              </span>
              {isExpanded ? (
                <FaChevronUp
                  style={{
                    fontSize: '1.2em',
                    color: 'var(--color-system-verydark)',
                    cursor: 'pointer',
                    marginLeft: '0.8ex',
                  }}
                  onClick={() => setIsExpanded(false)}
                  aria-label="Collapse prompt"
                  tabIndex={isExpanded ? undefined : -1} // 변경
                />
              ) : (
                <FaChevronDown
                  style={{
                    fontSize: '1.2em',
                    color: 'var(--color-system-verydark)',
                    cursor: 'pointer',
                    marginLeft: '0.8ex',
                  }}
                  onClick={() => setIsExpanded(true)}
                  aria-label="Expand prompt"
                  tabIndex={isExpanded ? undefined : -1} // 변경
                />
              )}
            </div>
            {isExpanded && (
              <div
                style={{
                  width: '100%',
                }}
              >
                <pre
                  ref={evaluatedPromptRef} // ref를 pre 태그로 이동
                  tabIndex={-1} // tabIndex를 pre 태그로 이동
                  style={{
                    whiteSpace: 'pre-wrap',
                    background: '#f9f9f9',
                    padding: '10px',
                    borderRadius: '5px',
                    overflowY: 'auto', // 스크롤 가능하게
                    maxHeight: '200px', // 적절한 높이 설정
                  }}
                >
                  {systemPrompt}
                </pre>
              </div>
            )}
          </div>
        )}
      </RootElement>
    </div>
  );
};

export default SystemPromptEditor;
