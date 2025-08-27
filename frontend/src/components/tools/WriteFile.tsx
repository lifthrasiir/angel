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

const WriteFileCall: React.FC<FunctionCallMessageProps> = ({ functionCall }) => {
  const args = functionCall.args;
  if (!validateExactKeys(args, argsKeys)) {
    return null;
  }

  return (
    <>
      <div className="function-title-bar function-call-title-bar">
        write_file: <code>{args.file_path}</code>
      </div>
      <pre>{args.content}</pre>
    </>
  );
};

const responseKeys = { status: 'string', unified_diff: 'string' } as const;

const WriteFileResponse: React.FC<FunctionResponseMessageProps> = ({ functionResponse }) => {
  const response = functionResponse.response;
  if (!validateExactKeys(response, responseKeys)) {
    return null;
  }
  if (response.status !== 'success') {
    return null;
  }

  return (
    <>
      <div className="function-title-bar function-response-title-bar">Success</div>
      {response.unified_diff === 'No changes' ? <p>No changes</p> : <pre>{response.unified_diff}</pre>}
    </>
  );
};

const WriteFilePair: React.FC<FunctionPairComponentProps> = ({ functionCall, functionResponse, onToggleView }) => {
  const args = functionCall.args;
  const response = functionResponse.response;

  if (!validateExactKeys(args, argsKeys) || !validateExactKeys(response, responseKeys)) {
    return null;
  }
  if (response.status != 'success') return null;

  return (
    <div className="function-pair-combined-container">
      <div className="chat-bubble">
        <div className="function-title-bar function-combined-title-bar" onClick={onToggleView}>
          write_file: <code>{args.file_path}</code>
        </div>
        {response.unified_diff === 'No changes' ? <p>No changes</p> : <pre>{response.unified_diff}</pre>}
      </div>
    </div>
  );
};

registerFunctionCallComponent('write_file', WriteFileCall);
registerFunctionResponseComponent('write_file', WriteFileResponse);
registerFunctionPairComponent('write_file', WriteFilePair);
