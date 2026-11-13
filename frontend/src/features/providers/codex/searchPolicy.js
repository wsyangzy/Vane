export const searchVendors = [
  { value: 'tavily', label: 'Tavily', endpoint: 'https://api.tavily.com/search', description: '网页与图片查询，支持域名和时效筛选。' },
  { value: 'brave', label: 'Brave Search', endpoint: 'https://api.search.brave.com/res/v1/web/search', description: '网页与图片查询。网页查询支持时效筛选，图片查询不支持精确日期。' },
  { value: 'exa', label: 'Exa', endpoint: 'https://api.exa.ai/search', description: '网页查询与正文提取，支持域名和日期筛选；此适配不提供图片查询。' },
  { value: 'searxng', label: 'SearXNG', endpoint: '', description: '接入自建实例，需启用 JSON 输出。支持网页与图片；时效支持 1、30/31、365/366 天，取决于实例的搜索引擎。' }
];
export const defaultSearchPolicy = () => ({ mode: 'auto', vendor: 'tavily', endpoint: searchVendors[0].endpoint, timeoutMs: 30000, maxResults: 5, hasAPIKey: false });
