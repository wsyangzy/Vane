import { useEffect, useId, useRef, useState } from 'react';

export default function useChannelSetupNavigation({ loading, saving, setError }) {
  const id = useId();
  const [step, setStep] = useState(0);
  const [furthestStep, setFurthestStep] = useState(0);
  const destination = useRef(null);
  const formId = `${id}-${step === 2 ? 1 : step}`;

  useEffect(() => {
    // Reuse each mounted form's validation and payload builder when crossing
    // previously visited steps, including the shared credentials/billing form.
    if (destination.current !== null && step < destination.current) {
      document.getElementById(formId)?.requestSubmit();
    } else {
      destination.current = null;
    }
  }, [step, formId]);

  const advance = () => {
    setFurthestStep(current => Math.max(current, step + 1));
    setError('');
    setStep(step + 1);
  };
  const moveForward = (index) => {
    destination.current = index;
    document.getElementById(formId)?.requestSubmit();
  };
  const navigate = (index) => {
    if (loading || saving.current || index === step || index < 0 || index > furthestStep) return;
    setError('');
    if (index < step) {
      destination.current = null;
      setStep(index);
    } else {
      moveForward(index);
    }
  };
  const next = () => {
    if (!loading && !saving.current) moveForward(step + 1);
  };
  const cancelPendingNavigation = () => { destination.current = null; };

  return { id, step, furthestStep, formId, advance, navigate, next, cancelPendingNavigation };
}
