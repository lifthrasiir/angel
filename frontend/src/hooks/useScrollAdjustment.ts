import { useRef, useEffect, useCallback } from 'react';

interface UseScrollAdjustmentProps {
  chatAreaRef: React.RefObject<HTMLDivElement>;
}

export const useScrollAdjustment = ({ chatAreaRef }: UseScrollAdjustmentProps) => {
  const resizeObserverRef = useRef<ResizeObserver | null>(null);
  const lastScrollHeightRef = useRef(0);
  const wasAtBottomRef = useRef(false);

  // Helper function to check if user is at or near the bottom
  const isAtBottom = (element: HTMLElement, threshold: number = 50): boolean => {
    return element.scrollHeight - element.scrollTop - element.clientHeight <= threshold;
  };

  const adjustScroll = useCallback(
    (oldScrollHeight: number, oldScrollTop: number) => {
      const chatAreaElement = chatAreaRef.current;
      if (!chatAreaElement) {
        console.warn('adjustScroll: chatAreaElement is null. Cannot adjust scroll.');
        return;
      }

      // Store whether user was at bottom before content changes
      wasAtBottomRef.current = isAtBottom(chatAreaElement);

      // Initial scroll adjustment
      requestAnimationFrame(() => {
        const initialNewScrollHeight = chatAreaElement.scrollHeight;
        const initialNewScrollTop = oldScrollTop + (initialNewScrollHeight - oldScrollHeight);
        chatAreaElement.scrollTop = initialNewScrollTop;
        lastScrollHeightRef.current = initialNewScrollHeight;

        // Setup ResizeObserver to handle content size changes (including image loading)
        if (resizeObserverRef.current) {
          resizeObserverRef.current.disconnect();
        }

        resizeObserverRef.current = new ResizeObserver(() => {
          const currentScrollHeight = chatAreaElement.scrollHeight;
          if (currentScrollHeight !== lastScrollHeightRef.current) {
            // Only follow scroll if user was at bottom before this change
            if (wasAtBottomRef.current) {
              const scrollDiff = currentScrollHeight - lastScrollHeightRef.current;
              chatAreaElement.scrollTop += scrollDiff;
            }
            lastScrollHeightRef.current = currentScrollHeight;
          }
        });

        // Observe the chat area and all its child elements for size changes
        resizeObserverRef.current.observe(chatAreaElement);

        // Also observe all images within the chat area
        const images = chatAreaElement.querySelectorAll('img');
        images.forEach((img) => resizeObserverRef.current!.observe(img));
      });
    },
    [chatAreaRef],
  );

  // Public method to handle scroll-to-bottom behavior for new messages
  const scrollToBottom = useCallback(() => {
    const chatAreaElement = chatAreaRef.current;
    if (!chatAreaElement) {
      console.warn('scrollToBottom: chatAreaElement is null. Cannot scroll.');
      return;
    }

    requestAnimationFrame(() => {
      chatAreaElement.scrollTop = chatAreaElement.scrollHeight;
      wasAtBottomRef.current = true; // Mark as at bottom after explicit scroll
    });
  }, [chatAreaRef]);

  // Public method to handle dynamic content loading when user might be at bottom
  const handleContentLoad = useCallback(() => {
    const chatAreaElement = chatAreaRef.current;
    if (!chatAreaElement) {
      return;
    }

    // Store whether user is currently at bottom
    wasAtBottomRef.current = isAtBottom(chatAreaElement);
    lastScrollHeightRef.current = chatAreaElement.scrollHeight;

    // Setup ResizeObserver to watch for size changes
    if (resizeObserverRef.current) {
      resizeObserverRef.current.disconnect();
    }

    resizeObserverRef.current = new ResizeObserver(() => {
      const currentScrollHeight = chatAreaElement.scrollHeight;
      if (currentScrollHeight !== lastScrollHeightRef.current) {
        // Only follow scroll if user was at bottom before this change
        if (wasAtBottomRef.current) {
          const scrollDiff = currentScrollHeight - lastScrollHeightRef.current;
          chatAreaElement.scrollTop += scrollDiff;
        }
        lastScrollHeightRef.current = currentScrollHeight;
      }
    });

    // Observe the chat area and all images
    resizeObserverRef.current.observe(chatAreaElement);

    // Also observe all images within the chat area
    const images = chatAreaElement.querySelectorAll('img');
    images.forEach((img) => resizeObserverRef.current!.observe(img));
  }, [chatAreaRef]);

  // Cleanup observer on unmount
  useEffect(() => {
    return () => {
      if (resizeObserverRef.current) {
        resizeObserverRef.current.disconnect();
        resizeObserverRef.current = null;
      }
    };
  }, []);

  return { adjustScroll, scrollToBottom, handleContentLoad };
};
