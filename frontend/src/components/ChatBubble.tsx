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
}) => {
  const [isExpanded, setIsExpanded] = useState(heighten === true); // Heighten toggle
  const [isContentVisible, setIsContentVisible] = useState(collapsed !== true); // Content toggle
  const [showHeightenToggleChevron, setShowHeightenToggleChevron] = useState(false); // Is heighten toggle required?
  const contentRef = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    if (heighten !== undefined && contentRef.current) {
      // Temporarily set max-height to none to measure full content height
      contentRef.current.style.maxHeight = 'none';
      const contentHeight = contentRef.current.scrollHeight;
      contentRef.current.style.maxHeight = ''; // Reset max-height

      // Compare with 30vh
      const collapsedHeightPx = (30 / 100) * window.innerHeight;
      setShowHeightenToggleChevron(contentHeight > collapsedHeightPx);
    }
  }, [children, heighten]);

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

  const handleHeaderClick = onHeaderClick || (collapsed !== undefined ? toggleContentVisibility : undefined);
  const handleContentChevronClick = (event: React.MouseEvent) => {
    event.stopPropagation();
    toggleContentVisibility();
  };
  const handleHeightenChevronClick = toggleHeighten;

  const bubbleContentStyle: React.CSSProperties = {};
  if (!isContentVisible) {
    bubbleContentStyle.display = 'none'; // Hide content completely
  } else if (heighten !== undefined && !isExpanded && showHeightenToggleChevron) {
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
          style={bubbleContentStyle}
        >
          {isContentVisible && children}
        </div>
        {heighten !== undefined &&
          showHeightenToggleChevron &&
          !showHeaderToggle &&
          isContentVisible && ( // Only show toggle button if no chevron in header and content is visible
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
