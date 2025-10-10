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
import FileAttachmentList from '../FileAttachmentList';
import ChatBubble from '../ChatBubble';
import { getLanguageFromFilename, useHighlightCode } from '../../utils/highlightUtils';

const argsKeys = { file_path: 'string' } as const;

const ReadFileCall: React.FC<FunctionCallMessageProps> = ({ functionCall, messageId, messageInfo, children }) => {
  const args = functionCall.args;
  if (!validateExactKeys(args, argsKeys)) {
    return children;
  }

  return (
    <ChatBubble
      messageId={messageId}
      containerClassName="agent-message"
      bubbleClassName="agent-function-call function-message-bubble"
      messageInfo={messageInfo}
      title={
        <>
          read_file: <code>{args.file_path}</code>
        </>
      }
    />
  );
};

const responseKeys = { content: 'string', note: 'string?' } as const;

const ReadFileResponse: React.FC<FunctionResponseMessageProps> = ({
  functionResponse,
  messageId,
  attachments,
  messageInfo,
  children,
}) => {
  const response = functionResponse.response;
  if (!validateExactKeys(response, responseKeys)) {
    return children;
  }

  const highlightedContent = useHighlightCode(response.content || '');

  return (
    <ChatBubble
      messageId={messageId}
      containerClassName="user-message"
      bubbleClassName="function-message-bubble"
      messageInfo={messageInfo}
    >
      <pre className="function-code-block">
        <code dangerouslySetInnerHTML={{ __html: highlightedContent }} />
      </pre>
      {response.note && <p>{response.note}</p>}
      {attachments && attachments.length > 0 && <FileAttachmentList attachments={attachments} />}
    </ChatBubble>
  );
};

const ReadFilePair: React.FC<FunctionPairComponentProps> = ({
  functionCall,
  functionResponse,
  onToggleView,
  attachments,
  responseMessageInfo,
  children,
}) => {
  const args = functionCall.args;
  const response = functionResponse.response;

  if (!validateExactKeys(args, argsKeys) || !validateExactKeys(response, responseKeys)) {
    return children;
  }

  const filePath = args.file_path;
  const language = getLanguageFromFilename(filePath);

  const highlightedContent = useHighlightCode(response.content || '', language);

  return (
    <ChatBubble
      containerClassName="function-pair-combined-container"
      bubbleClassName="function-combined-bubble"
      messageInfo={responseMessageInfo}
      heighten={false}
      collapsed={true}
      title={
        <>
          read_file: <code>{args.file_path}</code>
        </>
      }
      showHeaderToggle={true}
      onHeaderClick={onToggleView}
    >
      <pre className="function-code-block">
        <code dangerouslySetInnerHTML={{ __html: highlightedContent }} />
      </pre>
      {response.note && <p>{response.note}</p>}
      {attachments && attachments.length > 0 && <FileAttachmentList attachments={attachments} />}
    </ChatBubble>
  );
};

registerFunctionCallComponent('read_file', ReadFileCall);
registerFunctionResponseComponent('read_file', ReadFileResponse);
registerFunctionPairComponent('read_file', ReadFilePair);
