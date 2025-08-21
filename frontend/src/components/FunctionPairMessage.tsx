import type React from 'react';
import FunctionCallMessage from './FunctionCallMessage';
import FunctionResponseMessage from './FunctionResponseMessage';
import { FunctionCall, FunctionResponse } from '../types/chat';

interface FunctionPairMessageProps {
  functionCall: FunctionCall;
  functionResponse: FunctionResponse;
  callMessageId?: string;
  responseMessageId?: string;
}

const FunctionPairMessage: React.FC<FunctionPairMessageProps> = ({
  functionCall,
  functionResponse,
  callMessageId,
  responseMessageId,
}) => {
  return (
    <div className="function-pair-container">
      <FunctionCallMessage functionCall={functionCall} messageId={callMessageId} />
      <div className="function-pair-bar"></div>
      <FunctionResponseMessage functionResponse={functionResponse} messageId={responseMessageId} />
    </div>
  );
};

export default FunctionPairMessage;
