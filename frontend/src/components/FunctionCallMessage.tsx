import type React from 'react';
import { useLayoutEffect, useRef, useState } from 'react';
import { FaChevronCircleDown, FaChevronCircleUp } from 'react-icons/fa';
import { measureContentHeight } from '../utils/measurementUtils';
import PrettyJSON from './PrettyJSON';
import { FunctionCall } from '../types/chat';
import { getFunctionCallComponent } from '../utils/functionMessageRegistry';

interface FunctionCallMessageProps {
  functionCall: FunctionCall;
  messageInfo?: React.ReactNode;
  messageId?: string;
}

const FunctionCallMessage: React.FC<FunctionCallMessageProps> = ({ functionCall, messageInfo, messageId }) => {
  const CustomComponent = getFunctionCallComponent(functionCall.name);

  if (CustomComponent) {
    // If a custom component exists for this function call
    return (
      <div id={messageId} className="chat-message-container agent-message">
        <div className="chat-bubble agent-function-call function-message-bubble">
          <CustomComponent functionCall={functionCall} messageId={messageId} />
        </div>
        {messageInfo}
      </div>
    );
  }

  const [mode, setMode] = useState<'compact' | 'collapsed' | 'expanded'>('compact');
  const [showToggle, setShowToggle] = useState(false);
  const messageRef = useRef<HTMLDivElement>(null);

  const codeContent = JSON.stringify(functionCall.args, null, 2);
  const callArgs = JSON.stringify(functionCall.args);

  useLayoutEffect(() => {
    if (messageRef.current && mode === 'expanded') {
      const contentHeight = measureContentHeight(messageRef, false, codeContent, functionCall.args);
      const collapsedHeight = window.innerHeight * 0.3;
      setShowToggle(contentHeight > collapsedHeight);
    }
  }, [functionCall.args, codeContent, mode]);

  const toggleMode = () => {
    setMode((prevMode) => {
      if (prevMode === 'compact') return 'collapsed';
      if (prevMode === 'collapsed') return 'expanded';
      return 'compact';
    });
  };

  const renderContent = () => {
    switch (mode) {
      case 'compact':
        return (
          <div id={messageId} className="chat-message-container agent-message">
            <div
              className="chat-bubble agent-function-call function-message-bubble"
              style={{ cursor: 'pointer' }}
              onClick={toggleMode}
            >
              <div className="function-title-bar function-call-title-bar">
                {functionCall.name}({callArgs})
              </div>
            </div>
            {messageInfo}
          </div>
        );
      case 'collapsed':
        return (
          <div id={messageId} className="chat-message-container agent-message">
            <div
              className="chat-bubble agent-function-call function-message-bubble"
              style={{ cursor: 'pointer' }}
              onClick={toggleMode}
            >
              <div className="function-title-bar function-call-title-bar">Function Call: {functionCall.name}</div>
              <div ref={messageRef} className="function-message-content">
                <PrettyJSON data={functionCall.args} />
              </div>
            </div>
            {messageInfo}
          </div>
        );
      case 'expanded':
        return (
          <div id={messageId} className="chat-message-container agent-message">
            <div className="chat-bubble agent-function-call function-message-bubble">
              <div
                className="function-title-bar function-call-title-bar"
                style={{ cursor: 'pointer' }}
                onClick={toggleMode}
              >
                Function Call: {functionCall.name}
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
            </div>
            {messageInfo}
          </div>
        );
      default:
        return null;
    }
  };

  return <>{renderContent()}</>;
};

export default FunctionCallMessage;
