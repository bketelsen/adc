const {chromium}=require(process.env.ADC_PLAYWRIGHT_MODULE || 'playwright');
const path=require('node:path'),fs=require('node:fs');
(async()=>{
 const browser=await chromium.launch({headless:true});
 try{
 const base=process.argv[2],context=await browser.newContext({viewport:{width:1440,height:1100}});
 await context.addCookies([{name:'adc_session',value:'transcript-fixture',url:base}]);
 const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(base+'/task?org=org&id=task');
 const details=page.locator('#preflight-R1');await details.locator(':scope > summary').click();
 await details.getByText('Fixture reviewer unavailable',{exact:false}).waitFor();
 await page.getByLabel('Message or steering').fill('Keep this question');
 await page.request.get(base+'/fixture-ready');
 await details.getByText('Fixture reviewer restored',{exact:false}).waitFor();
 if(await details.getAttribute('open')===null)throw Error('Readiness update collapsed disclosure');
 if(await page.getByLabel('Message or steering').inputValue()!=='Keep this question')throw Error('Readiness update lost steering');
 const dir=path.resolve(__dirname,'../../../../work');fs.mkdirSync(dir,{recursive:true});
 await page.screenshot({path:path.join(dir,'ui-preflight-desktop.png'),fullPage:true});
 await page.setViewportSize({width:390,height:844});
 if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Phone overflow');
 await page.screenshot({path:path.join(dir,'ui-preflight-mobile.png'),fullPage:true});
 await page.goto(base+'/connections?org=org');
 await page.getByRole('button',{name:'＋ GitHub',exact:true}).click();
 const dialog=page.locator('#new-github-connection');
 await dialog.getByLabel('Name',{exact:true}).fill('Fixture GitHub');
 await dialog.getByLabel('Allowed repositories').fill('fixture/project');
 await dialog.getByLabel('GitHub access token').fill('synthetic-browser-token');
 if(await dialog.getByLabel('GitHub access token').getAttribute('type')!=='password')throw Error('Token not masked');
 await page.screenshot({path:path.join(dir,'ui-github-connection-mobile.png'),fullPage:true});
 await dialog.getByRole('button',{name:'Save GitHub connection'}).click();
 await page.getByText('Fixture GitHub',{exact:true}).waitFor();
 if((await page.content()).includes('synthetic-browser-token'))throw Error('Token echoed in page');
 if(errors.length)throw Error(errors.join('\n'));
 console.log('PASS: preflight recovery disclosure, preserved steering, phone layout, scoped GitHub form, masked token and no credential echo');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
