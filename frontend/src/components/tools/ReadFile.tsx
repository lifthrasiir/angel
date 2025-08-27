import React from 'react';
import { FunctionCall, FunctionResponse } from '../../types/chat';
import { validateExactKeys } from '../../utils/functionMessageValidation';

interface ReadFileCallProps {
  functionCall: FunctionCall;
  messageId?: string;
}

export const ReadFileCall: React.FC<ReadFileCallProps> = ({ functionCall }) => {
  const args = functionCall.args;
  if (!validateExactKeys(args, { file_path: 'string' } as const)) {
    return null;
  }

  return (
    <div className="function-title-bar function-call-title-bar">
      read_file: <code>{args.file_path}</code>
    </div>
  );
};

interface ReadFileResponseProps {
  functionResponse: FunctionResponse;
  messageId?: string;
}

export const ReadFileResponse: React.FC<ReadFileResponseProps> = ({ functionResponse }) => {
  const response = functionResponse.response;
  if (!validateExactKeys(response, { content: 'string' } as const)) {
    return null;
  }

  return (
    <>
      <div className="function-title-bar function-response-title-bar">File contents</div>
      <pre>{response.content}</pre>
    </>
  );
};
