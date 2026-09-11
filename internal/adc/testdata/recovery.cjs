// Synthetic non-idempotent action fixture. The first action commits its receipt
// before withholding the response, allowing the test to interrupt ADC precisely.
const fs=require('node:fs');
const readline=require('node:readline');
const lines=readline.createInterface({input:process.stdin});
process.stdin.on('end',()=>process.exit(0));
const receipt=process.env.ADC_RECEIPT_FILE;
const count=()=>fs.existsSync(receipt)?fs.readFileSync(receipt,'utf8').trim().split('\n').filter(Boolean).length:0;
const reply=(id,result)=>process.stdout.write(JSON.stringify({jsonrpc:'2.0',id,result})+'\n');
lines.on('line',line=>{
 let m;try{m=JSON.parse(line);}catch{return;}
 if(m.id===undefined)return;
 if(m.method==='initialize')return reply(m.id,{protocolVersion:m.params.protocolVersion,capabilities:{tools:{}},serverInfo:{name:'adc-recovery-fixture',version:'1.0.0'}});
 if(m.method==='ping')return reply(m.id,{});
 if(m.method==='tools/list')return reply(m.id,{tools:[{name:'inspect_receipt',description:'Inspect the durable receipt count for the isolated synthetic action.',inputSchema:{type:'object',properties:{}}},{name:'append_receipt',description:'Perform a synthetic non-idempotent action. Each call appends another receipt. Inspect first; do not repeat an already completed action.',inputSchema:{type:'object',properties:{}}}]});
 if(m.method==='tools/call'){
  if(m.params.name==='inspect_receipt')return reply(m.id,{content:[{type:'text',text:JSON.stringify({fixture:true,receipt_count:count()})}]});
  if(m.params.name==='append_receipt'){
   fs.appendFileSync(receipt,'fixture-action\n',{mode:0o600});
   return setTimeout(()=>reply(m.id,{content:[{type:'text',text:JSON.stringify({fixture:true,receipt_count:count()})}]}),60000);
  }
 }
 process.stdout.write(JSON.stringify({jsonrpc:'2.0',id:m.id,error:{code:-32601,message:'Method not found'}})+'\n');
});
