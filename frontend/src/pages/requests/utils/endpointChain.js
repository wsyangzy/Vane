export const parseEndpointChain = (value) => {
  if (Array.isArray(value)) return value;
  if (typeof value !== 'string') return [];

  const raw = value.trim();
  if (!raw) return [];

  if (raw.startsWith('[')) {
    try {
      const parsed = JSON.parse(raw);
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      // Fall through to legacy delimiter parsing.
    }
  }

  return raw.split(' -> ').map((item) => item.trim()).filter(Boolean);
};

export const parseChainNode = (item, hop) => {
  if (!item) return { raw: '', channel: '', credentialGroup: '', endpoint: '', hop };

  if (typeof item === 'object' && item !== null) {
    const channel = item.channel || '';
    const credentialGroup = item.credential_group || item.credentialGroup || item.group_name || item.group || '';
    const endpoint = item.endpoint || '';
    const statusCode = Number(item.status_code ?? item.statusCode ?? item.http_status ?? item.httpStatus ?? 0) || 0;
    const error = item.error || item.error_message || item.errorMessage || item.failure_reason || item.failureReason || '';
    const raw = channel
      ? `${channel}::${credentialGroup ? `${credentialGroup}::` : ''}${endpoint}`
      : endpoint;
    return {
      raw,
      channel,
      credentialGroup,
      endpoint,
      statusCode,
      error,
      hop: item.hop || hop,
    };
  }

  const raw = String(item || '').trim();
  if (!raw) return { raw: '', channel: '', credentialGroup: '', endpoint: '', hop };

  const parts = raw.split('::');
  if (parts.length >= 3) {
    return {
      raw,
      channel: parts[0] || '',
      credentialGroup: parts[1] || '',
      endpoint: parts.slice(2).join('::') || '',
      hop,
    };
  }
  if (parts.length === 2) {
    return {
      raw,
      channel: parts[0] || '',
      credentialGroup: '',
      endpoint: parts[1] || '',
      hop,
    };
  }

  return { raw, channel: '', credentialGroup: '', endpoint: parts[0] || '', hop };
};

export const getEndpointChainItems = (request) => {
  const normalized = parseEndpointChain(request?.endpointChain);
  if (normalized.length > 0) return normalized;
  return parseEndpointChain(request?.endpoint_chain);
};

export const getRequestCredentialGroupLabel = (request = {}, chainNodes = []) => {
  const credentialGroup = String(chainNodes[chainNodes.length - 1]?.credentialGroup || '').trim();
  if (credentialGroup) return credentialGroup;

  const directGroup = String(request.credentialGroup || request.groupName || request.group_name || '').trim();
  if (directGroup) return directGroup;

  if (String(request.provider || '').toLowerCase() === 'codex') {
    return String(request.group || '').trim() || '-';
  }
  return '-';
};

export const getRequestChannelLabel = (request = {}, chainNodes = [], fallback = '-') => {
  const directChannel = String(request.channel || '').trim();
  if (directChannel) return directChannel;

  for (let index = chainNodes.length - 1; index >= 0; index -= 1) {
    const channel = String(chainNodes[index]?.channel || '').trim();
    if (channel) return channel;
  }

  if (String(request.provider || '').toLowerCase() !== 'codex') {
    return String(request.group || '').trim() || fallback;
  }
  return fallback;
};

export const chainHasChannel = (items) => (
  items.some((item) => Boolean(parseChainNode(item).channel))
);

export const chainHasMultipleChannels = (items) => {
  const channels = new Set();
  items.forEach((item) => {
    const channel = parseChainNode(item).channel;
    if (channel) channels.add(channel);
  });
  return channels.size > 1;
};

export const parseEndpointName = (item) => parseChainNode(item).endpoint;
