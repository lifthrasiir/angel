import React, { useLayoutEffect, useRef, useState, forwardRef, useImperativeHandle } from 'react';
import { FaChevronDown, FaChevronUp, FaChevronCircleDown, FaChevronCircleUp } from 'react-icons/fa';
import { handleEnterKey } from '../utils/enterKeyHandler';

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
  sideContents?: React.ReactNode; // Content to display beside the bubble
}

export interface ChatBubbleRef {
  saveEdit: () => void;
}

const ChatBubble = forwardRef<ChatBubbleRef, ChatBubbleProps>(
  (
    {
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
      sideContents,
    },
    ref,
  ) => {
    const [isExpanded, setIsExpanded] = useState(heighten === true); // Heighten toggle
    const [isContentVisible, setIsContentVisible] = useState(collapsed !== true); // Content toggle
    const [showHeightenToggleChevron, setShowHeightenToggleChevron] = useState(false); // Is heighten toggle required?
    const contentRef = useRef<HTMLDivElement>(null);
    const textareaRef = useRef<HTMLTextAreaElement>(null);
    const [currentEditText, setCurrentEditText] = useState(editText || '');
    const [wasEditing, setWasEditing] = useState(false); // Track previous editing state

    useLayoutEffect(() => {
      if (heighten !== undefined && contentRef.current && !isEditing) {
        // Check if content is too long - if so, skip expensive measurement
        const getChildrenText = (children: React.ReactNode): string => {
          if (typeof children === 'string') return children;
          if (typeof children === 'number') return children.toString();
          if (React.isValidElement(children)) {
            return getChildrenText(children.props.children);
          }
          return '';
        };
        const contentText = getChildrenText(children);
        const isLongContent = contentText.length > 10000;

        if (isLongContent) {
          // For very long content, always show toggle without measurement
          setShowHeightenToggleChevron(true);
          return;
        }

        // Clone the element to measure scrollHeight without affecting layout
        const clone = contentRef.current.cloneNode(true) as HTMLElement;
        clone.style.position = 'absolute';
        clone.style.visibility = 'hidden';
        clone.style.height = 'auto';
        clone.style.maxHeight = 'none';
        clone.style.overflow = 'visible';
        clone.style.top = '-9999px';
        clone.style.left = '-9999px';

        document.body.appendChild(clone);
        const contentHeight = clone.scrollHeight;
        document.body.removeChild(clone);

        // Compare with 30vh
        const collapsedHeightPx = (30 / 100) * window.innerHeight;
        const needsToggle = contentHeight > collapsedHeightPx;
        setShowHeightenToggleChevron(needsToggle);
      }
    }, [children, heighten, isEditing]);

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

    // Expose methods via ref
    useImperativeHandle(
      ref,
      () => ({
        saveEdit: handleEditSave,
      }),
      [currentEditText, onEditSave],
    );

    const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      // Use the common Enter key handler
      const isHandled = handleEnterKey(e, {
        onSendOrConfirm: handleEditSave,
        value: currentEditText,
      });

      // If Enter key was handled, don't process Escape
      if (isHandled) {
        return;
      }

      // Handle Escape key for cancel
      if (e.key === 'Escape') {
        e.preventDefault();
        handleEditCancel();
      }
    };

    const handleInput = (e: React.FormEvent<HTMLTextAreaElement>) => {
      const target = e.target as HTMLTextAreaElement;
      const newValue = target.value;

      // For very long content, skip expensive measurement and use max height
      if (newValue.length > 10000) {
        target.style.height = '60vh'; // Use max height directly
        setCurrentEditText(newValue);
        return;
      }

      // Create a temporary div with same text styles to measure height
      const measurer = document.createElement('div');
      const computedStyle = window.getComputedStyle(target);

      // Copy relevant styles
      measurer.style.position = 'absolute';
      measurer.style.visibility = 'hidden';
      measurer.style.height = 'auto';
      measurer.style.width = computedStyle.width;
      measurer.style.padding = computedStyle.padding;
      measurer.style.border = computedStyle.border;
      measurer.style.fontSize = computedStyle.fontSize;
      measurer.style.fontFamily = computedStyle.fontFamily;
      measurer.style.lineHeight = computedStyle.lineHeight;
      measurer.style.whiteSpace = 'pre-wrap';
      measurer.style.wordWrap = 'break-word';
      measurer.style.top = '-9999px';
      measurer.style.left = '-9999px';

      measurer.textContent = newValue;
      document.body.appendChild(measurer);

      const measuredHeight = measurer.scrollHeight;
      document.body.removeChild(measurer);

      target.style.height = measuredHeight + 'px';
      setCurrentEditText(newValue);
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

    const bubbleContent = (
      <>
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
      </>
    );

    const bubbleElement = <div className={`chat-bubble ${bubbleClassName || ''}`}>{bubbleContent}</div>;

    if (sideContents) {
      return (
        <div id={messageId} className={`chat-message-container ${containerClassName || ''}`}>
          <div className="chat-bubble-container">
            {bubbleElement}
            {sideContents}
          </div>
          {messageInfo}
        </div>
      );
    }

    return (
      <div id={messageId} className={`chat-message-container ${containerClassName || ''}`}>
        {bubbleElement}
        {messageInfo}
      </div>
    );
  },
);

ChatBubble.displayName = 'ChatBubble';

export default ChatBubble;
