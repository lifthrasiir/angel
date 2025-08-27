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

const argsKeys = { file_path: 'string' } as const;

const ReadFileCall: React.FC<FunctionCallMessageProps> = ({ functionCall }) => {
  const args = functionCall.args;
  if (!validateExactKeys(args, argsKeys)) {
    return null;
  }

  return (
    <div className="function-title-bar function-call-title-bar">
      read_file: <code>{args.file_path}</code>
    </div>
  );
};

const responseKeys = { content: 'string' } as const;

const ReadFileResponse: React.FC<FunctionResponseMessageProps> = ({ functionResponse }) => {
  const response = functionResponse.response;
  if (!validateExactKeys(response, responseKeys)) {
    return null;
  }

  return (
    <>
      <div className="function-title-bar function-response-title-bar">File contents</div>
      <pre>{response.content}</pre>
    </>
  );
};

const ReadFilePair: React.FC<FunctionPairComponentProps> = ({ functionCall, functionResponse, onToggleView }) => {
  const args = functionCall.args;
  const response = functionResponse.response;
  const [isExpanded, setIsExpanded] = useState(false);

  const handleToggleExpand = (event: React.MouseEvent) => {
    event.stopPropagation();
    setIsExpanded(!isExpanded);
  };

  if (!validateExactKeys(args, argsKeys) || !validateExactKeys(response, responseKeys)) {
    return null;
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
        {isExpanded && <pre>{response.content}</pre>}
      </div>
    </div>
  );
};

registerFunctionCallComponent('read_file', ReadFileCall);
registerFunctionResponseComponent('read_file', ReadFileResponse);
registerFunctionPairComponent('read_file', ReadFilePair);
