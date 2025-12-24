import type React from 'react';
import { useState } from 'react';
import PrettyJSON from '../PrettyJSON';
import { FunctionCall } from '../../types/chat';
import { getFunctionCallComponent } from '../../utils/functionMessageRegistry';
import ChatBubble from './ChatBubble';

interface FunctionCallMessageProps {
  functionCall: FunctionCall;
  messageInfo?: React.ReactNode;
  messageId?: string;
}

const FunctionCallMessage: React.FC<FunctionCallMessageProps> = ({ functionCall, messageInfo, messageId }) => {
  const CustomComponent = getFunctionCallComponent(functionCall.name);

  const [mode, setMode] = useState<'compact' | 'collapsed' | 'expanded'>('compact');

  const codeContent = JSON.stringify(functionCall.args, null, 2);
  const callArgs = JSON.stringify(functionCall.args);

  const handleHeaderClick = () => {
    setMode((prevMode) => {
      if (prevMode === 'compact') return 'collapsed';
      if (prevMode === 'collapsed') return 'expanded';
      return 'compact';
    });
  };

  const renderContent = () => {
    if (mode === 'compact') {
      return (
        <ChatBubble
          messageId={messageId}
          containerClassName="agent-message"
          bubbleClassName="agent-function-call function-message-bubble"
          messageInfo={messageInfo}
          title={`${functionCall.name}(${callArgs})`}
          onHeaderClick={handleHeaderClick}
        />
      );
    } else {
      return (
        <ChatBubble
          messageId={messageId}
          containerClassName="agent-message"
          bubbleClassName="agent-function-call function-message-bubble"
          messageInfo={messageInfo}
          heighten={false}
          title={`Function Call: ${functionCall.name}`}
          showHeaderToggle={true}
          onHeaderClick={handleHeaderClick}
        >
          {mode === 'collapsed' ? (
            <PrettyJSON data={functionCall.args} />
          ) : (
            <pre className="function-code-block">{codeContent}</pre>
          )}
        </ChatBubble>
      );
    }
  };

  if (CustomComponent) {
    // If a custom component exists for this function call
    return (
      <CustomComponent functionCall={functionCall} messageId={messageId} messageInfo={messageInfo}>
        {renderContent()}
      </CustomComponent>
    );
  } else {
    return <>{renderContent()}</>;
  }
};

export default FunctionCallMessage;
