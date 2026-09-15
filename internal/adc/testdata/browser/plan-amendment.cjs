const {chromium}=require(process.env.ADC_PLAYWRIGHT_MODULE || 'playwright');
(async()=>{
 const browser=await chromium.launch({headless:true});
 try {
  const base=process.argv[2], context=await browser.newContext({viewport:{width:1280,height:960}});
  await context.addCookies([{name:'adc_session',value:'transcript-fixture',url:base}]);
  const page=await context.newPage(), errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/plan?org=org&id=task&step=R1');
  await page.locator('#milestone-R1-gate > summary').click();
  await page.locator('#remove-requirement-R1-gate > summary').click();
  await page.setViewportSize({width:390,height:844});
  if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Phone overflow');
  const form=page.locator('#remove-requirement-R1-gate form');
  await form.getByLabel('Reason').fill('No longer proportionate to this small project.');
  await form.getByRole('button',{name:'Remove requirement and resume',exact:true}).click();
  await page.waitForFunction(()=>!document.getElementById('milestone-R1-gate'));
  await page.locator('#omit-step-R1 > summary').click();
  const omission=page.locator('#omit-step-R1 form');
  await omission.getByLabel('Reason').fill('Retire the whole no-work exercise.');
  await omission.getByRole('button',{name:'Omit step and continue',exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('#plan-step-R1 .badge')?.textContent==='omitted');
  if(!await page.getByText('1 omitted by human',{exact:true}).count())throw Error('Omission not distinguished from success');
  if(errors.length)throw Error(errors.join('\n'));
  console.log('PASS: human requirement removal on phone, authenticated form, persisted scope change and whole-step omission, no fabricated evidence');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
