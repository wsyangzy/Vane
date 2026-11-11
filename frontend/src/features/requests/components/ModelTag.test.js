import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { getModelBrand } from '../utils/modelBrand.js';

const source = fs.readFileSync(new URL('./ModelTag.jsx', import.meta.url), 'utf8');

test('model brands survive versions, namespaces, case and mini variants', () => {
  const examples = {
    deepseek: ['deepseek-v4.1-flash', 'deepseek-ai/DeepSeek-V4-Pro', 'deepseek-mini'],
    glm: ['glm-5.3', 'z-ai/GLM-4.7-Flash'],
    kimi: ['kimi-k3.1-pro', 'moonshotai/Kimi-K2.6', 'moonshot-v1-128k'],
    qwen: ['qwen3.8-max', 'Qwen/Qwen3-Coder-Plus'],
    claude: ['claude-sonnet-4-5', 'opus-4.7'],
    openai: ['gpt-6.1-sol', 'o3-mini', 'openai/codex-mini-latest']
  };
  for (const [brand, models] of Object.entries(examples)) {
    for (const model of models) {
      assert.equal(getModelBrand(model, 'claude'), brand, model);
      assert.equal(getModelBrand(model, 'codex'), brand, model);
    }
  }
});

test('unknown and empty models use the request provider icon', () => {
  for (const model of ['', null, undefined, 'unknown', '-', 'custom-model', 'my-mini', 'notdeepseek']) {
    assert.equal(getModelBrand(model, 'claude'), 'claude');
    assert.equal(getModelBrand(model, 'codex'), 'codex');
  }
  assert.equal(getModelBrand('custom-model'), 'claude');
});

test('model tag keeps the full name and rewrite indicator on one line', () => {
  assert.match(source, /whitespace-nowrap/);
  assert.match(source, /rewriteIndicator\s*&&/);
  assert.match(source, /shrink-0/);
  assert.doesNotMatch(source, /truncate/);
  assert.doesNotMatch(source, /left-full/);
});

test('gpt-5.6 codename variants get distinct colors', () => {
  // sol / terra / luna 同属 5.6，此前共用 minor 配色而无法区分
  assert.match(source, /GPT5_CODENAME_COLOR_MAP/);

  // 代号判定必须早于 minor 配色，否则三者仍会一起落到 5.6 的配色上
  const codenameIdx = source.indexOf('GPT5_CODENAME_COLOR_MAP[codename]');
  const minorIdx = source.indexOf('GPT5_MINOR_COLOR_MAP[minorVersion]');
  assert.ok(codenameIdx > 0, 'codename lookup must exist');
  assert.ok(minorIdx > 0, 'minor lookup must exist');
  assert.ok(codenameIdx < minorIdx, 'codename lookup must precede the minor lookup');

  // 三个代号各自定义了配色，且互不相同
  const mapBlock = source.slice(source.indexOf('const GPT5_CODENAME_COLOR_MAP'));
  const styleLines = ['sol', 'terra', 'luna'].map((name) => {
    const line = mapBlock
      .split('\n')
      .find((text) => text.trimStart().startsWith(`${name}:`));
    assert.ok(line, `${name} must have its own color entry`);
    return line.trim();
  });
  assert.equal(new Set(styleLines).size, 3, 'sol / terra / luna must not share a color');
});

test('gpt-6-astra uses an OpenAI icon and a dedicated model style', () => {
  assert.match(source, /GPT6_CODENAME_COLOR_MAP/);
  assert.match(source, /astra:\s*createModelStyle\([^\n]*'openai'\)/);

  const gpt6Index = source.indexOf('const gpt6ColorClasses = getGpt6ColorClasses(lowerName)');
  const gpt5Index = source.indexOf('const gpt5ColorClasses = getGpt5ColorClasses(lowerName)');
  assert.ok(gpt6Index > 0, 'gpt-6 lookup must exist');
  assert.ok(gpt5Index > gpt6Index, 'gpt-6 lookup must precede the gpt-5 fallback');
});
