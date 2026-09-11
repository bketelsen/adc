// Synthetic, read-only stdio MCP fixture. No real infrastructure access.
const readline=require('node:readline');
const lines=readline.createInterface({input:process.stdin});
lines.on('line',line=>{
 let m;try{m=JSON.parse(line);}catch{return;}
 if(m.id===undefined)return;
 let result;
 if(m.method==='initialize')result={protocolVersion:m.params.protocolVersion,capabilities:{tools:{}},serverInfo:{name:'adc-inventory-fixture',version:'1.0.0'}};
 else if(m.method==='ping')result={};
 else if(m.method==='tools/list')result={tools:[{name:'inventory',description:'Read the synthetic inventory used to qualify ADC MCP transport. No real servers.',inputSchema:{type:'object',properties:{},additionalProperties:false}}]};
 else if(m.method==='tools/call'&&m.params.name==='inventory')result={content:[{type:'text',text:JSON.stringify({fixture:true,inventory_id:process.env.ADC_FIXTURE_MARKER,servers:[{name:'fixture-nas',state:'healthy'}]})}]};
 else {process.stdout.write(JSON.stringify({jsonrpc:'2.0',id:m.id,error:{code:-32601,message:'Method not found'}})+'\n');return;}
 process.stdout.write(JSON.stringify({jsonrpc:'2.0',id:m.id,result})+'\n');
});
