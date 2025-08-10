import type React from 'react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { FaChevronDown, FaChevronUp } from 'react-icons/fa';
import { useAtom } from 'jotai';
import { messagesAtom } from '../atoms/chatAtoms';

export interface PredefinedPrompt {
  label: string;
  value: string;
}

interface SystemPromptEditorProps {
  initialPrompt: string;
  currentLabel: string; // New prop for the label
  onPromptUpdate: (prompt: PredefinedPrompt) => void; // Changed prop
  isEditing: boolean;
  predefinedPrompts?: PredefinedPrompt[];
  isGlobalSettings?: boolean; // New prop to indicate if it's used in global settings
}

const CUSTOM_PROMPT_SYMBOL = Symbol('custom');

const SystemPromptEditor: React.FC<SystemPromptEditorProps> = ({
  initialPrompt,
  currentLabel, // Destructure new prop
  onPromptUpdate, // Destructure changed prop
  isEditing,
  predefinedPrompts = [],
  isGlobalSettings = false,
}) => {
  const [systemPrompt, setSystemPromptInternal] = useState(initialPrompt);
  const [internalLabel, setInternalLabel] = useState(currentLabel); // Internal state for label
  const [messages] = useAtom(messagesAtom);
  const messagesLength = messages.length;

  // Update internal state when initialPrompt prop changes
  useEffect(() => {
    setSystemPromptInternal(initialPrompt);
  }, [initialPrompt]);

  // Update internal label state when currentLabel prop changes
  useEffect(() => {
    setInternalLabel(currentLabel);
  }, [currentLabel]);

  const setSystemPrompt = (prompt: string) => {
    setSystemPromptInternal(prompt);
    onPromptUpdate({ label: internalLabel, value: prompt }); // Call with both label and value
  };

  const systemPromptTextareaRef = useRef<HTMLTextAreaElement>(null);
  const evaluatedPromptRef = useRef<HTMLPreElement>(null);
  const [evaluatedPrompt, setEvaluatedPrompt] = useState<string>('');
  const [evaluationError, setEvaluationError] = useState<string | null>(null);

  // Always treat as custom prompt if isGlobalSettings is true
  const [promptType, setPromptType] = useState<string | symbol>(CUSTOM_PROMPT_SYMBOL);
  const [isManuallySetToCustom, setIsManuallySetToCustom] = useState(false);

  // Always expanded if isGlobalSettings is true
  const [isExpanded, setIsExpanded] = useState<boolean>(isGlobalSettings ? true : false);

  // Update promptType whenever systemPrompt or predefinedPrompts change
  useEffect(() => {
    if (isGlobalSettings) {
      setPromptType(CUSTOM_PROMPT_SYMBOL); // Always custom for global settings
      setIsExpanded(true); // Always expanded for global settings
      return;
    }
    // Don't auto-change if user manually set to custom
    if (isManuallySetToCustom) {
      return;
    }
    const foundPrompt = predefinedPrompts.find((p) => p.value === systemPrompt);
    if (foundPrompt) {
      setPromptType(foundPrompt.value);
    } else {
      setPromptType(CUSTOM_PROMPT_SYMBOL); // Custom prompt
    }
  }, [systemPrompt, predefinedPrompts, isGlobalSettings, isManuallySetToCustom]);

  const adjustSystemPromptTextareaHeight = useCallback(() => {
    if (systemPromptTextareaRef.current) {
      const textarea = systemPromptTextareaRef.current;
      textarea.style.height = 'auto';
      textarea.style.height = textarea.scrollHeight + 'px';
    }
  }, []);

  // Determine if the controls should be read-only
  const isReadOnly = !isGlobalSettings && messagesLength !== 0;

  useEffect(() => {
    // Only adjust height if it's a custom prompt and not read-only
    if (promptType === CUSTOM_PROMPT_SYMBOL && isEditing && !isReadOnly) {
      adjustSystemPromptTextareaHeight();
    }
  }, [systemPrompt, isEditing, promptType, adjustSystemPromptTextareaHeight, isReadOnly]);

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
    if (isEditing) {
      // Always evaluate for global settings, or if custom prompt
      if (isGlobalSettings || promptType === CUSTOM_PROMPT_SYMBOL) {
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
  }, [systemPrompt, isEditing, evaluateTemplate, promptType, isGlobalSettings]);

  const handlePromptTypeChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const selectedIndex = e.target.selectedIndex;
    const customOptionIndex = predefinedPrompts.length; // Assuming "Custom" is always the last option

    if (selectedIndex === customOptionIndex) {
      // User selected "Custom"
      setPromptType(CUSTOM_PROMPT_SYMBOL);
      setIsManuallySetToCustom(true);
      // Don't change systemPrompt here to avoid triggering the useEffect
    } else {
      // User selected a predefined prompt
      const selectedPrompt = predefinedPrompts[selectedIndex];
      setPromptType(selectedPrompt.value);
      setIsManuallySetToCustom(false);
      setSystemPrompt(selectedPrompt.value);
    }
  };

  const clickAnywhereToExpand = !isEditing && !isExpanded;

  // 최상위 렌더링 요소를 조건부로 할당
  const RootElement = clickAnywhereToExpand ? 'button' : 'div';

  return (
    <div className="chat-message-container system-prompt-message">
      <RootElement
        className={`chat-bubble system-prompt-bubble ${isExpanded || promptType === CUSTOM_PROMPT_SYMBOL ? 'expanded' : ''}`}
        style={{
          cursor: clickAnywhereToExpand ? 'pointer' : '',
        }}
        onClick={
          clickAnywhereToExpand
            ? () => {
                setIsExpanded(true);
                setTimeout(() => {
                  if (!isEditing) {
                    evaluatedPromptRef.current?.focus();
                  }
                }, 0);
              }
            : undefined
        } // onClick 핸들러 조건부 할당
        aria-label={clickAnywhereToExpand ? 'Expand system prompt' : undefined}
        tabIndex={clickAnywhereToExpand ? undefined : -1} // tabIndex 조건부 할당
        role={clickAnywhereToExpand ? undefined : isEditing ? undefined : 'button'} // role 조건부 할당
      >
        {isEditing ? (
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
              {isGlobalSettings ? (
                <input
                  type="text"
                  placeholder="Prompt Label"
                  value={internalLabel}
                  onChange={(e) => {
                    setInternalLabel(e.target.value);
                    onPromptUpdate({ label: e.target.value, value: systemPrompt }); // Update label immediately
                  }}
                  style={{
                    padding: '5px',
                    borderRadius: '5px',
                    border: '1px solid #ccc',
                    flexGrow: 1, // Allow it to take available space
                  }}
                />
              ) : (
                <select
                  value={typeof promptType === 'symbol' ? promptType.description : promptType} // Use description for symbol value
                  onChange={handlePromptTypeChange}
                  disabled={isReadOnly}
                  style={{
                    padding: '5px',
                    borderRadius: '5px',
                    border: '1px solid #ccc',
                  }}
                >
                  {predefinedPrompts.map((p) => (
                    <option key={p.label} value={p.value}>
                      {p.label}
                    </option>
                  ))}
                  <option value={CUSTOM_PROMPT_SYMBOL.description}>Custom</option>
                </select>
              )}
              {!isGlobalSettings && ( // Hide chevron for global settings
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
              )}
            </div>

            {/* Always show textarea for global settings, or if custom prompt */}
            {(isGlobalSettings || promptType === CUSTOM_PROMPT_SYMBOL) && (
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
                className={isEditing ? 'system-prompt-textarea-editable' : ''}
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
                  tabIndex={isExpanded ? undefined : -1}
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
                  tabIndex={isExpanded ? undefined : -1}
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
                  ref={evaluatedPromptRef}
                  tabIndex={-1}
                  style={{
                    whiteSpace: 'pre-wrap',
                    background: '#f9f9f9',
                    padding: '10px',
                    borderRadius: '5px',
                    overflowY: 'auto',
                    maxHeight: '200px',
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
