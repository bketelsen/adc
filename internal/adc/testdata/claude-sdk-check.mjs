// Real SDK/native initialization and MCP handshake; deliberately no user turn.
import {pathToFileURL} from 'node:url';
import {createRequire} from 'node:module';
const [sdkPath, cliPath, cwd] = process.argv.slice(2);
const {query} = await import(pathToFileURL(sdkPath));
const require = createRequire(sdkPath);
const {McpServer} = require('@modelcontextprotocol/sdk/server/mcp.js');
const {ListToolsRequestSchema, CallToolRequestSchema} = require('@modelcontextprotocol/sdk/types.js');
const instance = new McpServer({name:'adc',version:'1'}, {capabilities:{tools:{}}});
instance.server.setRequestHandler(ListToolsRequestSchema, async () => ({tools:[{name:'adc_fixture',description:'Never called by this no-model check',inputSchema:{type:'object',properties:{}}}]}));
instance.server.setRequestHandler(CallToolRequestSchema, async () => {throw Error('Unexpected model tool call');});
let release; const pending = new Promise(r => release=r);
const q=query({prompt:(async function*(){await pending;})(), options:{cwd,env:{...process.env},pathToClaudeCodeExecutable:cliPath,settingSources:[],plugins:[],skills:[],persistSession:false,strictMcpConfig:true,tools:[],mcpServers:{adc:{type:'sdk',name:'adc',instance}}}});
try {
 const models=await q.supportedModels();
 let states=[];
 for(let i=0;i<30;i++) {states=await q.mcpServerStatus();if(states.some(s=>s.name==='adc'&&s.status==='connected'))break;await new Promise(r=>setTimeout(r,100));}
 if(!states.some(s=>s.name==='adc'&&s.status==='connected'))throw Error('SDK MCP handshake failed: '+JSON.stringify(states));
 console.log(JSON.stringify({nativeSDKInitialized:true,SDKMCPConnected:true,modelMetadataRows:models.length,modelTurns:0}));
} finally {release();q.close();}
