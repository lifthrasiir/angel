import React from 'react';

interface FunctionCallMessageProps {
  functionCall: any;
}

const FunctionCallMessage: React.FC<FunctionCallMessageProps> = ({ functionCall }) => {
  const codeContent = JSON.stringify(functionCall.args, null, 2);

  return (
    <div className="chat-message-container agent-message">
      <div className="chat-bubble agent-function-call function-message-bubble">
        <div className="function-title-bar function-call-title-bar">
          Function Call: {functionCall.name}
        </div>
        <pre className="function-code-block">
          {codeContent}
        </pre>
      </div>
    </div>
  );
};

export default FunctionCallMessage;
