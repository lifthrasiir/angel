import type React from 'react';
import FunctionCallMessage from './FunctionCallMessage';
import FunctionResponseMessage from './FunctionResponseMessage';
import { FunctionCall, FunctionResponse } from '../types/chat';

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
  return (
    <div className="function-pair-container">
      <FunctionCallMessage functionCall={functionCall} messageId={callMessageId} messageInfo={callMessageInfo} />
      <div className="function-pair-bar"></div>
      <FunctionResponseMessage
        functionResponse={functionResponse}
        messageId={responseMessageId}
        messageInfo={responseMessageInfo}
      />
    </div>
  );
};

export default FunctionPairMessage;
