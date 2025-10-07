import type React from 'react';
import { useState } from 'react';
import PrettyJSON from './PrettyJSON';
import { FunctionResponse, FileAttachment } from '../types/chat';
import { getFunctionResponseComponent } from '../utils/functionMessageRegistry';
import FileAttachmentList from './FileAttachmentList';
import ChatBubble from './ChatBubble';
import { isImageOnlyMessage } from '../utils/messageUtils';

interface FunctionResponseMessageProps {
  functionResponse: FunctionResponse;
  messageInfo?: React.ReactNode;
  messageId?: string;
  attachments?: FileAttachment[];
  sessionId?: string;
}

const FunctionResponseMessage: React.FC<FunctionResponseMessageProps> = ({
  functionResponse,
  messageInfo,
  messageId,
  attachments,
  sessionId,
}) => {
  const CustomComponent = getFunctionResponseComponent(functionResponse.name);

  const [mode, setMode] = useState<'compact' | 'collapsed' | 'expanded'>('compact');
  const imageOnly = isImageOnlyMessage(functionResponse.response, attachments);

  let responseData = functionResponse.response;
  let responseText: string;

  if (responseData === null || responseData === undefined || responseData === '') {
    responseText = '(empty response)';
  } else if (typeof responseData === 'string') {
    try {
      responseData = JSON.parse(responseData);
      responseText = JSON.stringify(responseData, null, 2);
    } catch (e) {
      console.warn('Function response is not valid JSON string, using raw string:', functionResponse.response);
      responseText = responseData; // Use the raw string if parsing fails
    }
  } else {
    responseText = JSON.stringify(responseData, null, 2);
  }
  const codeContent = responseText;

  let soleObjectKey: string | undefined;
  if (typeof responseData === 'object') {
    const keys = Object.keys(responseData);
    if (keys.length === 1 && keys[0]) soleObjectKey = keys[0];
  }

  const handleHeaderClick = () => {
    setMode((prevMode) => {
      if (prevMode === 'compact') return 'collapsed';
      if (prevMode === 'collapsed') return 'expanded';
      return 'compact';
    });
  };

  const renderContent = () => {
    if (mode === 'compact') {
      return (
        <ChatBubble
          messageId={messageId}
          containerClassName="user-message"
          bubbleClassName="agent-function-response function-message-bubble"
          messageInfo={messageInfo}
          title={codeContent}
          onHeaderClick={handleHeaderClick}
        >
          <FileAttachmentList
            attachments={attachments}
            messageId={messageId}
            sessionId={sessionId}
            isImageOnlyMessage={imageOnly}
          />
        </ChatBubble>
      );
    } else {
      return (
        <ChatBubble
          messageId={messageId}
          containerClassName="user-message"
          bubbleClassName="agent-function-response function-message-bubble"
          messageInfo={messageInfo}
          heighten={false}
          title={<>Function Response: {soleObjectKey && <code>{soleObjectKey}</code>}</>}
          showHeaderToggle={true}
          onHeaderClick={handleHeaderClick}
        >
          {mode === 'collapsed' ? (
            <PrettyJSON data={soleObjectKey ? responseData[soleObjectKey] : responseData} />
          ) : (
            <pre className="function-code-block">{codeContent}</pre>
          )}
          <FileAttachmentList
            attachments={attachments}
            messageId={messageId}
            sessionId={sessionId}
            isImageOnlyMessage={imageOnly}
          />
        </ChatBubble>
      );
    }
  };

  if (CustomComponent) {
    // If a custom component exists for this function response
    return (
      <CustomComponent
        functionResponse={functionResponse}
        messageId={messageId}
        attachments={attachments}
        sessionId={sessionId}
      >
        {renderContent()}
      </CustomComponent>
    );
  } else {
    return <>{renderContent()}</>;
  }
};

export default FunctionResponseMessage;
