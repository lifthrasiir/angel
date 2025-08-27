import React, { useState } from 'react';
import FunctionCallMessage from './FunctionCallMessage';
import FunctionResponseMessage from './FunctionResponseMessage';
import { FunctionCall, FunctionResponse } from '../types/chat';
import { getFunctionPairComponent } from '../utils/functionMessageRegistry';

interface FunctionPairMessageProps {
  functionCall: FunctionCall;
  functionResponse: FunctionResponse;
  callMessageId?: string;
  responseMessageId?: string;
  callMessageInfo?: React.ReactNode;
  responseMessageInfo?: React.ReactNode;
}

const FunctionPairMessage: React.FC<FunctionPairMessageProps> = ({
  functionCall,
  functionResponse,
  callMessageId,
  responseMessageId,
  callMessageInfo,
  responseMessageInfo,
}) => {
  const CombinedComponent = getFunctionPairComponent(functionCall.name);
  const [showCombinedView, setShowCombinedView] = useState(!!CombinedComponent);

  const handleToggleView = () => {
    setShowCombinedView(!showCombinedView);
  };

  if (CombinedComponent && showCombinedView) {
    return (
      <CombinedComponent
        functionCall={functionCall}
        functionResponse={functionResponse}
        callMessageId={callMessageId}
        responseMessageId={responseMessageId}
        onToggleView={handleToggleView}
      />
    );
  }

  return (
    <div className="function-pair-container">
      <FunctionCallMessage functionCall={functionCall} messageId={callMessageId} messageInfo={callMessageInfo} />
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
      />
    </div>
  );
};

export default FunctionPairMessage;
