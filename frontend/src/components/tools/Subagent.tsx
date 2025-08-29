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
import UserTextMessage from '../UserTextMessage';
import ModelTextMessage from '../ModelTextMessage';
import MarkdownRenderer from '../MarkdownRenderer';

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
    <div id={messageId} className="chat-message-container agent-message">
      <div className="chat-bubble agent-function-call function-message-bubble">
        <div className="function-title-bar function-call-title-bar">
          Subagent
          {args.subagent_id && (
            <>
              : <code>{args.subagent_id}</code>
            </>
          )}
        </div>
        {args.system_prompt && <SystemMessage text={args.system_prompt} messageId={`${messageId}.system`} />}
        <p>{args.text}</p>
      </div>
      {messageInfo}
    </div>
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
    <div id={messageId} className="chat-message-container user-message">
      <div className="chat-bubble function-message-bubble">
        <div className="function-title-bar function-response-title-bar">
          Subagent: {response.subagent_id && <code>{response.subagent_id}</code>} Response
        </div>
        <MarkdownRenderer content={response.response_text} />
      </div>
      {messageInfo}
    </div>
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
    <div className="function-pair-combined-container">
      <div className="chat-bubble">
        <div className="function-title-bar function-combined-title-bar" onClick={onToggleView}>
          Subagent: <code>{args.subagent_id || response.subagent_id}</code>
        </div>
        {args.system_prompt && <SystemMessage text={args.system_prompt} messageId={`${responseMessageId}.system`} />}
        <UserTextMessage text={args.text} messageId={`${responseMessageId}.user`} />
        <ModelTextMessage
          className="agent-message"
          text={response.response_text}
          messageId={`${responseMessageId}.model`}
        />
      </div>
      {responseMessageInfo}
    </div>
  );
};

registerFunctionCallComponent('subagent', SubagentCall);
registerFunctionResponseComponent('subagent', SubagentResponse);
registerFunctionPairComponent('subagent', SubagentPair);

export {};
