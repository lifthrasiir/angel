import type React from 'react';
import FunctionCallMessage from './FunctionCallMessage';
import FunctionResponseMessage from './FunctionResponseMessage';
import { FunctionCall, FunctionResponse } from '../types/chat';

interface FunctionPairMessageProps {
  functionCall: FunctionCall;
  functionResponse: FunctionResponse;
}

const FunctionPairMessage: React.FC<FunctionPairMessageProps> = ({ functionCall, functionResponse }) => {
  return (
    <div className="function-pair-container">
      <FunctionCallMessage functionCall={functionCall} />
      <div className="function-pair-bar"></div>
      <FunctionResponseMessage functionResponse={functionResponse} />
    </div>
  );
};

export default FunctionPairMessage;
