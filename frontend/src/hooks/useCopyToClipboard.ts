import { useSetAtom } from 'jotai';
import { toastMessageAtom } from '../atoms/uiAtoms';

export const useCopyToClipboard = () => {
  const setToastMessage = useSetAtom(toastMessageAtom);

  const copyToClipboard = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setToastMessage('Copied to clipboard');
    } catch (err) {
      console.error('Failed to copy text:', err);
      setToastMessage('Failed to copy to clipboard');
    }
  };

  return { copyToClipboard };
};
