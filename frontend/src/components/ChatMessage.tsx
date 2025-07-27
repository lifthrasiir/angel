import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import breaks from 'remark-breaks';
import remarkHtml from 'remark-html';
import remarkMath from 'remark-math'; // Add this import
import rehypeKatex from 'rehype-katex'; // Add this import
import 'katex/dist/katex.min.css'; // Add this import for KaTeX CSS
import { smartPreprocessMarkdown } from '../lib/markdown-preprocessor';
import { rehypeHandleRawNodes } from '../lib/rehype/rehype-handle-raw-nodes';

interface ChatMessageProps {
  role: string;
  text: string;
}

const ChatMessage: React.FC<ChatMessageProps> = React.memo(({ role, text }) => {
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
        remarkPlugins={[remarkGfm, breaks, remarkHtml, remarkMath]} // Add remarkMath
        rehypePlugins={[rehypeHandleRawNodes, rehypeKatex]} // Add rehypeKatex
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
}); // Correct closing for React.memo wrapped functional component

export default ChatMessage;
