import { useRef, useEffect, useCallback } from 'react';

interface UseScrollAdjustmentProps {
  chatAreaRef: React.RefObject<HTMLDivElement>;
}

export const useScrollAdjustment = ({ chatAreaRef }: UseScrollAdjustmentProps) => {
  const observerRef = useRef<MutationObserver | null>(null);
  const lastScrollHeightRef = useRef(0);
  const scrollAdjustmentTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const adjustScroll = useCallback(
    (oldScrollHeight: number, oldScrollTop: number) => {
      const chatAreaElement = chatAreaRef.current;
      if (!chatAreaElement) {
        console.warn('adjustScroll: chatAreaElement is null. Cannot adjust scroll.');
        return;
      }

      // Initial scroll adjustment
      requestAnimationFrame(() => {
        const initialNewScrollHeight = chatAreaElement.scrollHeight;
        const initialNewScrollTop = oldScrollTop + (initialNewScrollHeight - oldScrollHeight);
        chatAreaElement.scrollTop = initialNewScrollTop;
        lastScrollHeightRef.current = initialNewScrollHeight;

        // Setup MutationObserver to handle subsequent DOM changes (e.g., image loading)
        if (observerRef.current) {
          observerRef.current.disconnect();
        }

        observerRef.current = new MutationObserver((mutations) => {
          mutations.forEach(() => {
            const currentScrollHeight = chatAreaElement.scrollHeight;
            if (currentScrollHeight !== lastScrollHeightRef.current) {
              const scrollDiff = currentScrollHeight - lastScrollHeightRef.current;
              chatAreaElement.scrollTop += scrollDiff;
              lastScrollHeightRef.current = currentScrollHeight;

              // Reset timeout on change
              if (scrollAdjustmentTimeoutRef.current) {
                clearTimeout(scrollAdjustmentTimeoutRef.current);
              }
              scrollAdjustmentTimeoutRef.current = setTimeout(() => {
                observerRef.current?.disconnect();
                observerRef.current = null;
                scrollAdjustmentTimeoutRef.current = null;
              }, 500); // Disconnect after 500ms of no changes
            }
          });
        });

        observerRef.current.observe(chatAreaElement, { childList: true, subtree: true, attributes: true });
      });
    },
    [chatAreaRef],
  );

  // Cleanup observer on unmount
  useEffect(() => {
    return () => {
      if (observerRef.current) {
        observerRef.current.disconnect();
        observerRef.current = null;
      }
      if (scrollAdjustmentTimeoutRef.current) {
        clearTimeout(scrollAdjustmentTimeoutRef.current);
        scrollAdjustmentTimeoutRef.current = null;
      }
    };
  }, []);

  return { adjustScroll };
};
