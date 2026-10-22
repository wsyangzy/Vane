import { createContext, useContext } from 'react';

export const PoolContext = createContext(null);
export const usePoolAccounts = () => useContext(PoolContext);
