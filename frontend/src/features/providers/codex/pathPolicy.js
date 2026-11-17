export const PATH_STRATEGY_OPTIONS = [
  { value: 'v1', label: '标准 /v1 路径' },
  { value: 'base', label: '按 API 基础地址拼接' }
];

export const previewEndpointPath = (baseURL, strategy) => {
  const base = String(baseURL || '').trim().replace(/\/+$/, '');
  if (!base) return '';
  if (strategy === 'legacy') return '保留原有路径行为；检测并选择策略后再迁移';
  return `${base}${strategy === 'v1' && !base.endsWith('/v1') ? '/v1' : ''}/responses`;
};

export const buildEndpointProbeInput = ({ channel, credentialGroup, endpoint, baseURL, model, pathStrategy, verifyResponse }) => {
  const savedChannel = Number(channel?.id) > 0;
  return {
    channelId: savedChannel ? Number(channel.id) : 0,
    credentialGroupId: savedChannel ? Number(credentialGroup?.id) || 0 : 0,
    endpointId: Number(endpoint?.id) > 0 ? Number(endpoint.id) : 0,
    baseURL, model, pathStrategy, verifyResponse,
    apiKey: savedChannel ? undefined : credentialGroup?.apiKey || credentialGroup?.api_key
  };
};

export const PROBE_RESULT_LABELS = {
  models_verified: '模型接口可用，生成接口尚未验证',
  response_verified: '流式请求验证通过',
  authentication_failed: '鉴权失败，请检查凭证或权限',
  rate_limited: '上游限流，暂时无法确认',
  network_error: '连接失败，暂时无法确认',
  http_error: '接口返回错误，暂时无法确认',
  invalid_response: '响应不符合接口协议',
  invalid_stream_response: '响应不符合流式协议',
  incomplete_response: '响应未完整结束',
  incomplete_stream: '响应未完整结束',
  empty_response: '上游返回空响应',
  response_read_error: '响应读取失败',
  response_too_large: '检测响应超出大小限制',
  response_incomplete: '上游返回未完成事件',
  upstream_stream_error: '上游返回流式错误',
  invalid_stream_event: '上游返回损坏的流式事件',
  non_stream_response: '流式请求收到普通 JSON 响应',
  empty_stream: '上游返回空响应',
  stream_prelude_too_large: '上游在有效输出前超出大小限制',
  stream_event_too_large: '单条流式事件超出大小限制',
  stream_probe_too_large: '验证响应超出大小限制'
};
