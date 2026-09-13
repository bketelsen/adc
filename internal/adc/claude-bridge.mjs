// ADC's private stdio bridge to the official Claude Agent SDK. Authentication
// remains with Claude's native CLI; this bridge never implements OAuth.
import readline from 'node:readline';
import {createRequire} from 'node:module';
import {pathToFileURL} from 'node:url';
import {randomUUID} from 'node:crypto';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';

const sdkPath = process.argv[2], cliPath = process.argv[3];
const {query} = await import(pathToFileURL(sdkPath));
const require = createRequire(sdkPath);
const {McpServer} = require('@modelcontextprotocol/sdk/server/mcp.js');
const {ListToolsRequestSchema, CallToolRequestSchema} = require('@modelcontextprotocol/sdk/types.js');
const write = value => process.stdout.write(JSON.stringify(value) + '\n');
const event = (method, params) => write({method, params});
const pending = new Map(), sessions = new Map();
const rpc = (method, params) => new Promise((resolve, reject) => {
  const id = randomUUID(); pending.set(id, {resolve, reject}); write({id, method, params});
});
// [1m] selects context capacity; Claude strips it from response model IDs.
const sameModel = (actual, requested) => actual.replace(/\[1m\]$/, '') === requested.replace(/\[1m\]$/, '');
const nativeTools = ['Bash', 'Read', 'Write', 'Edit', 'Glob', 'Grep', 'WebFetch', 'WebSearch'];
const baseOptions = () => ({
  cwd: process.cwd(), env: {...process.env}, pathToClaudeCodeExecutable: cliPath,
  settingSources: [], settings: {forceLoginMethod: 'claudeai'}, plugins: [], skills: [],
  persistSession: false, strictMcpConfig: true, tools: [], agents: {},
  permissionMode: 'bypassPermissions', allowDangerouslySkipPermissions: true,
  onElicitation: async () => ({action: 'decline'}),
  onUserDialog: async () => ({behavior: 'cancelled'}), stderr: () => {},
});
async function authStatus() {
  let stdout;
  try { ({stdout} = await promisify(execFile)(cliPath, ['auth', 'status'], {env: process.env, timeout: 15000, maxBuffer: 65536})); }
  catch (e) { if (e.code === 1 && e.stdout) stdout = e.stdout; else throw new Error('Claude authentication status unavailable'); }
  const a = JSON.parse(stdout);
  if (!a.loggedIn) return {account: null};
  if (a.authMethod !== 'claude.ai') throw new Error('Connect a Claude subscription using the displayed native sign-in command; API billing is not selected');
  return {account: {type: 'claude-subscription', email: a.email || '', planType: a.subscriptionType || ''}};
}
function idlePrompt() { let release; const wait = new Promise(r => release = r); return {release, stream: (async function* () { await wait; })()}; }
async function models() {
  const login = await authStatus(); if (!login.account) throw new Error('Sign in to this ADC Claude account first');
  const idle = idlePrompt(); const q = query({prompt: idle.stream, options: baseOptions()});
  try {
    const ms = await q.supportedModels();
    const unique = new Map();
    for (const m of ms) {
      const id = m.resolvedModel || m.value;
      if (id.startsWith('claude-')) unique.set(id, {id, model: id, displayName: m.displayName});
    }
    return {data: [...unique.values()]};
  } finally { idle.release(); q.close(); }
}
function reportUsage(s, model, totals) {
  const mapped = {inputTokens: totals.inputTokens, outputTokens: totals.outputTokens, cachedInputTokens: totals.cacheReadInputTokens, cacheWriteInputTokens: totals.cacheCreationInputTokens};
  event('thread/tokenUsage/updated', {threadId: s.id, model, tokenUsage: {total: mapped}});
}
async function consume(s) {
  try {
    for await (const m of s.q) {
      if (m.type === 'system' && m.subtype === 'init' && !sameModel(m.model, s.model)) throw new Error('Claude returned a different model; no substitution permitted');
      if (m.type === 'assistant') {
        const model = m.message.model;
        if (model && !sameModel(model, s.model)) throw new Error(`Claude changed the execution model from ${s.model} to ${model}; no substitution permitted`);
        const u = m.message.usage;
        if (u && m.message.id && model) {
          s.messages.set(m.message.id, {model, inputTokens: u.input_tokens, outputTokens: u.output_tokens, cacheReadInputTokens: u.cache_read_input_tokens, cacheCreationInputTokens: u.cache_creation_input_tokens});
          const total = {};
          for (const x of s.messages.values()) if (x.model === model) for (const field of ['inputTokens', 'outputTokens', 'cacheReadInputTokens', 'cacheCreationInputTokens']) if (Number.isFinite(x[field]) && x[field] >= 0) total[field] = (total[field] ?? 0) + x[field];
          reportUsage(s, model, total);
        }
        for (const item of m.message.content || []) {
          if (item.type === 'text') event('item/completed', {threadId: s.id, item: {id: m.uuid, type: 'agentMessage', text: item.text}});
          if (item.type === 'tool_use' && !item.name.startsWith('mcp__adc__')) {
            const mcp = item.name.match(/^mcp__(.+?)__(.+)$/);
            const trace = {id: item.id, type: mcp ? 'mcpToolCall' : ['Write', 'Edit'].includes(item.name) ? 'fileChange' : item.name === 'Bash' ? 'commandExecution' : 'nativeTool', server: mcp?.[1], tool: mcp?.[2] || item.name, arguments: item.input, status: 'running'};
            s.tools.set(item.id, trace); event('item/started', {threadId: s.id, item: trace});
          }
        }
      }
      if (m.type === 'user' && Array.isArray(m.message?.content)) for (const item of m.message.content) {
        const trace = s.tools.get(item.tool_use_id);
        if (item.type === 'tool_result' && trace) { event('item/completed', {threadId: s.id, item: {...trace, status: item.is_error ? 'failed' : 'completed', result: item.content}}); s.tools.delete(item.tool_use_id); }
      }
      if (m.type === 'result') {
        for (const [model, totals] of Object.entries(m.modelUsage || {})) reportUsage(s, model, totals);
        event('turn/completed', {threadId: s.id, turn: {id: s.id, status: m.is_error ? 'failed' : 'completed', error: m.is_error ? {message: (m.errors || [m.subtype]).join('; ')} : null}});
      }
    }
  } catch (e) {
    if (!s.stopping) event('turn/completed', {threadId: s.id, turn: {id: s.id, status: 'failed', error: {message: String(e.message || e)}}});
  }
}
async function handle(method, p) {
  switch (method) {
    case 'initialize': return {};
    case 'account/read': return authStatus();
    case 'account/logout': await promisify(execFile)(cliPath, ['auth', 'logout'], {env: process.env, timeout: 15000}); return {};
    case 'model/list': return models();
    case 'thread/start': {
      if (!(await authStatus()).account) throw new Error('Sign in to this ADC Claude account first');
      const id = randomUUID();
      sessions.set(id, {id, model: p.model, params: p, tools: new Map(), messages: new Map()});
      return {thread: {id}, model: p.model};
    }
    case 'turn/start': {
      const s = sessions.get(p.threadId); if (!s) throw new Error('Unknown thread');
      if (Object.hasOwn(s.params.config.mcpServers, 'adc')) throw new Error('The MCP connection name adc is reserved for ADC tools; rename that connection');
      const mc = new McpServer({name: 'adc', version: '1.0.0'}, {capabilities: {tools: {}}});
      mc.server.setRequestHandler(ListToolsRequestSchema, async () => ({tools: s.params.dynamicTools.map(t => ({name: t.name, description: t.description, inputSchema: t.inputSchema}))}));
      mc.server.setRequestHandler(CallToolRequestSchema, async req => {
        const result = await rpc('item/tool/call', {threadId: s.id, callId: randomUUID(), tool: req.params.name, arguments: req.params.arguments || {}});
        return {isError: !result.success, content: result.contentItems.map(x => ({type: 'text', text: x.text}))};
      });
      const options = {...baseOptions(), cwd: s.params.cwd, model: s.model, systemPrompt: s.params.developerInstructions, effort: p.effort,
        tools: s.params.config.proposal ? [] : nativeTools,
        mcpServers: {...s.params.config.mcpServers, adc: {type: 'sdk', name: 'adc', instance: mc}},
      };
      const text = p.input.map(x => x.text).join('\n');
      let release;
      const ready = new Promise(r => release = r);
      s.q = query({prompt: (async function* () { await ready; yield {type: 'user', message: {role: 'user', content: text}, parent_tool_use_id: null}; })(), options});
      // Do not spend a model turn on an assignment whose required tools failed
      // to start. This includes ADC's own durable outcome tools.
      try {
        const required = Object.keys(options.mcpServers);
        const deadline = Date.now() + 20000;
        while (true) {
          const states = await s.q.mcpServerStatus();
          if (required.every(name => states.some(x => x.name === name && x.status === 'connected'))) break;
          const failed = states.find(x => required.includes(x.name) && ['failed', 'needs-auth', 'disabled'].includes(x.status));
          if (failed || Date.now() >= deadline) throw new Error('Required MCP connection unavailable: ' + (failed?.name || required.join(', ')));
          await new Promise(r => setTimeout(r, 100));
        }
      } catch (e) { s.stopping = true; s.q.close(); throw e; }
      void consume(s);
      release();
      return {turn: {id: s.id}};
    }
    case 'turn/interrupt': { const s = sessions.get(p.threadId); if (s?.q) { s.stopping = true; await s.q.interrupt(); } return {}; }
    case 'thread/unsubscribe': { const s = sessions.get(p.threadId); if (s) { s.stopping = true; s.q?.close(); sessions.delete(p.threadId); } return {}; }
    default: throw new Error('Unsupported Claude bridge request: ' + method);
  }
}
const input = readline.createInterface({input: process.stdin});
input.on('line', line => {
  let m; try { m = JSON.parse(line); } catch { return; }
  if (!m.method) { const cb = pending.get(m.id); if (cb) { pending.delete(m.id); m.error ? cb.reject(new Error(m.error.message)) : cb.resolve(m.result); } return; }
  if (m.id === undefined) return;
  handle(m.method, m.params || {}).then(result => write({id: m.id, result}), e => write({id: m.id, error: {code: -32000, message: String(e.message || e)}}));
});
input.on('close', () => { for (const s of sessions.values()) s.q?.close(); process.exit(0); });
process.on('SIGTERM', () => { for (const s of sessions.values()) s.q?.close(); process.exit(0); });
