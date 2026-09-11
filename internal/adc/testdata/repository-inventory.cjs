// Synthetic read-only MCP. The public inventory ID derives from a private seed
// so tests can prove transport worked without copying credentials into evidence.
const crypto = require('node:crypto');
const readline = require('node:readline');
readline.createInterface({input: process.stdin}).on('line', line => {
  let m; try { m = JSON.parse(line); } catch { return; }
  if (m.id === undefined) return;
  let result;
  if (m.method === 'initialize') result = {protocolVersion: m.params.protocolVersion, capabilities: {tools: {}}, serverInfo: {name: 'adc-repository-inventory', version: '1.0.0'}};
  else if (m.method === 'ping') result = {};
  else if (m.method === 'tools/list') result = {tools: [{name: 'inventory', description: 'Read synthetic repository suite inventory. No real infrastructure.', inputSchema: {type: 'object', properties: {}, additionalProperties: false}}]};
  else if (m.method === 'tools/call' && m.params.name === 'inventory') result = {content: [{type: 'text', text: JSON.stringify({fixture: true, inventory_id: crypto.createHash('sha256').update(process.env.ADC_FIXTURE_SEED).digest('hex'), suites: ['trixie', 'forky']})}]};
  else { process.stdout.write(JSON.stringify({jsonrpc: '2.0', id: m.id, error: {code: -32601, message: 'Method not found'}}) + '\n'); return; }
  process.stdout.write(JSON.stringify({jsonrpc: '2.0', id: m.id, result}) + '\n');
});
