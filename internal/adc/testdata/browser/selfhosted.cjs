const {chromium}=require(process.env.ADC_PLAYWRIGHT_MODULE || 'playwright');
const path=require('node:path'),fs=require('node:fs');
(async()=>{
 const browser=await chromium.launch({headless:true});
 try{
 const base=process.argv[2],provider=process.argv[3],context=await browser.newContext({viewport:{width:1440,height:1000}});
 await context.addCookies([{name:'adc_session',value:'transcript-fixture',url:base}]);
 const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(base+'/connections?org=org');
 await page.getByRole('button',{name:'＋ Self-hosted',exact:true}).click();
 const dialog=page.locator('#new-selfhosted-account');
 await dialog.getByLabel('Account label').fill('Fixture Lemonade');
 await dialog.getByLabel('API base URL').fill(provider);
 await dialog.getByLabel('API key (optional)',{exact:true}).fill('synthetic-selfhosted-key');
 if(await dialog.getByLabel('Concurrent active runs').inputValue()!=='1')throw Error('Wrong starting capacity');
 if(await dialog.getByLabel('API key (optional)',{exact:true}).getAttribute('type')!=='password')throw Error('Key not masked');
 await dialog.getByRole('button',{name:'Connect server'}).click();
 await page.getByText('Connected · 1 model available').waitFor();
 await page.getByText('alibaba-qwen',{exact:false}).waitFor();
 if((await page.content()).includes('synthetic-selfhosted-key'))throw Error('API key echoed');
 const dir=path.resolve(__dirname,'../../../../work');fs.mkdirSync(dir,{recursive:true});
 await page.screenshot({path:path.join(dir,'ui-selfhosted-desktop.png'),fullPage:true});
 await page.setViewportSize({width:390,height:844});
 if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Self-hosted phone overflow');
 await page.screenshot({path:path.join(dir,'ui-selfhosted-mobile.png'),fullPage:true});
 await page.getByRole('button',{name:'Save connection'}).click();
 await page.getByText('Connected · 1 model available').waitFor();
 if((await page.content()).includes('synthetic-selfhosted-key'))throw Error('Edit echoed key');
 await page.goto(base+'/team?org=org');await page.getByRole('button',{name:'＋ Add agent',exact:true}).click();
 if(await page.locator('#new-agent select[name="provider"] option[value="selfhosted"]').count()!==1)throw Error('Self-hosted role provider unavailable');
 if(errors.length)throw Error(errors.join('\n'));
 console.log('PASS: personal endpoint creation, discovered family/catalog, sealed masked key, blank-key preservation, role provider option and phone layout');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
