#!/usr/bin/env node
// Real Chromium/Swagger UI smoke test. Install Playwright outside the repository
// and set NODE_PATH to its node_modules; do not supply an MCP credential.
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');

const baseURL = (process.env.APITEST_BASE_URL || 'http://localhost:8080').replace(/\/$/, '');
const origin = new URL(baseURL).origin;
const serverName = process.env.APITEST_MCP_SERVER_NAME || 'campus-tools';
const serverOrigin = process.env.APITEST_MCP_BASE_URL || 'http://mcp-demo:8090';
const reportPath = path.join(__dirname, 'report', 'swagger-browser.json');
const steps = [];
let stage = 'launch';
let browser;

function publicServer(server) {
  assert.equal(typeof server.has_token, 'boolean');
  for (const key of ['token', 'credential_ref', 'token_env', 'generation', 'api_key']) {
    assert.equal(Object.hasOwn(server, key), false);
  }
}

async function main() {
  browser = await chromium.launch({ headless: true });
  const browserVersion = browser.version();
  const page = await browser.newPage();
  page.setDefaultTimeout(15000);
  const destinations = new Set();
  page.on('request', request => destinations.add(new URL(request.url()).origin));
  await page.goto(baseURL + '/swagger/index.html');
  await page.waitForSelector('.opblock');
  assert(await page.locator('.opblock').count() >= 33);

  // Every business request below is submitted by Swagger's real Execute button,
  // not by page.evaluate(fetch), APIRequestContext, a mock, or a request route.
  async function execute(method, template, expectedStatus, options = {}) {
    stage = method + ' ' + template;
    const op = page.locator('.opblock-' + method.toLowerCase()).filter({
      has: page.locator('.opblock-summary-path').filter({ hasText: new RegExp('^' + template.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '$') }),
    });
    assert.equal(await op.count(), 1);
    if (!/\bis-open\b/.test(await op.getAttribute('class'))) await op.locator('.opblock-summary-control').click();
    stage = method + ' ' + template + ' (enable Try it out)';
    const tryButton = op.locator('button.try-out__btn');
    await tryButton.waitFor({ state: 'visible' });
    if ((await tryButton.innerText()).trim() === 'Try it out') await tryButton.click();
    let target = template;
    for (const [name, value] of Object.entries(options.params || {})) {
      await op.locator(`input[placeholder="${name}"]`).fill(String(value));
      target = target.replace('{' + name + '}', String(value));
    }
    if (options.body) {
      stage = method + ' ' + template + ' (edit request body)';
      await op.locator('textarea.body-param__text').fill(JSON.stringify(options.body));
    }
    stage = method + ' ' + template + ' (Execute)';
    const waiting = page.waitForResponse(response => response.request().method() === method && new URL(response.url()).pathname === target, { timeout: 60000 });
    const [response] = await Promise.all([
      waiting,
      op.getByRole('button', { name: 'Execute', exact: true }).click(),
    ]);
    steps.push({ method, path: target, status: response.status(), expected_status: expectedStatus });
    assert.equal(response.status(), expectedStatus);
    if (options.token) assert.equal(response.request().headers().authorization, 'Bearer ' + options.token);
    return expectedStatus === 204 ? {} : await response.json();
  }

  await execute('GET', '/api/mcp/servers', 401);
  const login = await execute('POST', '/api/login', 200, { body: {
    username: process.env.APITEST_TEACHER_USERNAME || 'teacher_a',
    password: process.env.APITEST_TEACHER_PASSWORD || 'CampusClaw#2026',
  } });
  assert(login.token && login.user.role === 'teacher');
  const token = login.token;
  await page.evaluate(value => window.ui.preauthorizeApiKey('BearerAuth', 'Bearer ' + value), token);
  const listed = await execute('GET', '/api/mcp/servers', 200, { token });
  listed.servers.forEach(publicServer);
  let server = listed.servers.find(item => item.name === serverName);
  const registration = await execute('POST', '/api/mcp/servers', server ? 400 : 201, {
    token, body: { name: serverName, base_url: serverOrigin },
  });
  if (!server) server = registration;
  publicServer(server);
  assert(server.id && server.has_token);
  const params = { id: server.id };
  const connected = await execute('POST', '/api/mcp/servers/{id}/connect', 200, { params, token });
  publicServer(connected);
  assert.equal(connected.state, 'connected', 'Pending changes require teacher review; this test never auto-approves them');
  const directory = await execute('GET', '/api/mcp/servers/{id}/tools', 200, { params, token });
  assert.deepEqual(directory.tools.map(tool => tool.name).sort(), ['formula_hint', 'unit_convert']);
  assert(directory.tools.every(tool => tool.description && tool.inputSchema.type === 'object'));
  // A deliberately stale hash exercises the real approval handler and its 409
  // contract, without approving unknown definitions or manufacturing a change.
  await execute('POST', '/api/mcp/servers/{id}/approve', 409, { params, token, body: { snapshot_hash: '0'.repeat(64) } });
  const refs = ['knowledge_search', `formula_hint@${serverName}`, `unit_convert@${serverName}`];
  const source = `名称：Swagger验证${Date.now()}\n适用条件：勾股,单位\n资料：数学\n工具：${refs.join(',')}\n启用：是\n\n只提示下一步需要检查什么，不给最终得数。\n`;
  const skill = await execute('POST', '/api/skills', 201, { token, body: { source } });
  assert.equal(skill.source, source);
  assert.deepEqual(skill.tools, refs);
  const skills = await execute('GET', '/api/skills', 200, { token });
  const saved = skills.skills.find(item => item.id === skill.id);
  assert.equal(saved.source, source);
  assert.deepEqual(saved.tools, refs);
  await execute('GET', '/api/mcp/audit', 200, { token });
  await execute('GET', '/api/me', 200, { token });
  await execute('POST', '/api/mcp/servers/{id}/disconnect', 204, { params, token });
  const unavailable = await execute('POST', '/api/skills', 400, { token, body: { source } });
  assert(unavailable.error.includes('工具'));
  const unchanged = await execute('GET', '/api/skills', 200, { token });
  assert.equal(unchanged.skills.find(item => item.id === skill.id).source, source);
  stage = 'browser credential boundary';
  assert.deepEqual([...destinations], [origin]);
  await browser.close();
  browser = undefined;
  const report = { browser: 'Chromium ' + browserVersion, interface: 'Swagger UI Execute buttons',
    mcp_credentials_supplied: false, same_origin_requests_only: true,
    approval_branch: 'stale hash 409; no unknown definitions auto-approved',
    registration: listed.servers.some(item => item.name === serverName) ? 'existing fixed registration reused; duplicate 400 verified' : 'created 201',
    steps };
  await fs.mkdir(path.dirname(reportPath), { recursive: true });
  await fs.writeFile(reportPath, JSON.stringify(report, null, 2) + '\n');
  console.log(`PASS: real Chromium Swagger UI, ${steps.length} Execute requests; safe report: ${reportPath}`);
}

main().catch(async error => {
  // Playwright exception messages/traces may contain DOM values or a cURL with
  // the application Bearer. Never dump them, request headers, or response bodies.
  const category = error.message.includes('strict mode violation') ? 'ambiguous selector' : error.name;
  console.error('FAIL: Swagger browser stage:', stage, '(' + category + ')');
  if (steps.length) console.error('Last safe HTTP evidence:', JSON.stringify(steps.at(-1)));
  if (browser) await browser.close();
  process.exitCode = 1;
});
