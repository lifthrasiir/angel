import type React from 'react';
import { useLayoutEffect, useRef, useState } from 'react';
import { FaChevronCircleDown, FaChevronCircleUp } from 'react-icons/fa';
import { measureContentHeight } from '../utils/measurementUtils';
import PrettyJSON from './PrettyJSON';
import { FunctionResponse, FileAttachment } from '../types/chat';
import { getFunctionResponseComponent } from '../utils/functionMessageRegistry';
import FileAttachmentList from './FileAttachmentList';

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

  if (CustomComponent) {
    // If a custom component exists for this function response
    return (
      <div id={messageId} className="chat-message-container user-message">
        <div className="chat-bubble function-message-bubble">
          <CustomComponent
            functionResponse={functionResponse}
            messageId={messageId}
            attachments={attachments}
            sessionId={sessionId}
          />
        </div>
        {messageInfo}
      </div>
    );
  }

  const [mode, setMode] = useState<'compact' | 'collapsed' | 'expanded'>('compact');
  const [showToggle, setShowToggle] = useState(false);
  const messageRef = useRef<HTMLDivElement>(null);

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

  useLayoutEffect(() => {
    if (messageRef.current && mode === 'expanded') {
      const contentHeight = measureContentHeight(messageRef, false, codeContent, responseData, soleObjectKey);
      const collapsedHeight = window.innerHeight * 0.3;
      setShowToggle(contentHeight > collapsedHeight);
    }
  }, [functionResponse.response, codeContent, mode, soleObjectKey]);

  const toggleMode = () => {
    setMode((prevMode) => {
      if (prevMode === 'compact') return 'collapsed';
      if (prevMode === 'collapsed') return 'expanded';
      return 'compact';
    });
  };

  const renderContent = () => {
    return (
      <div id={messageId} className="chat-message-container user-message">
        <div className="chat-bubble function-message-bubble">
          {/* Original function response rendering */}
          {mode === 'compact' && (
            <div
              className="function-title-bar function-response-title-bar"
              style={{ cursor: 'pointer' }}
              onClick={toggleMode}
            >
              {codeContent}
            </div>
          )}
          {mode === 'collapsed' && (
            <>
              <div
                className="function-title-bar function-response-title-bar"
                style={{ cursor: 'pointer' }}
                onClick={toggleMode}
              >
                Function Response: {soleObjectKey && <code>{soleObjectKey}</code>}
              </div>
              <div ref={messageRef} className="function-message-content">
                <PrettyJSON data={soleObjectKey ? responseData[soleObjectKey] : responseData} />
              </div>
            </>
          )}
          {mode === 'expanded' && (
            <>
              <div
                className="function-title-bar function-response-title-bar"
                style={{ cursor: 'pointer' }}
                onClick={toggleMode}
              >
                Function Response: {soleObjectKey && <code>{soleObjectKey}</code>}
              </div>
              <div
                ref={messageRef}
                className="function-message-content"
                style={showToggle ? { maxHeight: '30vh', overflowY: 'auto' } : {}}
              >
                <pre className="function-code-block">{codeContent}</pre>
              </div>
              {showToggle && (
                <div className="function-message-toggle-button" onClick={toggleMode}>
                  {mode === 'expanded' ? <FaChevronCircleUp /> : <FaChevronCircleDown />}
                </div>
              )}
            </>
          )}

          <FileAttachmentList attachments={attachments} messageId={messageId} sessionId={sessionId} />
        </div>
        {messageInfo}
      </div>
    );
  };

  return <>{renderContent()}</>;
};

export default FunctionResponseMessage;
