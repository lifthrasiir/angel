import type React from 'react';
import { useLayoutEffect, useRef, useState } from 'react';
import { FaChevronCircleDown, FaChevronCircleUp } from 'react-icons/fa';
import { measureContentHeight } from '../utils/measurementUtils';
import PrettyJSON from './PrettyJSON';

interface FunctionCallMessageProps {
  functionCall: any;
}

const FunctionCallMessage: React.FC<FunctionCallMessageProps> = ({ functionCall }) => {
  const [showPrettyJson, setShowPrettyJson] = useState(true);
  const [isExpanded, setIsExpanded] = useState(false);
  const [showToggle, setShowToggle] = useState(false);
  const messageRef = useRef<HTMLDivElement>(null);

  const codeContent = JSON.stringify(functionCall.args, null, 2);

  useLayoutEffect(() => {
    if (messageRef.current) {
      const contentHeight = measureContentHeight(messageRef, showPrettyJson, codeContent, functionCall.args);
      const collapsedHeight = window.innerHeight * 0.3;
      console.log('FunctionCallMessage: contentHeight', contentHeight, 'collapsedHeight', collapsedHeight);
      setShowToggle(contentHeight > collapsedHeight);
    }
  }, [functionCall.args, showPrettyJson, codeContent]);

  const togglePrettyJson = () => {
    setShowPrettyJson(!showPrettyJson);
  };

  const toggleExpand = () => {
    setIsExpanded(!isExpanded);
  };

  return (
    <div className="chat-message-container agent-message">
      <div className="chat-bubble agent-function-call function-message-bubble">
        <div
          className="function-title-bar function-call-title-bar"
          style={{ cursor: 'pointer' }}
          onClick={togglePrettyJson}
        >
          Function Call: {functionCall.name}
        </div>
        <div
          ref={messageRef}
          className={`function-message-content ${isExpanded ? 'expanded' : 'collapsed'}`}
          style={showToggle && !isExpanded ? { maxHeight: '30vh', overflowY: 'auto' } : {}}
        >
          {showPrettyJson ? (
            <PrettyJSON data={functionCall.args} />
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
    </div>
  );
};

export default FunctionCallMessage;
