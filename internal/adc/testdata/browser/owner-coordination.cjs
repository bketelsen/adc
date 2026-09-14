const {chromium}=require(process.env.ADC_PLAYWRIGHT_MODULE || 'playwright');
const path=require('node:path'),fs=require('node:fs');
(async()=>{
 const browser=await chromium.launch({headless:true});
 try{
  const base=process.argv[2],context=await browser.newContext({viewport:{width:1280,height:960}});
  await context.addCookies([{name:'adc_session',value:'coordination-fixture',url:base}]);
  const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/coordination?org=org');
  await page.getByRole('heading',{name:'Owner coordination',exact:true}).waitFor();
  const request=page.locator('section[id^="request-"]');
  await request.getByRole('heading',{name:'Confirm backup coverage'}).waitFor();
  await request.getByText('Waiting for an authorized receiving work context',{exact:false}).waitFor();
  if(await request.locator('details').first().getAttribute('open')!==null)throw Error('Evidence starts expanded');
  await request.getByText('Request, evidence and accountability',{exact:true}).click();
  await request.getByText('Which hosting volumes have observed backup coverage?',{exact:true}).waitFor();
  const dir=path.resolve(__dirname,'../../../../work');fs.mkdirSync(dir,{recursive:true});
  await page.screenshot({path:path.join(dir,'ui-owner-coordination-desktop.png'),fullPage:true});
  await page.setViewportSize({width:390,height:844});
  if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Phone overflow');
  await page.screenshot({path:path.join(dir,'ui-owner-coordination-mobile.png'),fullPage:true});
  await request.locator('summary').filter({hasText:/^Cancel request$/}).click();
  await request.getByLabel('Reason',{exact:true}).fill('No longer needed; preserve unrelated work.');
  await request.getByRole('button',{name:'Cancel request',exact:true}).click();
  await request.getByText('cancelled',{exact:true}).waitFor();
  const response=await page.request.post(base+'/owner-request-action?org=org',{form:{action:'cancel',id:'forged'}});
  if(response.status()!==403)throw Error('Missing CSRF accepted');
  if(errors.length)throw Error(errors.join('\n'));
  console.log('PASS: accountable owner queue, collapsed evidence, desktop/phone layout, cancellation and CSRF');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
