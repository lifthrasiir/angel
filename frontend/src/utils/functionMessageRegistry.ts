import React from 'react';
import { FunctionCall, FunctionResponse, FileAttachment } from '../types/chat';

export interface FunctionCallMessageProps {
  functionCall: FunctionCall;
  messageId?: string;
  messageInfo?: React.ReactNode;
  children?: React.ReactNode;
}

export interface FunctionResponseMessageProps {
  functionResponse: FunctionResponse;
  messageId?: string;
  attachments?: FileAttachment[];
  sessionId?: string;
  messageInfo?: React.ReactNode;
  children?: React.ReactNode;
}

export interface FunctionPairComponentProps {
  functionCall: FunctionCall;
  functionResponse: FunctionResponse;
  callMessageId?: string;
  responseMessageId?: string;
  onToggleView: () => void;
  attachments?: FileAttachment[];
  sessionId?: string;
  callMessageInfo?: React.ReactNode;
  responseMessageInfo?: React.ReactNode;
  children?: React.ReactNode;
}

type FunctionCallComponent = React.ComponentType<FunctionCallMessageProps>;
type FunctionResponseComponent = React.ComponentType<FunctionResponseMessageProps>;
type FunctionPairComponent = React.ComponentType<FunctionPairComponentProps>;

interface FunctionMessageRegistry {
  callComponents: Map<string, FunctionCallComponent>;
  responseComponents: Map<string, FunctionResponseComponent>;
  pairComponents: Map<string, FunctionPairComponent>;
}

const registry: FunctionMessageRegistry = {
  callComponents: new Map(),
  responseComponents: new Map(),
  pairComponents: new Map(),
};

export const registerFunctionCallComponent = (functionName: string, component: FunctionCallComponent) => {
  registry.callComponents.set(functionName, component);
};

export const registerFunctionResponseComponent = (functionName: string, component: FunctionResponseComponent) => {
  registry.responseComponents.set(functionName, component);
};

export const registerFunctionPairComponent = (functionName: string, component: FunctionPairComponent) => {
  registry.pairComponents.set(functionName, component);
};

export const getFunctionCallComponent = (functionName: string) => {
  return registry.callComponents.get(functionName);
};

export const getFunctionResponseComponent = (functionName: string) => {
  return registry.responseComponents.get(functionName);
};

export const getFunctionPairComponent = (functionName: string) => {
  return registry.pairComponents.get(functionName);
};
