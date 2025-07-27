import React, { useState, useEffect } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import breaks from 'remark-breaks';
import remarkHtml from 'remark-html';
import 'katex/dist/katex.min.css'; // Add this import for KaTeX CSS
import { smartPreprocessMarkdown } from '../lib/markdown-preprocessor';
import { rehypeHandleRawNodes } from '../lib/rehype/rehype-handle-raw-nodes';

interface ChatMessageProps {
  role: string;
  text: string;
  type?: "model" | "thought" | "system" | "user";
}

const ChatMessage: React.FC<ChatMessageProps> = React.memo(({ role, text, type }) => {
  const [katexLoaded, setKatexLoaded] = useState(false);
  const [remarkMath, setRemarkMath] = useState<any>(null);
  const [rehypeKatex, setRehypeKatex] = useState<any>(null);

  useEffect(() => {
    Promise.all([
      import('remark-math'),
      import('rehype-katex'),
    ]).then(([remarkMathModule, rehypeKatexModule]) => {
      setRemarkMath(() => remarkMathModule.default);
      setRehypeKatex(() => rehypeKatexModule.default);
      setKatexLoaded(true);
    }).catch(error => {
      console.error("Failed to load KaTeX modules:", error);
    });
  }, []);

  const messageStyle: React.CSSProperties = {};
  if (type === 'thought') {
    messageStyle.opacity = 0.5;
  }

  if (role === 'user') {
    return (
      <div className="message" style={messageStyle}>
        <strong>You:</strong> {text}
      </div>
    );
  }

  const preprocessedText = smartPreprocessMarkdown(text);

  return (
    <div className="message" style={messageStyle}>
      <ReactMarkdown
        remarkPlugins={katexLoaded ? [remarkGfm, breaks, remarkHtml, remarkMath] : [remarkGfm, breaks, remarkHtml]} // Conditionally add remarkMath
        rehypePlugins={katexLoaded ? [rehypeHandleRawNodes, rehypeKatex] : [rehypeHandleRawNodes]} // Conditionally add rehypeKatex
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
