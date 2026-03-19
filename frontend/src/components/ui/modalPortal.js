import { createPortal } from 'react-dom';

export const renderModalPortal = (content) => {
  if (typeof document === 'undefined' || !document.body) {
    return content;
  }

  return createPortal(content, document.body);
};
