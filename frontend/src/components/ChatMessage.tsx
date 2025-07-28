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

  let contentText = text;
  let containerClassName = "chat-message-container";
  let bubbleClassName = "chat-bubble";

  if (role === 'user') {
    containerClassName += " user-message";
    if (type === 'function_response' && functionResponse) {
      bubbleClassName += " agent-function-response"; // Use agent-function-response style
      let responseData = functionResponse.response;
      let responseText: string;

      if (responseData === null || responseData === undefined || responseData === "") {
        responseText = "(empty response)";
      } else if (typeof responseData === 'string') {
        try {
          responseData = JSON.parse(responseData);
          responseText = JSON.stringify(responseData, null, 2);
        } catch (e) {
          console.warn("Function response is not valid JSON string, using raw string:", functionResponse.response);
          responseText = responseData; // Use the raw string if parsing fails
        }
      } else {
        responseText = JSON.stringify(responseData, null, 2);
      }
      const codeContent = responseText;

      return (
        <div className={containerClassName}>
          <div className={`${bubbleClassName} function-message-bubble`}>
            <div className="function-title-bar function-response-title-bar">
              Function Response:
            </div>
            <pre className="function-code-block">
              {codeContent}
            </pre>
          </div>
        </div>
      );
    } else {
      // For regular user messages, we don't use ReactMarkdown, just render plain text
      return (
        <div className={containerClassName}>
          <div className={bubbleClassName}>
            {text}
          </div>
        </div>
      );
    }
  } else { // role === 'model' or 'system'
    containerClassName += " agent-message";
    if (type === 'thought') {
      bubbleClassName += " agent-thought";
      const [subject, description] = (text || '').split('\n', 2);
      contentText = `**Thought: ${subject}**\n${description || ''}`;
      const preprocessedText = smartPreprocessMarkdown(contentText || '');
      return (
        <div className={containerClassName}>
          <div className={bubbleClassName}>
            <ReactMarkdown
              remarkPlugins={katexLoaded ? [remarkGfm, breaks, remarkHtml, remarkMath] : [remarkGfm, breaks, remarkHtml]}
              rehypePlugins={katexLoaded ? [rehypeHandleRawNodes, rehypeKatex] : [rehypeHandleRawNodes]}
              components={{
                p: ({ node, ...props }) => {
                  return <p {...props} />;
                },
              }}
            >
              {preprocessedText}
            </ReactMarkdown>
          </div>
        </div>
      );
    } else if (type === 'function_call' && functionCall) {
      bubbleClassName += " agent-function-call";
      const codeContent = JSON.stringify(functionCall.args, null, 2);
      return (
        <div className={containerClassName}>
          <div className={`${bubbleClassName} function-message-bubble`}>
            <div className="function-title-bar function-call-title-bar">
              Function Call: {functionCall.name}
            </div>
            <pre className="function-code-block">
              {codeContent}
            </pre>
          </div>
        </div>
      );
    } else if (type === 'function_response' && functionResponse) {
      bubbleClassName += " agent-function-response";
      let responseData = functionResponse.response;
      let responseText: string;

      if (responseData === null || responseData === undefined || responseData === "") {
        responseText = "(empty response)";
      } else if (typeof responseData === 'string') {
        try {
          responseData = JSON.parse(responseData);
          responseText = JSON.stringify(responseData, null, 2);
        } catch (e) {
          console.warn("Function response is not valid JSON string, using raw string:", functionResponse.response);
          responseText = responseData; // Use the raw string if parsing fails
        }
      } else {
        responseText = JSON.stringify(responseData, null, 2);
      }
      const codeContent = responseText;
      return (
        <div className={containerClassName}>
          <div className={`${bubbleClassName} function-message-bubble`}>
            <div className="function-title-bar function-response-title-bar">
              Function Response:
            </div>
            <pre className="function-code-block">
              {codeContent}
            </pre>
          </div>
        </div>
      );
    }
    // For other model/system messages, we use ReactMarkdown
    const preprocessedText = smartPreprocessMarkdown(contentText || '');

    return (
      <div className={containerClassName}>
        <div className={bubbleClassName}>
          <ReactMarkdown
            remarkPlugins={katexLoaded ? [remarkGfm, breaks, remarkHtml, remarkMath] : [remarkGfm, breaks, remarkHtml]}
            rehypePlugins={katexLoaded ? [rehypeHandleRawNodes, rehypeKatex] : [rehypeHandleRawNodes]}
            components={{
              p: ({ node, ...props }) => {
                return <p {...props} />;
              },
            }}
          >
            {preprocessedText}
          </ReactMarkdown>
        </div>
      </div>
    );
  }
}); // Correct closing for React.memo wrapped functional component

export default ChatMessage;
