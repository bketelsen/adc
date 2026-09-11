const {chromium}=require(process.env.ADC_PLAYWRIGHT_MODULE||'playwright');
const path=require('node:path');const output=path.resolve(__dirname,'../../../../work');const [base]=process.argv.slice(2);
(async()=>{const browser=await chromium.launch({headless:true});try{
 const page=await browser.newPage({viewport:{width:1440,height:1000}});const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(base+'/login');await page.getByLabel('Username',{exact:true}).fill('fixture');await page.getByLabel('Password',{exact:true}).fill('ui-fixture-password');await page.getByRole('button',{name:'Sign in'}).click();
 await page.goto(base+'/connections?org=org');await page.getByRole('button',{name:'＋ Codex',exact:true}).click();
 const form=page.locator('#new-codex-account');await form.getByLabel('Account label').fill('Personal Codex');await form.getByRole('button',{name:'Continue to sign-in'}).click();await page.waitForURL(/codex-account/);
 await page.getByRole('button',{name:'Start ChatGPT sign-in'}).click();await page.getByText('FIXT-1234',{exact:true}).waitFor();
 await page.screenshot({path:path.join(output,'ui-codex-login-desktop.png'),fullPage:true});await page.setViewportSize({width:390,height:844});if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Codex connection overflows phone');await page.screenshot({path:path.join(output,'ui-codex-login-mobile.png'),fullPage:true});
 await page.getByRole('link',{name:'Check sign-in',exact:true}).click();await page.getByRole('heading',{name:'Connected',exact:true}).waitFor();await page.getByRole('heading',{name:'Available Codex models'}).waitFor();
 await page.goto(base+'/team?org=org');await page.locator('.agent-card').filter({has:page.getByRole('heading',{name:'Developer',exact:true})}).getByRole('button',{name:'Edit configuration'}).click();
 await page.locator('#edit-dev').getByLabel('Provider',{exact:true}).selectOption('codex');await page.locator('#edit-dev').getByLabel('Model',{exact:true}).fill('gpt-5.6-sol');await page.locator('#edit-dev').getByRole('button',{name:'Save changes'}).click();
 if(errors.length)throw Error(errors.join('\n'));console.log(JSON.stringify({createPersonalAccount:true,deviceCode:true,signInState:true,modelCatalog:true,explicitRoleProvider:true,mobileOverflow:false,browserErrors:errors}));
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exitCode=1});
