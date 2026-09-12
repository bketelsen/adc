const {chromium}=require(process.env.ADC_PLAYWRIGHT_MODULE || 'playwright');
const path=require('node:path'),fs=require('node:fs');
(async()=>{
 const browser=await chromium.launch({headless:true});
 try{
 const base=process.argv[2],context=await browser.newContext({viewport:{width:1440,height:1100}});
 await context.addCookies([{name:'adc_session',value:'transcript-fixture',url:base}]);
 const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(base+'/task?org=org&id=task');
 const detail=page.locator('#integration-R1');await detail.locator(':scope > summary').click();
 await detail.getByText('fixture:producer',{exact:true}).waitFor();
 const check=page.locator('#validation-R1-unit');await check.locator(':scope > summary').click();
 await check.getByText('Fixture verifier output',{exact:true}).waitFor();
 await page.getByLabel('Message or steering').fill('My unfinished question');
 await page.request.get(base+'/fixture-change');
 await check.locator(':scope > summary').filter({hasText:'stale'}).waitFor();
 if(await detail.getAttribute('open')===null || await check.getAttribute('open')===null)throw Error('Live update collapsed evidence');
 if(await page.getByLabel('Message or steering').inputValue()!=='My unfinished question')throw Error('Steering lost');
 const dir=path.resolve(__dirname,'../../../../work');fs.mkdirSync(dir,{recursive:true});
 await page.evaluate(()=>scrollTo(0,0));await page.screenshot({path:path.join(dir,'ui-integration-desktop.png'),fullPage:true});
 await page.setViewportSize({width:390,height:844});
 if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Phone overflows');
 if(await page.locator('.plan-steps').evaluate(el=>el.clientHeight>innerHeight*.49))throw Error('Unbounded evidence list');
 await page.screenshot({path:path.join(dir,'ui-integration-mobile.png'),fullPage:true});
 if(errors.length)throw Error(errors.join('\n'));
 console.log('PASS: repository base/output, reported validation, live stale checks, preserved disclosures/steering and phone layout');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
