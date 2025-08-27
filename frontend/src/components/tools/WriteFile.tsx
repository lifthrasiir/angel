import React from 'react';
import { FunctionCall, FunctionResponse } from '../../types/chat';
import { validateExactKeys } from '../../utils/functionMessageValidation';

interface WriteFileCallProps {
  functionCall: FunctionCall;
  messageId?: string;
}

export const WriteFileCall: React.FC<WriteFileCallProps> = ({ functionCall }) => {
  const args = functionCall.args;
  if (!validateExactKeys(args, { file_path: 'string', content: 'string' } as const)) {
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

interface WriteFileResponseProps {
  functionResponse: FunctionResponse;
  messageId?: string;
}

export const WriteFileResponse: React.FC<WriteFileResponseProps> = ({ functionResponse }) => {
  const response = functionResponse.response;
  if (!validateExactKeys(response, { status: 'string', unified_diff: 'string' } as const)) {
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
