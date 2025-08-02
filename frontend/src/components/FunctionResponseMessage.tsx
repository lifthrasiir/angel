import type React from 'react';
import { useLayoutEffect, useRef, useState } from 'react';
import { FaChevronCircleDown, FaChevronCircleUp } from 'react-icons/fa';
import { measureContentHeight } from '../utils/measurementUtils';
import PrettyJSON from './PrettyJSON';

interface FunctionResponseMessageProps {
  functionResponse: any;
  isUserRole?: boolean; // Optional prop to differentiate user's function response
  messageInfo?: React.ReactNode; // New prop for MessageInfo
}

const FunctionResponseMessage: React.FC<FunctionResponseMessageProps> = ({
  functionResponse,
  isUserRole,
  messageInfo,
}) => {
  const [showPrettyJson, setShowPrettyJson] = useState(true);
  const [isExpanded, setIsExpanded] = useState(false);
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

  useLayoutEffect(() => {
    if (messageRef.current) {
      const contentHeight = measureContentHeight(messageRef, showPrettyJson, codeContent, responseData, soleObjectKey);
      const collapsedHeight = window.innerHeight * 0.3;
      console.log('FunctionResponseMessage: contentHeight', contentHeight, 'collapsedHeight', collapsedHeight);
      setShowToggle(contentHeight > collapsedHeight);
    }
  }, [functionResponse.response, showPrettyJson, codeContent]);

  const togglePrettyJson = () => {
    setIsExpanded(false); // Reset expand state when toggling JSON view
    setShowPrettyJson(!showPrettyJson);
  };

  const toggleExpand = () => {
    setIsExpanded(!isExpanded);
  };

  const containerClassName = `chat-message-container ${isUserRole ? 'user-message' : 'agent-message'}`;
  const bubbleClassName = `chat-bubble function-message-bubble`;

  let soleObjectKey: string | undefined;
  if (typeof responseData === 'object') {
    const keys = Object.keys(responseData);
    if (keys.length === 1 && keys[0]) soleObjectKey = keys[0];
  }

  return (
    <div className={containerClassName}>
      <div className={bubbleClassName}>
        <div
          className="function-title-bar function-response-title-bar"
          style={{ cursor: 'pointer' }}
          onClick={togglePrettyJson}
        >
          Function Response: {showPrettyJson && soleObjectKey && <code>{soleObjectKey}</code>}
        </div>
        <div
          ref={messageRef}
          className={`function-message-content ${isExpanded ? 'expanded' : 'collapsed'}`}
          style={showToggle && !isExpanded ? { maxHeight: '30vh', overflowY: 'auto' } : {}}
        >
          {showPrettyJson ? (
            <PrettyJSON data={soleObjectKey ? responseData[soleObjectKey] : responseData} />
          ) : (
            <pre className="function-code-block">{codeContent}</pre>
          )}
        </div>
        {showToggle && (
          <div className="function-message-toggle-button" onClick={toggleExpand}>
            {isExpanded ? <FaChevronCircleUp /> : <FaChevronCircleDown />}
          </div>
        )}
      </div>
      {messageInfo} {/* Render MessageInfo outside chat-bubble */}
    </div>
  );
};

export default FunctionResponseMessage;
