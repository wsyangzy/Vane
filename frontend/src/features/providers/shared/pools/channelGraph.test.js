import assert from 'node:assert/strict';
import test from 'node:test';
import { buildChannelGraph, fitChannelGraph, zoomChannelGraph } from './channelGraph.js';

test('channel graph keeps endpoint ownership and priority without changing source arrays', () => {
  const groups = [{ id: 1, name: '默认', isDefault: true }, { id: 2, name: '团队' }];
  const endpoints = [{ id: 4, groupId: 2, priority: 2 }, { id: 3, groupId: 2, priority: 1 }, { id: 2, groupId: 0, priority: 2 }, { id: 1, groupId: '1', priority: 1 }];
  const graph = buildChannelGraph('渠道', groups, endpoints);
  assert.deepEqual(graph.nodes.filter((node) => node.kind === 'endpoint').map((node) => [node.endpoint.id, node.group.id]), [[1, 1], [2, 1], [3, 2], [4, 2]]);
  assert.deepEqual(endpoints.map((endpoint) => endpoint.id), [4, 3, 2, 1]);
  assert.equal(graph.links.length, graph.nodes.length - 1);
});

test('uneven branches and empty groups fit without overlapping sibling nodes', () => {
  const groups = [{ id: 1 }, { id: 2 }, { id: 3 }, { id: 4 }];
  const endpoints = Array.from({ length: 30 }, (_, id) => ({ id, groupId: id < 24 ? 1 : 3, priority: id }));
  const graph = buildChannelGraph('渠道', groups, endpoints);
  for (const kind of ['group', 'endpoint']) {
    const nodes = graph.nodes.filter((node) => node.kind === kind);
    for (let i = 1; i < nodes.length; i++) assert.ok(nodes[i].y >= nodes[i - 1].y + nodes[i - 1].height);
  }
  for (const node of graph.nodes) {
    assert.ok(node.x >= 0 && node.y >= 0);
    assert.ok(node.x + node.width <= graph.width && node.y + node.height <= graph.height);
  }
  const fitted = fitChannelGraph(graph, { width: 780, height: 330 });
  assert.ok(fitted.y >= 0 && fitted.y + graph.height * fitted.zoom <= 330);
  const empty = buildChannelGraph('空渠道', [], []);
  assert.equal(empty.nodes.length, 1);
  assert.equal(empty.links.length, 0);
  assert.ok(Object.values(fitChannelGraph(empty, { width: 760, height: 330 })).every(Number.isFinite));
});

test('fit keeps workflow cards inside compact and regular canvases', () => {
  const graph = buildChannelGraph('渠道', [{ id: 1 }, { id: 2 }, { id: 3 }], Array.from({ length: 6 }, (_, id) => ({ id, groupId: Math.floor(id / 2) + 1 })));
  for (const [height, minimumZoom] of [[394, 0.65], [470, 0.8]]) {
    const viewport = { width: 780, height };
    const view = fitChannelGraph(graph, viewport);
    assert.ok(view.zoom <= 1 && view.zoom >= minimumZoom);
    assert.ok(view.x >= 0 && view.y >= 0);
    assert.ok(view.x + graph.width * view.zoom <= viewport.width);
    assert.ok(view.y + graph.height * view.zoom <= viewport.height);
  }
});

test('zoom preserves the scene point under the pointer and respects its limits', () => {
  const view = { zoom: 0.8, x: -20, y: 50 };
  const point = { x: 320, y: 160 };
  for (const requested of [0.01, 1.2, 3]) {
    const next = zoomChannelGraph(view, requested, point);
    assert.ok(next.zoom >= 0.1 && next.zoom <= 1.75);
    assert.ok(Math.abs((point.x - next.x) / next.zoom - (point.x - view.x) / view.zoom) < 1e-9);
    assert.ok(Math.abs((point.y - next.y) / next.zoom - (point.y - view.y) / view.zoom) < 1e-9);
  }
});
