import React from 'react';
import { validateExactKeys } from '../../../utils/functionMessageValidation';
import {
  registerFunctionCallComponent,
  registerFunctionResponseComponent,
  registerFunctionPairComponent,
  FunctionCallMessageProps,
  FunctionResponseMessageProps,
  FunctionPairComponentProps,
} from '../../../utils/functionMessageRegistry';
import ChatBubble from '../ChatBubble';
import PrettyDiff from '../../PrettyDiff';
import { getLanguageFromFilename, useHighlightCode } from '../../../utils/highlightUtils';

const argsKeys = { file_path: 'string', content: 'string' } as const;

const WriteFileCall: React.FC<FunctionCallMessageProps> = ({ functionCall, messageId, messageInfo, children }) => {
  const args = functionCall.args;
  if (!validateExactKeys(args, argsKeys)) {
    return children;
  }

  const filePath = args.file_path;
  const language = getLanguageFromFilename(filePath);

  const highlightedContent = useHighlightCode(args.content || '', language);

  return (
    <ChatBubble
      messageId={messageId}
      containerClassName="agent-message"
      bubbleClassName="agent-function-call function-message-bubble"
      messageInfo={messageInfo}
      title={
        <>
          write_file: <code>{args.file_path}</code>
        </>
      }
    >
      <pre>
        <code dangerouslySetInnerHTML={{ __html: highlightedContent }} />
      </pre>
    </ChatBubble>
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
    <ChatBubble
      messageId={messageId}
      containerClassName="user-message"
      bubbleClassName="function-message-bubble"
      messageInfo={messageInfo}
      title="Success"
    >
      {response.unified_diff === 'No changes' ? <p>No changes</p> : <PrettyDiff diffContent={response.unified_diff} />}
    </ChatBubble>
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

  const filePath = args.file_path;
  const language = getLanguageFromFilename(filePath); // Get language from file_path

  return (
    <ChatBubble
      containerClassName="function-pair-combined-container"
      bubbleClassName="function-combined-bubble"
      messageInfo={responseMessageInfo}
      heighten={true}
      title={
        <>
          write_file: <code>{args.file_path}</code>
        </>
      }
      showHeaderToggle={true}
      onHeaderClick={onToggleView}
    >
      {response.unified_diff === 'No changes' ? (
        <p>No changes</p>
      ) : (
        <PrettyDiff diffContent={response.unified_diff} baseLanguage={language} />
      )}
    </ChatBubble>
  );
};

registerFunctionCallComponent('write_file', WriteFileCall);
registerFunctionResponseComponent('write_file', WriteFileResponse);
registerFunctionPairComponent('write_file', WriteFilePair);
