import React, { useState } from 'react';
import { validateExactKeys } from '../../utils/functionMessageValidation';
import {
  registerFunctionCallComponent,
  registerFunctionResponseComponent,
  registerFunctionPairComponent,
  FunctionCallMessageProps,
  FunctionResponseMessageProps,
  FunctionPairComponentProps,
} from '../../utils/functionMessageRegistry';
import { FaChevronDown, FaChevronUp } from 'react-icons/fa';
import FileAttachmentList from '../FileAttachmentList';

const argsKeys = { file_path: 'string' } as const;

const ReadFileCall: React.FC<FunctionCallMessageProps> = ({ functionCall, messageId, messageInfo, children }) => {
  const args = functionCall.args;
  if (!validateExactKeys(args, argsKeys)) {
    return children;
  }

  return (
    <div id={messageId} className="chat-message-container agent-message">
      <div className="chat-bubble agent-function-call function-message-bubble">
        <div className="function-title-bar function-call-title-bar">
          read_file: <code>{args.file_path}</code>
        </div>
      </div>
      {messageInfo}
    </div>
  );
};

const responseKeys = { content: 'string', note: 'string?' } as const;

const ReadFileResponse: React.FC<FunctionResponseMessageProps> = ({
  functionResponse,
  messageId,
  attachments,
  sessionId,
  messageInfo,
  children,
}) => {
  const response = functionResponse.response;
  if (!validateExactKeys(response, responseKeys)) {
    return children;
  }

  return (
    <div id={messageId} className="chat-message-container user-message">
      <div className="chat-bubble function-message-bubble">
        <div className="read-file-response">
          <pre className="function-code-block">{response.content}</pre>
          {response.note && <p>{response.note}</p>}
          {attachments && attachments.length > 0 && (
            <FileAttachmentList attachments={attachments} messageId={messageId} sessionId={sessionId} />
          )}
        </div>
      </div>
      {messageInfo}
    </div>
  );
};

const ReadFilePair: React.FC<FunctionPairComponentProps> = ({
  functionCall,
  functionResponse,
  onToggleView,
  attachments,
  sessionId,
  responseMessageId,
  responseMessageInfo,
  children,
}) => {
  const args = functionCall.args;
  const response = functionResponse.response;
  const [isExpanded, setIsExpanded] = useState(false);

  const handleToggleExpand = (event: React.MouseEvent) => {
    event.stopPropagation();
    setIsExpanded(!isExpanded);
  };

  if (!validateExactKeys(args, argsKeys) || !validateExactKeys(response, responseKeys)) {
    return children;
  }

  return (
    <div className="function-pair-combined-container">
      <div className="chat-bubble">
        <div
          className="function-title-bar function-combined-title-bar"
          onClick={onToggleView}
          style={{ display: 'flex', alignItems: 'center' }}
        >
          <div style={{ flexGrow: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            read_file: <code>{args.file_path}</code>
          </div>
          <span
            style={{
              color: 'var(--color-combined-verydark)',
              cursor: 'pointer',
              height: '1em',
              marginLeft: '10px',
            }}
            onClick={handleToggleExpand}
          >
            {isExpanded ? <FaChevronUp /> : <FaChevronDown />}
          </span>
        </div>
        {isExpanded && (
          <div className="function-pair-expanded-content">
            <pre className="function-code-block">{response.content}</pre>
            {response.note && <p>{response.note}</p>}
            {attachments && attachments.length > 0 && (
              <FileAttachmentList attachments={attachments} messageId={responseMessageId} sessionId={sessionId} />
            )}
          </div>
        )}
      </div>
      {responseMessageInfo}
    </div>
  );
};

registerFunctionCallComponent('read_file', ReadFileCall);
registerFunctionResponseComponent('read_file', ReadFileResponse);
registerFunctionPairComponent('read_file', ReadFilePair);
