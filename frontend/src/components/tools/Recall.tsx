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

// Define the expected arguments for the recall tool call
const argsKeys = {
  query: 'string',
} as const;

// Define the expected response for the recall tool
const responseKeys = {
  response: 'string',
} as const;

const RecallCall: React.FC<FunctionCallMessageProps> = ({ functionCall, messageId, messageInfo, children }) => {
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
      title="Recalling..."
      heighten={false}
      collapsed={true}
    >
      <p>{args.query}</p>
    </ChatBubble>
  );
};

const RecallResponse: React.FC<FunctionResponseMessageProps> = ({
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

  return (
    <ChatBubble
      messageId={messageId}
      containerClassName="user-message"
      bubbleClassName="function-message-bubble"
      messageInfo={messageInfo}
      title="Recalled"
      heighten={false}
      collapsed={true}
    >
      <p>{response.response}</p>
      {attachments && attachments.length > 0 && <FileAttachmentList attachments={attachments} />}
    </ChatBubble>
  );
};

const RecallPair: React.FC<FunctionPairComponentProps> = ({
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

  // Determine if we have a response (recalled content)
  const hasResponse = response.response && response.response.trim() !== '';
  const title = hasResponse ? 'Recalled' : 'Recalling...';

  return (
    <ChatBubble
      containerClassName="function-pair-combined-container"
      bubbleClassName="function-combined-bubble"
      messageInfo={responseMessageInfo}
      title={title}
      onHeaderClick={onToggleView}
      heighten={false}
      collapsed={true}
      showHeaderToggle={true}
    >
      <p>{hasResponse ? response.response : args.query}</p>
      {attachments && attachments.length > 0 && <FileAttachmentList attachments={attachments} />}
    </ChatBubble>
  );
};

registerFunctionCallComponent('recall', RecallCall);
registerFunctionResponseComponent('recall', RecallResponse);
registerFunctionPairComponent('recall', RecallPair);
