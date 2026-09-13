const {chromium}=require(process.env.ADC_PLAYWRIGHT_MODULE || 'playwright');
const path=require('node:path'),fs=require('node:fs');
(async()=>{
 const browser=await chromium.launch({headless:true});
 try {
 const base=process.argv[2],context=await browser.newContext({viewport:{width:1440,height:1100}});
 await context.addCookies([{name:'adc_session',value:'transcript-fixture',url:base}]);
 const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(base+'/task?org=org&id=task');
 await page.locator('#plan-details > summary').click();
 const detail=page.locator('#milestone-R1-gate');await detail.locator(':scope > summary').click();
 await detail.getByText('required external conditions are not yet met',{exact:true}).waitFor();
 await detail.getByText('Latest observation',{exact:true}).click();
 await page.getByLabel('Message or steering').fill('Still composing my question');
 if(await page.locator('#plan-step-R2 .plan-links').count())throw Error('Waiting condition bypassed');
 await page.request.get(base+'/fixture-observe');
 await detail.getByText('Automatic wait: satisfied',{exact:true}).waitFor();
 if(await detail.locator('[id^="wait-result-"]').getAttribute('open')===null)throw Error('Live result collapsed observation');
 if(await page.getByLabel('Message or steering').inputValue()!=='Still composing my question')throw Error('Live observation erased steering');
 if(await page.locator('#plan-step-R2 .plan-links').count())throw Error('Automatic observation bypassed review');
 await page.request.get(base+'/fixture-review');await page.locator('#plan-step-R2').getByRole('link',{name:'Worker transcript →',exact:true}).waitFor();
 const dir=path.resolve(__dirname,'../../../../work');fs.mkdirSync(dir,{recursive:true});
 await page.evaluate(()=>window.scrollTo(0,0));await page.screenshot({path:path.join(dir,'ui-waits-desktop.png'),fullPage:true});
 await page.setViewportSize({width:390,height:844});
 if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Phone observer overflows');
 if(await page.locator('.plan-steps').evaluate(el=>el.clientHeight>innerHeight*.49))throw Error('Observation panel unbounded');
 await page.screenshot({path:path.join(dir,'ui-waits-mobile.png'),fullPage:true});
 if(errors.length)throw Error(errors.join('\n'));
 console.log('PASS: live MCP observation, waiting reason, deadline and evidence, preserved disclosure/steering, review gate and phone layout');
 } finally {await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
