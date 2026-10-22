import { createContext, createElement, useContext } from 'react';
import { createPortal } from 'react-dom';
import ScopedModalPortal from './ScopedModalPortal.jsx';

export const ModalPortalContext = createContext(null);

function ModalPortal({ content }) {
  const scope = useContext(ModalPortalContext);
  if (scope?.target) return createElement(ScopedModalPortal, { content, scope });
  return typeof document === 'undefined' || !document.body ? content : createPortal(content, document.body);
}

export const renderModalPortal = (content, { root = false } = {}) => {
  if (typeof document === 'undefined' || !document.body) {
    return content;
  }

  return root ? createPortal(content, document.body) : createElement(ModalPortal, { content });
};
