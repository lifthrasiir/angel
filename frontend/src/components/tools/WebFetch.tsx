import React from 'react';
import { validateExactKeys } from '../../utils/functionMessageValidation';
import {
  registerFunctionCallComponent,
  registerFunctionResponseComponent,
  registerFunctionPairComponent,
  FunctionCallMessageProps,
  FunctionResponseMessageProps,
  FunctionPairComponentProps,
} from '../../utils/functionMessageRegistry';
import ChatBubble from '../ChatBubble';
import MarkdownRenderer from '../MarkdownRenderer';

const argsKeys = { prompt: 'string' } as const;

// Extract URL from prompt for title display
const extractUrlFromPrompt = (prompt: string): string => {
  const urlRegex = /https?:\/\/[^\s<>"{}|\\^`\[\]]+/i;
  const match = prompt.match(urlRegex);
  return match ? match[0] : prompt;
};

const WebFetchCall: React.FC<FunctionCallMessageProps> = ({ functionCall, messageId, messageInfo, children }) => {
  const args = functionCall.args;
  if (!validateExactKeys(args, argsKeys)) {
    return children;
  }

  const urlOrPrompt = extractUrlFromPrompt(args.prompt);
  const displayTitle = urlOrPrompt.includes('http') ? urlOrPrompt : args.prompt;

  return (
    <ChatBubble
      messageId={messageId}
      containerClassName="agent-message"
      bubbleClassName="agent-function-call function-message-bubble"
      messageInfo={messageInfo}
      title={
        <>
          web_fetch: <code>{displayTitle}</code>
        </>
      }
    />
  );
};

const responseKeys = { llmContent: 'string', returnDisplay: 'string' } as const;

const WebFetchResponse: React.FC<FunctionResponseMessageProps> = ({
  functionResponse,
  messageId,
  messageInfo,
  children,
}) => {
  const response = functionResponse.response;
  if (!validateExactKeys(response, responseKeys)) {
    return children;
  }

  return (
    <ChatBubble
      messageId={messageId}
      containerClassName="user-message"
      bubbleClassName="function-message-bubble"
      messageInfo={messageInfo}
    >
      <MarkdownRenderer content={response.llmContent || ''} />

      {/* Return display message */}
      {response.returnDisplay && (
        <div style={{ marginTop: '8px' }}>
          <span
            style={{
              fontSize: '12px',
              color: '#888',
              fontStyle: 'italic',
            }}
          >
            {response.returnDisplay}
          </span>
        </div>
      )}
    </ChatBubble>
  );
};

const WebFetchPair: React.FC<FunctionPairComponentProps> = ({
  functionCall,
  functionResponse,
  onToggleView,
  responseMessageInfo,
  children,
}) => {
  const args = functionCall.args;
  const response = functionResponse.response;

  if (!validateExactKeys(args, argsKeys) || !validateExactKeys(response, responseKeys)) {
    return children;
  }

  const urlOrPrompt = extractUrlFromPrompt(args.prompt);
  const displayTitle = urlOrPrompt.includes('http') ? urlOrPrompt : args.prompt;

  return (
    <ChatBubble
      containerClassName="function-pair-combined-container"
      bubbleClassName="function-combined-bubble"
      messageInfo={responseMessageInfo}
      heighten={false}
      collapsed={true}
      title={
        <>
          web_fetch: <code>{displayTitle}</code>
        </>
      }
      showHeaderToggle={true}
      onHeaderClick={onToggleView}
    >
      {/* Show full prompt if it's different from the title (when title shows only URL) */}
      {urlOrPrompt !== args.prompt && (
        <div style={{ marginBottom: '12px' }}>
          <h4 style={{ margin: '0 0 8px 0', fontSize: '14px', fontWeight: '600', color: '#666' }}>Full Prompt:</h4>
          <pre
            style={{
              backgroundColor: '#f5f5f5',
              padding: '8px',
              borderRadius: '4px',
              fontSize: '12px',
              margin: '0',
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-word',
            }}
          >
            {args.prompt}
          </pre>
        </div>
      )}

      {/* LLM processed content */}
      <div style={{ marginBottom: '12px' }}>
        <h4 style={{ margin: '0 0 8px 0', fontSize: '14px', fontWeight: '600', color: '#666' }}>Processed Content:</h4>
        <MarkdownRenderer content={response.llmContent || ''} />
      </div>

      {/* Return display message */}
      {response.returnDisplay && (
        <div style={{ marginTop: '8px' }}>
          <span
            style={{
              fontSize: '12px',
              color: '#888',
              fontStyle: 'italic',
            }}
          >
            {response.returnDisplay}
          </span>
        </div>
      )}
    </ChatBubble>
  );
};

registerFunctionCallComponent('web_fetch', WebFetchCall);
registerFunctionResponseComponent('web_fetch', WebFetchResponse);
registerFunctionPairComponent('web_fetch', WebFetchPair);
