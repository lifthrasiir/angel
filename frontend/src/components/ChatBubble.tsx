import React, { useLayoutEffect, useRef, useState } from 'react';
import { FaChevronDown, FaChevronUp, FaChevronCircleDown, FaChevronCircleUp } from 'react-icons/fa';

interface ChatBubbleProps {
  messageId?: string;
  containerClassName?: string;
  bubbleClassName?: string;
  messageInfo?: React.ReactNode;
  children?: React.ReactNode;
  heighten?: boolean; // undefined: auto fixed, false: 30vh togglable, true: auto togglable
  collapsed?: boolean; // undefined: visible fixed, false: visible togglable, true: hidden togglable
  title?: React.ReactNode; // Optional title for the bubble
  showHeaderToggle?: boolean; // Optional: show chevron icon for expand/collapse in header
  onHeaderClick?: () => void; // Optional callback for header click
  // Edit functionality
  editText?: string; // Current edit text when in edit mode
  isEditing?: boolean; // Whether this bubble is in edit mode
  onEditSave?: (newText: string) => void; // Callback when edit is saved
  onEditCancel?: () => void; // Callback when edit is cancelled
  onEditStart?: () => void; // Callback when edit mode should start
  disableEdit?: boolean; // Disable edit functionality
}

const ChatBubble: React.FC<ChatBubbleProps> = ({
  messageId,
  containerClassName,
  bubbleClassName,
  messageInfo,
  children,
  heighten,
  collapsed,
  title,
  showHeaderToggle = false,
  onHeaderClick,
  editText,
  isEditing = false,
  onEditSave,
  onEditCancel,
}) => {
  const [isExpanded, setIsExpanded] = useState(heighten === true); // Heighten toggle
  const [isContentVisible, setIsContentVisible] = useState(collapsed !== true); // Content toggle
  const [showHeightenToggleChevron, setShowHeightenToggleChevron] = useState(false); // Is heighten toggle required?
  const contentRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [currentEditText, setCurrentEditText] = useState(editText || '');
  const [wasEditing, setWasEditing] = useState(false); // Track previous editing state

  useLayoutEffect(() => {
    if (heighten !== undefined && contentRef.current) {
      // Store original style
      const originalMaxHeight = contentRef.current.style.maxHeight;
      const originalOverflow = contentRef.current.style.overflowY;

      // Temporarily set max-height to none to measure full content height
      contentRef.current.style.maxHeight = 'none';
      contentRef.current.style.overflowY = 'visible';
      const contentHeight = contentRef.current.scrollHeight;

      // Restore original style
      contentRef.current.style.maxHeight = originalMaxHeight;
      contentRef.current.style.overflowY = originalOverflow;

      // Compare with 30vh
      const collapsedHeightPx = (30 / 100) * window.innerHeight;
      const needsToggle = contentHeight > collapsedHeightPx;
      setShowHeightenToggleChevron(needsToggle);
    }
  }, [children, heighten]);

  // Update edit text when prop changes
  React.useEffect(() => {
    setCurrentEditText(editText || '');
  }, [editText]);

  // Focus textarea when entering edit mode
  React.useEffect(() => {
    if (isEditing && textareaRef.current) {
      textareaRef.current.focus();
      // Adjust height on initial render if it's in editing mode
      textareaRef.current.style.height = 'auto';
      textareaRef.current.style.height = textareaRef.current.scrollHeight + 'px';
    }
  }, [isEditing]);

  // Reset expanded state when exiting edit mode
  React.useEffect(() => {
    // Only execute when actually transitioning from editing to not editing
    if (wasEditing && !isEditing && heighten === false) {
      setIsExpanded(false);
    }
    // Update wasEditing for next comparison
    setWasEditing(isEditing);
  }, [isEditing, heighten, wasEditing]);

  const toggleHeighten = () => {
    if (heighten !== undefined) {
      setIsExpanded(!isExpanded);
    }
  };

  const toggleContentVisibility = () => {
    if (collapsed !== undefined) {
      setIsContentVisible(!isContentVisible);
    }
  };

  // Edit handlers
  const handleEditSave = () => {
    if (onEditSave) {
      onEditSave(currentEditText);
    }
  };

  const handleEditCancel = () => {
    if (onEditCancel) {
      onEditCancel();
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && e.ctrlKey) {
      e.preventDefault();
      handleEditSave();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      handleEditCancel();
    }
  };

  const handleInput = (e: React.FormEvent<HTMLTextAreaElement>) => {
    const target = e.target as HTMLTextAreaElement;
    target.style.height = 'auto';
    target.style.height = target.scrollHeight + 'px';
    setCurrentEditText(target.value);
  };

  const handleHeaderClick = onHeaderClick || (collapsed !== undefined ? toggleContentVisibility : undefined);
  const handleContentChevronClick = (event: React.MouseEvent) => {
    event.stopPropagation();
    toggleContentVisibility();
  };
  const handleHeightenChevronClick = toggleHeighten;

  const bubbleContentStyle: React.CSSProperties = {};
  if (!isContentVisible) {
    bubbleContentStyle.display = 'none'; // Hide content completely
  } else if (heighten !== undefined && !isExpanded && showHeightenToggleChevron && !isEditing) {
    bubbleContentStyle.maxHeight = '30vh';
    bubbleContentStyle.overflowY = 'auto';
  }

  return (
    <div id={messageId} className={`chat-message-container ${containerClassName || ''}`}>
      <div className={`chat-bubble ${bubbleClassName || ''}`}>
        {(title || (collapsed !== undefined && showHeaderToggle)) && (
          <div
            className={`chat-bubble-header ${handleHeaderClick ? 'clickable-header' : ''}`}
            onClick={handleHeaderClick}
          >
            {title && <span className="chat-bubble-title">{title}</span>}
            {collapsed !== undefined && showHeaderToggle && (
              <span className="chat-bubble-chevron" onClick={handleContentChevronClick}>
                {isContentVisible ? <FaChevronUp /> : <FaChevronDown />}
              </span>
            )}
          </div>
        )}
        <div
          ref={contentRef}
          className={`chat-bubble-content ${heighten !== undefined && !isExpanded && showHeightenToggleChevron ? 'collapsed' : 'expanded'}`}
          style={{
            ...bubbleContentStyle,
            // Force maxHeight as a fallback
            maxHeight: bubbleContentStyle.maxHeight || undefined,
          }}
        >
          {isContentVisible &&
            (isEditing ? (
              <div
                style={{
                  display: 'grid',
                  gridTemplateColumns: '1fr',
                  gridTemplateRows: '1fr',
                  overflow: 'hidden',
                  maxHeight: '60vh',
                }}
              >
                {/* Original content for size reference - hidden in edit mode */}
                <div
                  style={{
                    visibility: 'hidden',
                    whiteSpace: 'pre-wrap',
                    wordWrap: 'break-word',
                    maxWidth: '100%',
                    overflow: 'auto',
                    maxHeight: '60vh',
                    minHeight: '40px',
                    minWidth: '100px',
                    gridColumn: '1',
                    gridRow: '1',
                  }}
                >
                  {children}
                </div>

                {/* Textarea overlayed in edit mode */}
                <textarea
                  ref={textareaRef}
                  style={{
                    gridColumn: '1',
                    gridRow: '1',
                    padding: '0',
                    border: '1px solid #ccc',
                    borderRadius: '4px',
                    resize: 'both',
                    boxSizing: 'border-box',
                    fontFamily: 'inherit',
                    fontSize: 'inherit',
                    lineHeight: 'inherit',
                    overflow: 'auto',
                    minWidth: '400px',
                    minHeight: '60px',
                    maxHeight: '60vh',
                  }}
                  value={currentEditText}
                  onChange={(e) => setCurrentEditText(e.target.value)}
                  onKeyDown={handleKeyDown}
                  onBlur={handleEditCancel}
                  onInput={handleInput}
                  placeholder="Edit your message..."
                />
              </div>
            ) : (
              children
            ))}
        </div>
        {heighten !== undefined &&
          showHeightenToggleChevron &&
          !showHeaderToggle &&
          isContentVisible &&
          !isEditing && ( // Don't show toggle button when editing
            <div className="chat-bubble-toggle-button" onClick={handleHeightenChevronClick}>
              {isExpanded ? <FaChevronCircleUp /> : <FaChevronCircleDown />}
            </div>
          )}
      </div>
      {messageInfo}
    </div>
  );
};

export default ChatBubble;
