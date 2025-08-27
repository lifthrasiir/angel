import { registerFunctionCallComponent, registerFunctionResponseComponent } from './functionMessageRegistry';
import { ReadFileCall, ReadFileResponse } from '../components/tools/ReadFile';
import { WriteFileCall, WriteFileResponse } from '../components/tools/WriteFile';

export const registerAllToolComponents = () => {
  registerFunctionCallComponent('read_file', ReadFileCall);
  registerFunctionResponseComponent('read_file', ReadFileResponse);

  registerFunctionCallComponent('write_file', WriteFileCall);
  registerFunctionResponseComponent('write_file', WriteFileResponse);
};
