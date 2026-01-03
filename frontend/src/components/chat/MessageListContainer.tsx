import type React from 'react';
import { useEffect, useRef } from 'react';
import { useScrollAdjustment } from '../../hooks/useScrollAdjustment';

interface MessageListContainerProps {
  chatAreaRef: React.RefObject<HTMLDivElement>;
  sessionId: string | undefined;
  messages: any[];
  hasMoreMessages: boolean;
  isLoading: boolean;
  loadEarlierMessages: () => void;
  children: React.ReactNode;
}

export const MessageListContainer: React.FC<MessageListContainerProps> = ({
  chatAreaRef,
  sessionId,
  messages,
  hasMoreMessages,
  isLoading,
  loadEarlierMessages,
  children,
}) => {
  // Scroll management
  const { scrollToBottom, adjustScroll } = useScrollAdjustment({ chatAreaRef });
  const lastMessageIdRef = useRef<string | null>(null);
  const isInitialLoadRef = useRef(true);
  const firstMessageIdRef = useRef<string | null>(null);
  const scrollStateRef = useRef({ scrollHeight: 0, scrollTop: 0 });

  // Reset initial load flag when session changes
  useEffect(() => {
    isInitialLoadRef.current = true;
    lastMessageIdRef.current = null;
    firstMessageIdRef.current = null;
    scrollStateRef.current = { scrollHeight: 0, scrollTop: 0 };
  }, [sessionId]);

  // Auto-scroll to bottom on initial load
  useEffect(() => {
    if (messages.length > 0 && isInitialLoadRef.current) {
      isInitialLoadRef.current = false;
      scrollToBottom();
    }
  }, [messages.length, scrollToBottom]);

  // Adjust scroll position when earlier messages are loaded (prepended)
  useEffect(() => {
    if (messages.length === 0) return;

    const chatArea = chatAreaRef.current;
    if (!chatArea) return;

    const firstMessageId = messages[0].id;

    // Save current scroll state BEFORE checking for changes
    const currentScrollState = {
      scrollHeight: chatArea.scrollHeight,
      scrollTop: chatArea.scrollTop,
    };

    // If first message changed, earlier messages were loaded
    if (firstMessageIdRef.current !== null && firstMessageIdRef.current !== firstMessageId) {
      // Use the saved scroll state from PREVIOUS render (before new messages were added)
      console.log('Adjusting scroll - old:', scrollStateRef.current, 'new:', currentScrollState);
      adjustScroll(scrollStateRef.current.scrollHeight, scrollStateRef.current.scrollTop);
    }

    // Update refs for next render
    firstMessageIdRef.current = firstMessageId;
    scrollStateRef.current = currentScrollState;
  }, [messages, chatAreaRef, adjustScroll]);

  // Auto-scroll to bottom when new messages arrive at the end
  useEffect(() => {
    if (messages.length === 0) return;

    const lastMessage = messages[messages.length - 1];
    const lastMessageId = lastMessage.id;

    // Only scroll if the last message changed (new message at end)
    if (lastMessageIdRef.current !== lastMessageId) {
      lastMessageIdRef.current = lastMessageId;
      scrollToBottom();
    }
  }, [messages, scrollToBottom]);

  // Setup content load handler for dynamic content (images, etc.)
  useEffect(() => {
    const handleContentLoad = () => {
      // Trigger scroll adjustment when content loads
      const chatArea = chatAreaRef.current;
      if (!chatArea) return;

      // Check if we're near bottom and should auto-scroll
      const isNearBottom = chatArea.scrollHeight - chatArea.scrollTop - chatArea.clientHeight < 100;
      if (isNearBottom) {
        scrollToBottom();
      }
    };

    // Observe image loads
    const images = chatAreaRef.current?.querySelectorAll('img');
    images?.forEach((img) => {
      if (img.complete) {
        handleContentLoad();
      } else {
        img.addEventListener('load', handleContentLoad);
        img.addEventListener('error', handleContentLoad);
      }
    });

    return () => {
      images?.forEach((img) => {
        img.removeEventListener('load', handleContentLoad);
        img.removeEventListener('error', handleContentLoad);
      });
    };
  }, [messages, chatAreaRef, scrollToBottom]);

  // Load earlier messages when scrolling to top
  useEffect(() => {
    const chatArea = chatAreaRef.current;
    if (!chatArea) return;

    const handleScroll = () => {
      // Update scroll state on every scroll event
      scrollStateRef.current = {
        scrollHeight: chatArea.scrollHeight,
        scrollTop: chatArea.scrollTop,
      };

      const scrollTop = chatArea.scrollTop;
      const scrollThreshold = 100; // Load when within 100px of top

      if (scrollTop <= scrollThreshold && hasMoreMessages && !isLoading) {
        console.log('Scroll event - loading earlier messages');
        loadEarlierMessages();
      }
    };

    chatArea.addEventListener('scroll', handleScroll);
    return () => {
      chatArea.removeEventListener('scroll', handleScroll);
    };
  }, [chatAreaRef, hasMoreMessages, isLoading, loadEarlierMessages]);

  // Auto-load earlier messages if viewport isn't filled
  useEffect(() => {
    const chatArea = chatAreaRef.current;
    if (!chatArea || !hasMoreMessages || isLoading) return;

    // Check if content height is less than viewport height
    const hasScroll = chatArea.scrollHeight > chatArea.clientHeight;
    if (!hasScroll && messages.length > 0) {
      console.log('Viewport not filled - auto-loading earlier messages');
      loadEarlierMessages();
    }
  }, [chatAreaRef, messages.length, hasMoreMessages, isLoading, loadEarlierMessages]);

  return (
    <div style={{ flexGrow: 1, overflowY: 'auto' }} ref={chatAreaRef}>
      <div
        style={{
          maxWidth: 'var(--chat-container-max-width)',
          margin: '0 auto',
          padding: 'var(--spacing-unit)',
        }}
      >
        {isLoading && <div style={{ textAlign: 'center', padding: '10px' }}>Loading more messages...</div>}
        {children}
        <div ref={lastMessageIdRef as React.RefObject<HTMLDivElement>} />
      </div>
    </div>
  );
};

export default MessageListContainer;
