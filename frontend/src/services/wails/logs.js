import { WailsApp, initWails } from './connection.js';

export const getRecentLogs = async (limit) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.GetRecentLogs(limit);
};

export const getLogStreamStatus = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.GetLogStreamStatus();
};

export const startLogStream = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.StartLogStream();
};

export const stopLogStream = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.StopLogStream();
};

export const saveLogFile = async (content, defaultFileName) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.SaveLogFile(content, defaultFileName);
};
