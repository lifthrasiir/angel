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
import SystemMessage from '../SystemMessage';
import MarkdownRenderer from '../MarkdownRenderer';
import ChatBubble from '../ChatBubble';

// Define the expected arguments for the subagent tool call
const argsKeys = {
  subagent_id: 'string?',
  system_prompt: 'string?',
  text: 'string',
} as const;

const SubagentCall: React.FC<FunctionCallMessageProps> = ({ functionCall, messageId, messageInfo, children }) => {
  const args = functionCall.args;
  if (!validateExactKeys(args, argsKeys)) {
    return children;
  }
  if (!args.subagent_id === !args.system_prompt) {
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
          Subagent
          {args.subagent_id && (
            <>
              : <code>{args.subagent_id}</code>
            </>
          )}
        </>
      }
      heighten={false}
    >
      {args.system_prompt && <SystemMessage text={args.system_prompt} messageId={`${messageId}.system`} />}
      <p>{args.text}</p>
    </ChatBubble>
  );
};

// Define the expected response for the subagent tool
const responseKeys = {
  subagent_id: 'string?',
  response_text: 'string',
} as const;

const SubagentResponse: React.FC<FunctionResponseMessageProps> = ({
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
      title={<>Subagent: {response.subagent_id && <code>{response.subagent_id}</code>} Response</>}
      heighten={false}
    >
      <MarkdownRenderer content={response.response_text} />
    </ChatBubble>
  );
};

const SubagentPair: React.FC<FunctionPairComponentProps> = ({
  functionCall,
  functionResponse,
  onToggleView,
  responseMessageInfo,
  children,
  responseMessageId,
}) => {
  const args = functionCall.args;
  const response = functionResponse.response;

  if (!validateExactKeys(args, argsKeys) || !validateExactKeys(response, responseKeys)) {
    return children;
  }
  if (!args.subagent_id === !args.system_prompt) {
    return children;
  }

  return (
    <ChatBubble
      containerClassName="function-pair-combined-container"
      bubbleClassName="function-combined-bubble"
      messageInfo={responseMessageInfo}
      title={
        <>
          Subagent: <code>{args.subagent_id || response.subagent_id}</code>
        </>
      }
      onHeaderClick={onToggleView}
    >
      {args.system_prompt && (
        <ChatBubble messageId={`${responseMessageId}.system`} containerClassName="system-message">
          <MarkdownRenderer content={args.system_prompt} />
        </ChatBubble>
      )}
      <ChatBubble
        messageId={`${responseMessageId}.user`}
        containerClassName="user-message"
        bubbleClassName="user-message-bubble-content"
        heighten={false}
      >
        {args.text}
      </ChatBubble>
      <ChatBubble messageId={`${responseMessageId}.model`} containerClassName="agent-message" heighten={false}>
        <MarkdownRenderer content={response.response_text} />
      </ChatBubble>
    </ChatBubble>
  );
};

registerFunctionCallComponent('subagent', SubagentCall);
registerFunctionResponseComponent('subagent', SubagentResponse);
registerFunctionPairComponent('subagent', SubagentPair);
