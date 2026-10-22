import { useCallback, useMemo, useRef, useState } from 'react';
import { ModalPortalContext } from './modalPortal.js';

// Provider workspaces can host their existing forms over an active canvas.
export default function ModalPortalScope({ children }) {
  const [target, setTarget] = useState(null);
  const [layerCount, setLayerCount] = useState(0);
  const layers = useRef(new Set());
  const registerLayer = useCallback((layer) => {
    for (const previous of layers.current) previous.inert = true;
    layers.current.add(layer);
    setLayerCount(layers.current.size);
    return () => {
      layers.current.delete(layer);
      const top = [...layers.current].at(-1);
      if (top) top.inert = false;
      setLayerCount(layers.current.size);
    };
  }, []);
  const isTopLayer = useCallback((layer) => [...layers.current].at(-1) === layer, []);
  const scope = useMemo(() => ({ target, setTarget, layerCount, registerLayer, isTopLayer }), [target, layerCount, registerLayer, isTopLayer]);
  return <ModalPortalContext.Provider value={scope}>{children}</ModalPortalContext.Provider>;
}
