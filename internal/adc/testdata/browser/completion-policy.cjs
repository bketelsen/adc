const {chromium}=require(process.env.ADC_PLAYWRIGHT_MODULE||'playwright');
const fs=require('node:fs'),path=require('node:path');
(async()=>{const browser=await chromium.launch({headless:true});try{
 const [base,steward,task]=process.argv.slice(2),context=await browser.newContext({viewport:{width:1280,height:960}});await context.addCookies([{name:'adc_session',value:'completion-fixture',url:base}]);
 const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));await page.goto(base+'/stewards?org=org');const section=page.locator('#steward-'+steward);
 await section.getByText('Edit charter',{exact:true}).click();const mode=section.getByLabel('Completion for new work');if(await mode.inputValue()!=='routine')throw Error('Policy not shown');
 const dir=path.resolve(__dirname,'../../../../work');fs.mkdirSync(dir,{recursive:true});await page.screenshot({path:path.join(dir,'ui-completion-desktop.png'),fullPage:true});
 await page.setViewportSize({width:390,height:844});if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Phone overflow');await page.screenshot({path:path.join(dir,'ui-completion-mobile.png'),fullPage:true});
 await mode.selectOption('reviewed');await section.getByRole('button',{name:'Save charter',exact:true}).click();await page.goto(base+'/task?org=org&id='+task);await page.getByText('Routine · observed evidence',{exact:true}).waitFor();
 const rejected=await page.request.post(base+'/steward-action?org=org',{form:{agent:steward,revision:'2',action:'charter',charter:'x',completion_mode:'routine'}});if(rejected.status()!==403)throw Error('Policy edit bypassed CSRF');if(errors.length)throw Error(errors.join('\n'));
 console.log('PASS: explicit policy edit, active work keeps routine snapshot, desktop/phone layout and CSRF');
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exitCode=1});
