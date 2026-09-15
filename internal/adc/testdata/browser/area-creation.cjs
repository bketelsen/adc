const {chromium}=require(process.env.ADC_PLAYWRIGHT_MODULE||'playwright');
(async()=>{
 const browser=await chromium.launch({headless:true});
 try {
  const base=process.argv[2],context=await browser.newContext({viewport:{width:1280,height:960}});
  await context.addCookies([{name:'adc_session',value:'transcript-fixture',url:base}]);
  const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/areas?org=org');
  await page.locator('#create-area > summary').click();
  await page.getByLabel('What should this area cover?').fill('Help me define responsibility for backups.');
  await page.getByLabel('Model for the area designer').fill('gpt-5.6-sol');
  await page.setViewportSize({width:390,height:844});
  if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Phone overflow');
  await page.getByRole('button',{name:'Discuss this area →',exact:true}).click();
  await page.waitForURL(/\/task\?/);
  if(!(await page.request.get(base+'/fixture-area-proposal')).ok())throw Error('Fixture proposal failed');
  const card=page.locator('#decisionlist > article').first();
  await card.getByText('Fixture backups',{exact:true}).waitFor();
  if(await card.getByText('Draft PR authorization',{exact:true}).count())throw Error('Unrelated publication control');
  await card.getByRole('button',{name:'Refine with notes',exact:true}).click();
  await card.getByLabel('Notes').fill('Use recovery as the name instead.');
  await card.getByRole('button',{name:'Refine with notes',exact:true}).click();
  await page.waitForFunction(()=>!document.querySelector('#decisionlist > article'));
  if(!(await page.request.get(base+'/fixture-area-proposal')).ok())throw Error('Fixture revision failed');
  await card.getByText('Fixture recovery',{exact:true}).waitFor();
  await card.getByRole('button',{name:'Approve',exact:true}).click();
  await page.waitForURL(/\/areas\?/);
  await page.getByRole('heading',{name:'Fixture recovery',exact:true}).waitFor();
  if(errors.length)throw Error(errors.join('\n'));
  console.log('PASS: desktop/phone area chat entry, task routing, refinement, approval and saved area; no publication control');
 } finally {await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
