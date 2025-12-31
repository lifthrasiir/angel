import React, { useState } from 'react';
import FunctionCallMessage from './FunctionCallMessage';
import FunctionResponseMessage from './FunctionResponseMessage';
import { FileAttachment, FunctionCall, FunctionResponse } from '../../types/chat';
import { getFunctionPairComponent } from '../../utils/functionMessageRegistry';

interface FunctionPairMessageProps {
  functionCall: FunctionCall;
  functionResponse: FunctionResponse;
  callMessageId: string;
  responseMessageId: string;
  callMessageInfo?: React.ReactNode;
  responseMessageInfo?: React.ReactNode;
  responseAttachments?: FileAttachment[];
  sessionId?: string;
}

const FunctionPairMessage: React.FC<FunctionPairMessageProps> = ({
  functionCall,
  functionResponse,
  callMessageId,
  responseMessageId,
  callMessageInfo,
  responseMessageInfo,
  responseAttachments,
  sessionId,
}) => {
  const CombinedComponent = getFunctionPairComponent(functionCall.name);
  const [showCombinedView, setShowCombinedView] = useState(!!CombinedComponent);

  const handleToggleView = () => {
    setShowCombinedView(!showCombinedView);
  };

  const content = (
    <div className="function-pair-container">
      <FunctionCallMessage
        functionCall={functionCall}
        messageId={callMessageId}
        sessionId={sessionId}
        messageInfo={callMessageInfo}
      />
      <div
        className="function-pair-bar"
        onClick={handleToggleView}
        style={{
          cursor: CombinedComponent ? 'pointer' : 'default',
        }}
      ></div>
      <FunctionResponseMessage
        functionResponse={functionResponse}
        messageId={responseMessageId}
        messageInfo={responseMessageInfo}
        attachments={responseAttachments}
        sessionId={sessionId}
      />
    </div>
  );

  if (CombinedComponent && showCombinedView) {
    return (
      <CombinedComponent
        functionCall={functionCall}
        functionResponse={functionResponse}
        callMessageId={callMessageId}
        responseMessageId={responseMessageId}
        attachments={responseAttachments}
        sessionId={sessionId}
        onToggleView={handleToggleView}
        callMessageInfo={callMessageInfo}
        responseMessageInfo={responseMessageInfo}
      >
        {content}
      </CombinedComponent>
    );
  } else {
    return content;
  }
};

export default FunctionPairMessage;
