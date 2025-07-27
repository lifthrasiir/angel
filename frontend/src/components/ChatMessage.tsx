import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import breaks from 'remark-breaks';
import remarkHtml from 'remark-html';
import { smartPreprocessMarkdown } from '../lib/markdown-preprocessor';
import { rehypeHandleRawNodes } from '../lib/rehype/rehype-handle-raw-nodes';

interface ChatMessageProps {
  role: string;
  text: string;
}

const ChatMessage: React.FC<ChatMessageProps> = ({ role, text }) => {
  if (role === 'user') {
    return (
      <div className="message">
        <strong>You:</strong> {text}
      </div>
    );
  }

  const preprocessedText = smartPreprocessMarkdown(text);

  return (
    <div className="message">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, breaks, remarkHtml]}
        rehypePlugins={[rehypeHandleRawNodes]}
        components={{
          p: ({ node, ...props }) => {
            const children = React.Children.toArray(props.children);
            if (children.length > 0 && typeof children[0] === 'string') {
              const firstChild = children[0] as string;
              if (firstChild.startsWith('Agent:')) {
                return <p {...props} />;
              }
            }
            return <p {...props} />;
          },
        }}
      >
        {`**Agent:** ${preprocessedText}`}
      </ReactMarkdown>
    </div>
  );
};

export default ChatMessage;
