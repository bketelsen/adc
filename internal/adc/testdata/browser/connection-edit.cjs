const {chromium}=require(process.env.ADC_PLAYWRIGHT_MODULE || 'playwright');
const path=require('node:path'),fs=require('node:fs');
(async()=>{
 const browser=await chromium.launch({headless:true});
 try{
 const base=process.argv[2],context=await browser.newContext({viewport:{width:1440,height:1000}});
 await context.addCookies([{name:'adc_session',value:'transcript-fixture',url:base}]);
 const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(base+'/connections?org=org');
 await page.locator('article.connection-row').filter({hasText:'Fixture MCP'}).getByRole('link',{name:'Edit',exact:true}).click();
 await page.getByRole('heading',{name:'Edit connection'}).waitFor();
 if((await page.content()).includes('synthetic-private-key'))throw Error('Saved secret exposed');
 if(await page.getByLabel('Environment changes (JSON)').inputValue()!=='')throw Error('Secret patch prepopulated');
 await page.getByLabel('Name',{exact:true}).fill('Renamed MCP');
 await page.getByLabel('Command',{exact:true}).fill('/bin/echo');
 await page.getByRole('button',{name:'Save changes',exact:true}).click();
 await page.getByText('Renamed MCP · stdio',{exact:true}).waitFor();
 await page.getByText('API_KEY',{exact:true}).waitFor();
 await page.getByLabel('Environment changes (JSON)').fill('{"API_KEY":null,"NEW_KEY":"fixture-replacement"}');
 await page.getByRole('button',{name:'Save changes',exact:true}).click();
 await page.getByText('NEW_KEY',{exact:true}).waitFor();
 if(await page.getByText('API_KEY',{exact:true}).count())throw Error('Removed secret still listed');
 if((await page.content()).includes('fixture-replacement'))throw Error('Replacement echoed');
 const dir=path.resolve(__dirname,'../../../../work');fs.mkdirSync(dir,{recursive:true});
 await page.screenshot({path:path.join(dir,'ui-connection-edit-desktop.png'),fullPage:true});
 await page.setViewportSize({width:390,height:844});
 if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Phone overflow');
 await page.screenshot({path:path.join(dir,'ui-connection-edit-mobile.png'),fullPage:true});
 await page.getByLabel('Header changes (JSON)').fill('{"secret":"never-echo-this",');
 await page.getByRole('button',{name:'Save changes',exact:true}).click();
 await page.getByText('headers must be a JSON object',{exact:false}).waitFor();
 if((await page.content()).includes('never-echo-this'))throw Error('Invalid submitted secret echoed');
 await page.getByRole('button',{name:'Save changes',exact:true}).waitFor();
 if(errors.length)throw Error(errors.join('\n'));
 console.log('PASS: existing connection link, stable identity, blank-key preservation, explicit replacement/removal, private error recovery and desktop/phone layout');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
