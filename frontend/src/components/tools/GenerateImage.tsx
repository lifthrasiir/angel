import React from 'react';
import { validateExactKeys } from '../../utils/functionMessageValidation';
import {
  registerFunctionCallComponent,
  registerFunctionResponseComponent,
  registerFunctionPairComponent,
  FunctionCallMessageProps,
  FunctionResponseMessageProps,
  FunctionPairComponentProps,
} from '../../utils/functionMessageRegistry';
import ChatBubble from '../ChatBubble';
import BlobImage from './BlobImage';

// Define the expected arguments for the generate_image tool call
const argsKeys = {
  text: 'string',
  input_hashes: 'array?',
  want_image: 'boolean?',
} as const;

const GenerateImageCall: React.FC<FunctionCallMessageProps> = ({ functionCall, messageId, messageInfo, children }) => {
  const args = functionCall.args;
  if (!validateExactKeys(args, argsKeys)) {
    return children;
  }

  // Convert input_hashes to FileAttachment array
  const inputHashes = args.input_hashes || [];

  // Determine title based on want_image parameter
  const title = args.want_image ? 'generate_image: want_image' : 'generate_image: !want_image';

  return (
    <ChatBubble
      messageId={messageId}
      containerClassName="agent-message"
      bubbleClassName="agent-function-call function-message-bubble"
      messageInfo={messageInfo}
      title={title}
      heighten={false}
    >
      <p>{args.text}</p>
      {inputHashes.length > 0 && (
        <div className="input-images-container" style={{ marginTop: '10px' }}>
          {inputHashes.map((hash, index) => (
            <BlobImage key={index} hash={hash} alt={`Input image ${hash.substring(0, 8)}`} />
          ))}
        </div>
      )}
    </ChatBubble>
  );
};

// Define the expected response for the generate_image tool
const responseKeys = {
  response: 'string',
} as const;

const GenerateImageResponse: React.FC<FunctionResponseMessageProps> = ({
  functionResponse,
  messageId,
  messageInfo,
  children,
}) => {
  const response = functionResponse.response;
  if (!validateExactKeys(response, responseKeys)) {
    return children;
  }

  // Function to convert hash strings to image elements
  const renderHashesAsImages = (text: string) => {
    // Split by whitespace to get individual hashes
    const parts = text.split(/(\s+)/);

    return parts.map((part, index) => {
      // Check if this part looks like a hash (64 character hex string for SHA-512/256)
      if (part.match(/^[a-f0-9]{64}$/i)) {
        return <BlobImage key={index} hash={part} alt={`Generated image ${part}`} />;
      }
      // Return whitespace or other text as-is
      return part === ' ' ? ' ' : part;
    });
  };

  return (
    <ChatBubble
      messageId={messageId}
      containerClassName="user-message"
      bubbleClassName="function-message-bubble"
      messageInfo={messageInfo}
      title="Generated Images"
      heighten={false}
    >
      <div className="generate-image-response">{renderHashesAsImages(response.response)}</div>
    </ChatBubble>
  );
};

const GenerateImagePair: React.FC<FunctionPairComponentProps> = ({
  functionCall,
  functionResponse,
  onToggleView,
  responseMessageInfo,
  children,
  responseMessageId,
}) => {
  const args = functionCall.args;
  const response = functionResponse.response;

  if (!validateExactKeys(args, argsKeys) || !validateExactKeys(response, responseKeys)) {
    return children;
  }

  // Convert input_hashes to FileAttachment array
  const inputHashes = args.input_hashes || [];

  // Function to convert hash strings to image elements
  const renderHashesAsImages = (text: string) => {
    const parts = text.split(/(\s+)/);

    return parts.map((part, index) => {
      if (part.match(/^[a-f0-9]{64}$/i)) {
        return <BlobImage key={index} hash={part} alt={`Generated image ${part}`} />;
      }
      return part;
    });
  };

  return (
    <ChatBubble
      containerClassName="function-pair-combined-container"
      bubbleClassName="function-combined-bubble"
      messageInfo={responseMessageInfo}
      title="Image generation"
      onHeaderClick={onToggleView}
    >
      <ChatBubble
        messageId={`${responseMessageId}.user`}
        containerClassName="user-message"
        bubbleClassName="user-message-bubble-content"
        heighten={false}
      >
        {args.text}
        {inputHashes.length > 0 && (
          <div className="input-images-container" style={{ marginTop: '10px' }}>
            {inputHashes.map((hash, index) => (
              <BlobImage key={index} hash={hash} alt={`Input image ${hash.substring(0, 8)}`} />
            ))}
          </div>
        )}
      </ChatBubble>
      <ChatBubble messageId={`${responseMessageId}.model`} containerClassName="agent-message">
        <div className="generate-image-response">{renderHashesAsImages(response.response)}</div>
      </ChatBubble>
    </ChatBubble>
  );
};

registerFunctionCallComponent('generate_image', GenerateImageCall);
registerFunctionResponseComponent('generate_image', GenerateImageResponse);
registerFunctionPairComponent('generate_image', GenerateImagePair);
