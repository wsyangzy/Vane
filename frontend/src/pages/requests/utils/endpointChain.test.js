import test from 'node:test';
import assert from 'node:assert/strict';

import {
  chainHasMultipleChannels,
  getEndpointChainItems,
  parseChainNode,
  parseEndpointChain,
  parseEndpointName,
} from './endpointChain.js';

test('parseEndpointChain keeps structured JSON chain nodes', () => {
  const chain = parseEndpointChain(JSON.stringify([
    { hop: 1, channel: 'CCTQ', credential_group: 'default', endpoint: 'Ark API' },
    { hop: 2, channel: 'Claude', credential_group: 'paid', endpoint: 'AWSQ' },
  ]));

  assert.equal(chain.length, 2);
  assert.deepEqual(parseChainNode(chain[0]), {
    raw: 'CCTQ::default::Ark API',
    channel: 'CCTQ',
    credentialGroup: 'default',
    endpoint: 'Ark API',
    hop: 1,
  });
  assert.equal(chainHasMultipleChannels(chain), true);
});

test('parseEndpointChain supports legacy transfer path strings', () => {
  const chain = parseEndpointChain('Claude::default::Ark API -> Claude::backup::AWSQ');

  assert.equal(chain.length, 2);
  assert.deepEqual(parseChainNode(chain[1], 2), {
    raw: 'Claude::backup::AWSQ',
    channel: 'Claude',
    credentialGroup: 'backup',
    endpoint: 'AWSQ',
    hop: 2,
  });
  assert.equal(parseEndpointName(chain[1]), 'AWSQ');
});

test('getEndpointChainItems falls back to raw endpoint_chain when normalized chain is empty', () => {
  const request = {
    endpointChain: [],
    endpoint_chain: JSON.stringify([
      { channel: 'CCTQ', credential_group: 'default', endpoint: 'Ark API' },
    ]),
  };

  const chain = getEndpointChainItems(request);

  assert.equal(chain.length, 1);
  assert.equal(parseChainNode(chain[0]).channel, 'CCTQ');
  assert.equal(parseChainNode(chain[0]).credentialGroup, 'default');
  assert.equal(parseChainNode(chain[0]).endpoint, 'Ark API');
});

test('chainHasMultipleChannels only reports actual inter-channel paths', () => {
  const sameChannelChain = [
    { channel: 'anyrouter', credential_group: '默认分组', endpoint: 'jp-node' },
    { channel: 'anyrouter', credential_group: '默认分组', endpoint: 'hk-node' },
  ];

  assert.equal(chainHasMultipleChannels(sameChannelChain), false);
  assert.equal(parseChainNode(sameChannelChain[0]).credentialGroup, '默认分组');

  assert.equal(chainHasMultipleChannels([
    { channel: 'anyrouter', credential_group: '默认分组', endpoint: 'hk-node' },
    { channel: 'Right Code', credential_group: 'AWSQ逆向渠道', endpoint: 'AWSQ逆向渠道' },
  ]), true);
});
