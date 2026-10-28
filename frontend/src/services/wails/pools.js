import { WailsApp, initWails } from './connection.js';

export async function checkPoolEndpoint(provider, id) {
  await initWails();
  if (typeof WailsApp?.CheckPoolEndpoint !== 'function') throw new Error('端点检测服务尚未就绪，请重启更新后的应用');
  await WailsApp.CheckPoolEndpoint(provider, Number(id));
}
