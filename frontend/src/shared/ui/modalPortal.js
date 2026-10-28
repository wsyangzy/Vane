import { createContext, createElement, useContext } from 'react';
import { createPortal } from 'react-dom';
import ScopedModalPortal from './ScopedModalPortal.jsx';
import SurfaceMotion from './SurfaceMotion.jsx';

export const ModalPortalContext = createContext(null);

function ModalPortal({ content, root }) {
  const scope = useContext(ModalPortalContext);
  const animatedContent = createElement(SurfaceMotion, { content });
  if (!root && scope?.target) return createElement(ScopedModalPortal, { content: animatedContent, scope });
  return typeof document === 'undefined' || !document.body ? content : createPortal(animatedContent, document.body);
}

export const renderModalPortal = (content, { root = false } = {}) => {
  if (typeof document === 'undefined' || !document.body) {
    return content;
  }

  return createElement(ModalPortal, { content, root });
};
