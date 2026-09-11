const {chromium}=require(process.env.ADC_PLAYWRIGHT_MODULE||'playwright');
const path=require('node:path');
const [base]=process.argv.slice(2);
(async()=>{
 const browser=await chromium.launch({headless:true});
 try {
  const page=await browser.newPage({viewport:{width:1440,height:1000}});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/login');
  await page.getByLabel('Username',{exact:true}).fill('fixture');
  await page.getByLabel('Password',{exact:true}).fill('ui-fixture-password');
  await page.getByRole('button',{name:'Sign in'}).click();
  await page.goto(base+'/permissions?org=org');
  await page.getByRole('heading',{name:'Useful access, clear limits.'}).waitFor();
  await page.getByLabel('Execution',{exact:true}).selectOption('protected');
  await page.getByRole('button',{name:'Save default',exact:true}).click();
  const request=page.locator('.permission-requests article');
  await request.getByText('Edit the proposed restrictions',{exact:true}).click();
  await request.locator('textarea[name="constraints-0"]').fill(JSON.stringify([{Pointer:'/target',Allowed:['fixture-reviewed']}]));
  await request.getByRole('button',{name:'Save edits for review',exact:true}).click();
  await page.getByText('ACCESS REQUEST · REVISION 2',{exact:true}).waitFor();
  await page.screenshot({path:path.resolve(__dirname,'../../../../work/ui-permissions-desktop.png'),fullPage:true});
  await page.setViewportSize({width:390,height:844});
  if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Permissions page overflows phone');
  await page.screenshot({path:path.resolve(__dirname,'../../../../work/ui-permissions-mobile.png'),fullPage:true});
  await page.getByLabel('Approve access for',{exact:true}).selectOption('assignment');
  await page.getByRole('button',{name:'Record decision',exact:true}).click();
  await page.getByText('No access requests are waiting for a decision.',{exact:true}).waitFor();
  await page.getByLabel('Select inventory',{exact:true}).check();
  const bulk=page.locator('#permission-bulk');
  await bulk.getByLabel('Access',{exact:true}).selectOption('allow');
  await bulk.getByLabel('Capability',{exact:true}).selectOption('read');
  await bulk.getByRole('button',{name:'Apply to selected tools',exact:true}).click();
  await page.locator('.permission-tools summary').filter({hasText:'allow · read'}).waitFor();
  if(errors.length)throw Error(errors.join('\n'));
  console.log(JSON.stringify({defaultSaved:true,editCreatesRevision:true,bundledApproval:true,bulkClassification:true,mobileOverflow:false,browserErrors:errors}));
 } finally {await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
