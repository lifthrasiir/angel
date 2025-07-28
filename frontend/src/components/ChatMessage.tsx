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
  text?: string;
  type?: "model" | "thought" | "system" | "user" | "function_call" | "function_response";
  functionCall?: any;
  functionResponse?: any;
}

const ChatMessage: React.FC<ChatMessageProps> = React.memo(({ role, text, type, functionCall, functionResponse }) => {
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
  } else if (type === 'function_call') {
    messageStyle.backgroundColor = '#e6f7ff'; // Light blue background
    messageStyle.borderLeft = '4px solid #1890ff'; // Blue left border
    messageStyle.padding = '10px';
    messageStyle.marginBottom = '10px';
  } else if (type === 'function_response') {
    messageStyle.backgroundColor = '#f6ffed'; // Light green background
    messageStyle.borderLeft = '4px solid #52c41a'; // Green left border
    messageStyle.padding = '10px';
    messageStyle.marginBottom = '10px';
  }

  let contentText = text;
  if (type === 'thought') {
    const [subject, description] = (text || '').split('\n', 2);
    contentText = `**Thought: ${subject}**\n${description || ''}`;
  } else if (type === 'function_call' && functionCall) {
    contentText = `**Function Call:** ${functionCall.name}\n\`\`\`json\n${JSON.stringify(functionCall.args, null, 2)}\n\`\`\``;
  } else if (type === 'function_response' && functionResponse) {
    let responseText = functionResponse.response;
    try {
      // Attempt to parse as JSON, then re-stringify for consistent formatting
      const parsedResponse = typeof functionResponse.response === 'string' ? JSON.parse(functionResponse.response) : functionResponse.response;
      responseText = JSON.stringify(parsedResponse, null, 2);
    } catch (e) {
      // If not valid JSON, use the raw string
      console.warn("Function response is not valid JSON:", functionResponse.response);
    }
    contentText = `**Function Response:**\n\`\`\`json\n${responseText}\n\`\`\``;
  } else if (role === 'user') {
    // No Markdown formatting on user inputs
    return (
      <div className="message" style={messageStyle}>
        <strong>You:</strong> {text}
      </div>
    );
  } else {
    contentText = `**Agent:** ${text}`;
  }

  const preprocessedText = smartPreprocessMarkdown(contentText);

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
        {preprocessedText}
      </ReactMarkdown>
    </div>
  );
}); // Correct closing for React.memo wrapped functional component

export default ChatMessage;
