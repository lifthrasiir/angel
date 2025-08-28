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

const argsKeys = { file_path: 'string', content: 'string' } as const;

const WriteFileCall: React.FC<FunctionCallMessageProps> = ({ functionCall, messageId, messageInfo, children }) => {
  const args = functionCall.args;
  if (!validateExactKeys(args, argsKeys)) {
    return children;
  }

  return (
    <div id={messageId} className="chat-message-container agent-message">
      <div className="chat-bubble agent-function-call function-message-bubble">
        <div className="function-title-bar function-call-title-bar">
          write_file: <code>{args.file_path}</code>
        </div>
        <pre>{args.content}</pre>
      </div>
      {messageInfo}
    </div>
  );
};

const responseKeys = { status: 'string', unified_diff: 'string' } as const;

const WriteFileResponse: React.FC<FunctionResponseMessageProps> = ({
  functionResponse,
  messageId,
  messageInfo,
  children,
}) => {
  const response = functionResponse.response;
  if (!validateExactKeys(response, responseKeys)) {
    return children;
  }
  if (response.status !== 'success') {
    return children;
  }

  return (
    <div id={messageId} className="chat-message-container user-message">
      <div className="chat-bubble function-message-bubble">
        <div className="function-title-bar function-response-title-bar">Success</div>
        {response.unified_diff === 'No changes' ? <p>No changes</p> : <pre>{response.unified_diff}</pre>}
      </div>
      {messageInfo}
    </div>
  );
};

const WriteFilePair: React.FC<FunctionPairComponentProps> = ({
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
  if (response.status != 'success') {
    return children;
  }

  return (
    <div className="function-pair-combined-container">
      <div className="chat-bubble">
        <div className="function-title-bar function-combined-title-bar" onClick={onToggleView}>
          write_file: <code>{args.file_path}</code>
        </div>
        {response.unified_diff === 'No changes' ? <p>No changes</p> : <pre>{response.unified_diff}</pre>}
      </div>
      {responseMessageInfo}
    </div>
  );
};

registerFunctionCallComponent('write_file', WriteFileCall);
registerFunctionResponseComponent('write_file', WriteFileResponse);
registerFunctionPairComponent('write_file', WriteFilePair);
