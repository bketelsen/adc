const {chromium}=require(process.env.ADC_PLAYWRIGHT_MODULE || 'playwright');
const path=require('node:path'),fs=require('node:fs');
(async()=>{
 const browser=await chromium.launch({headless:true});
 try {
  const base=process.argv[2],context=await browser.newContext({viewport:{width:1280,height:960}});
  await context.addCookies([{name:'adc_session',value:'transcript-fixture',url:base}]);
  const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/areas?org=org');
  await page.getByRole('heading',{name:'Areas of responsibility'}).waitFor();
  const area=page.locator('section[id^="area-"]');
  await area.getByText('Confirm the agreed outcome',{exact:true}).waitFor();
  await area.getByText('Edit intent',{exact:true}).click();
  await area.getByLabel('Intent and boundaries').fill('Keep the family responsibility current.');
  await area.getByRole('button',{name:'Save intent'}).click();
  await page.getByText('Keep the family responsibility current.',{exact:true}).first().waitFor();
  await area.locator('.area-prose').last().evaluate(el=>{const range=document.createRange();range.selectNodeContents(el);const selection=window.getSelection();selection.removeAllRanges();selection.addRange(range);document.dispatchEvent(new Event('selectionchange'));});
  await area.getByRole('button',{name:'Discuss selected passage'}).click();
  await area.getByLabel('Your note').fill('Tuesday is correct. Please retain that correction.');
  await area.getByRole('button',{name:'Save note',exact:true}).click();
  await area.getByText('Discussion and corrections',{exact:false}).click();
  await area.getByText('Tuesday is correct. Please retain that correction.',{exact:true}).waitFor();
  const dir=path.resolve(__dirname,'../../../../work');fs.mkdirSync(dir,{recursive:true});
  await page.screenshot({path:path.join(dir,'ui-ownership-desktop.png'),fullPage:true});
  await page.setViewportSize({width:390,height:844});
  if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Phone overflow');
  await page.screenshot({path:path.join(dir,'ui-ownership-mobile.png'),fullPage:true});
  const response=await page.request.post(base+'/area-action?org=org',{form:{name:'Forged',owner:'boss',intent:'No CSRF'}});
  if(response.status()!==403)throw Error('Missing CSRF accepted');
  if(errors.length)throw Error(errors.join('\n'));
  console.log('PASS: shared area view, intent correction, desktop/phone layout, cancellation and CSRF enforcement');
 } finally {await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
