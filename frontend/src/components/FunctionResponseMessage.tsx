import React from 'react';

interface FunctionResponseMessageProps {
  functionResponse: any;
  isUserRole?: boolean; // Optional prop to differentiate user's function response
}

const FunctionResponseMessage: React.FC<FunctionResponseMessageProps> = ({ functionResponse, isUserRole }) => {
  let responseData = functionResponse.response;
  let responseText: string;

  if (responseData === null || responseData === undefined || responseData === "") {
    responseText = "(empty response)";
  } else if (typeof responseData === 'string') {
    try {
      responseData = JSON.parse(responseData);
      responseText = JSON.stringify(responseData, null, 2);
    } catch (e) {
      console.warn("Function response is not valid JSON string, using raw string:", functionResponse.response);
      responseText = responseData; // Use the raw string if parsing fails
    }
  } else {
    responseText = JSON.stringify(responseData, null, 2);
  }
  const codeContent = responseText;

  const containerClassName = `chat-message-container ${isUserRole ? "user-message" : "agent-message"}`;
  const bubbleClassName = `chat-bubble function-message-bubble`;

  return (
    <div className={containerClassName}>
      <div className={bubbleClassName}>
        <div className="function-title-bar function-response-title-bar">
          Function Response:
        </div>
        <pre className="function-code-block">
          {codeContent}
        </pre>
      </div>
    </div>
  );
};

export default FunctionResponseMessage;
