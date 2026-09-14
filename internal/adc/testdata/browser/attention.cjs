const {chromium}=require(process.env.ADC_PLAYWRIGHT_MODULE||'playwright');
const fs=require('node:fs'),path=require('node:path');
(async()=>{const browser=await chromium.launch({headless:true});try{
 const base=process.argv[2],context=await browser.newContext({viewport:{width:1280,height:960}});
 await context.addCookies([{name:'adc_session',value:'attention-fixture',url:base}]);const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(base+'/?org=org');await page.getByRole('heading',{name:'Supervisor briefing',exact:true}).waitFor();await page.getByText('Verify container restore',{exact:true}).waitFor();
 const evidence=page.locator('details[id^="brief-evidence-"]');if(await evidence.getAttribute('open')!==null)throw Error('Evidence starts expanded');await evidence.locator('summary').click();
 // Wait for a real live refresh, then ensure the user's disclosure choice survives.
 await page.waitForTimeout(2400);if(await evidence.getAttribute('open')===null)throw Error('Live refresh closed evidence');
 const dir=path.resolve(__dirname,'../../../../work');fs.mkdirSync(dir,{recursive:true});await page.screenshot({path:path.join(dir,'ui-attention-desktop.png'),fullPage:true});
 await page.setViewportSize({width:390,height:844});if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Phone overflow');await page.screenshot({path:path.join(dir,'ui-attention-mobile.png'),fullPage:true});
 await evidence.getByRole('link',{name:'Open assessment and transcripts →'}).click();await page.locator('#assessment-details summary').click();await page.getByText('Independently reviewed',{exact:false}).first().waitFor();await page.getByRole('link',{name:'Owner transcript →'}).waitFor();
 if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Assessment phone overflow');if(errors.length)throw Error(errors.join('\n'));
 console.log('PASS: current blocker before reviewed narrative, collapsed evidence survives refresh, owner drilldown and desktop/phone layout');
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exitCode=1});
