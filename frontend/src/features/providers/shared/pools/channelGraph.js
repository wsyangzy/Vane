const ROOT = { x: 24, width: 160, height: 76 };
const GROUP = { x: 244, width: 190, height: 80 };
const ENDPOINT = { x: 494, width: 228, height: 66 };
const PADDING = 24;
const ENDPOINT_GAP = 12;
const GROUP_GAP = 24;
export const MIN_GRAPH_ZOOM = 0.1;
export const MAX_GRAPH_ZOOM = 1.75;

const centerY = (node) => node.y + node.height / 2;

function connect(source, target) {
  const x1 = source.x + source.width;
  const y1 = centerY(source);
  const x2 = target.x;
  const y2 = centerY(target);
  const bend = (x2 - x1) * 0.55;
  return {
    key: `${source.key}/${target.key}`,
    source: source.key,
    target: target.key,
    trunk: source.kind === 'channel',
    path: `M ${x1} ${y1} C ${x1 + bend} ${y1}, ${x2 - bend} ${y2}, ${x2} ${y2}`
  };
}

export function buildChannelGraph(name, groups, endpoints) {
  const branches = groups.map((group) => ({
    group,
    endpoints: endpoints
      .filter((endpoint) => Number(endpoint.groupId) === Number(group.id) || !endpoint.groupId && (group.isDefault || group.is_default))
      .sort((a, b) => Number(a.priority || 0) - Number(b.priority || 0))
  }));
  const spans = branches.map((branch) => Math.max(GROUP.height, branch.endpoints.length * ENDPOINT.height + Math.max(0, branch.endpoints.length - 1) * ENDPOINT_GAP));
  const contentHeight = spans.reduce((sum, span) => sum + span, 0) + Math.max(0, groups.length - 1) * GROUP_GAP;
  const height = Math.max(180, PADDING * 2 + contentHeight);
  const root = { ...ROOT, y: (height - ROOT.height) / 2, key: 'channel', kind: 'channel', label: name };
  const nodes = [root];
  const links = [];
  let cursor = (height - contentHeight) / 2;

  branches.forEach(({ group, endpoints: children }, index) => {
    const node = { ...GROUP, y: cursor + (spans[index] - GROUP.height) / 2, key: `group:${group.id}`, kind: 'group', label: group.name, group, endpointCount: children.length };
    nodes.push(node);
    links.push(connect(root, node));
    children.forEach((endpoint, endpointIndex) => {
      const child = { ...ENDPOINT, y: cursor + endpointIndex * (ENDPOINT.height + ENDPOINT_GAP) + (spans[index] - (children.length * ENDPOINT.height + (children.length - 1) * ENDPOINT_GAP)) / 2, key: `endpoint:${endpoint.id}`, kind: 'endpoint', label: endpoint.name, endpoint, group };
      nodes.push(child);
      links.push(connect(node, child));
    });
    cursor += spans[index] + GROUP_GAP;
  });

  return { nodes, links, width: Math.max(...nodes.map((node) => node.x + node.width)) + PADDING, height };
}

export function fitChannelGraph(graph, viewport) {
  const zoom = Math.max(MIN_GRAPH_ZOOM, Math.min(1, (viewport.width - 40) / graph.width, (viewport.height - 32) / graph.height));
  return { zoom, x: (viewport.width - graph.width * zoom) / 2, y: (viewport.height - graph.height * zoom) / 2 };
}

export function zoomChannelGraph(view, zoom, point) {
  const next = Math.max(MIN_GRAPH_ZOOM, Math.min(MAX_GRAPH_ZOOM, zoom));
  return { zoom: next, x: point.x - (point.x - view.x) * next / view.zoom, y: point.y - (point.y - view.y) * next / view.zoom };
}
