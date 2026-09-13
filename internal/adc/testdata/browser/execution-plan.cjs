const {chromium}=require(process.env.ADC_PLAYWRIGHT_MODULE || 'playwright');
const path=require('node:path'),fs=require('node:fs');
const [base]=process.argv.slice(2);
(async()=>{
 const browser=await chromium.launch({headless:true});
 try{
 const context=await browser.newContext({viewport:{width:1440,height:1100}});
 await context.addCookies([{name:'adc_session',value:'transcript-fixture',url:base}]);
 const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(base+'/task?org=org&id=task');
 await page.locator('#plan-details > summary').click();
 await page.getByRole('button',{name:'Start plan within assignment scope'}).click();
 await page.locator('#plan-details > summary').click();
 await page.request.get(base+'/fixture-plan-dispatch');
 await page.locator('#plan-step-R1').getByRole('link',{name:'Worker transcript →',exact:true}).waitFor();
 if(await page.locator('#plan-step-R2 .plan-links').count())throw Error('Prerequisite bypassed');
 await page.locator('#plan-brief-R1 > summary').click();
 await page.getByLabel('Message or steering').fill('A question I am still composing');
 await page.request.get(base+'/fixture-plan-review');
 await page.locator('#plan-step-R2').getByRole('link',{name:'Worker transcript →',exact:true}).waitFor();
 if(await page.locator('#plan-brief-R1').getAttribute('open')===null)throw Error('Live update collapsed inspected step');
 if(await page.getByLabel('Message or steering').inputValue()!=='A question I am still composing')throw Error('Live plan erased steering');
 if(await page.locator('#plan-step-R1 .badge').textContent()!=='complete')throw Error('Reviewed completion hidden');
 const dir=path.resolve(__dirname,'../../../../work');fs.mkdirSync(dir,{recursive:true});
 await page.evaluate(()=>window.scrollTo(0,0));
 await page.screenshot({path:path.join(dir,'ui-execution-plan-desktop.png'),fullPage:true});
 await page.setViewportSize({width:390,height:844});
 if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Phone plan overflows');
 if(await page.locator('.plan-steps').evaluate(el=>el.clientHeight>innerHeight*.49))throw Error('Plan scroll unbounded');
 await page.screenshot({path:path.join(dir,'ui-execution-plan-mobile.png'),fullPage:true});
 await page.locator('#plan-step-R2').getByRole('link',{name:'Worker transcript →',exact:true}).click();
 await page.locator('#run-header').waitFor();
 if(errors.length)throw Error(errors.join('\n'));
 console.log('PASS: draft activation, live dependency progression, review/run links, preserved details/steering and bounded phone layout');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
