(() => {
 const saved = localStorage.getItem('adc-theme');
 if (saved === 'dark' || saved === 'light') document.documentElement.dataset.theme = saved;
 document.addEventListener('click', event => {
  const opener = event.target.closest('[data-open]');
  if (opener) document.getElementById(opener.dataset.open)?.showModal();
  if (event.target.closest('[data-close]')) event.target.closest('dialog')?.close();
  if (event.target.closest('#theme')) {
   const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
   document.documentElement.dataset.theme = next;
   localStorage.setItem('adc-theme', next);
  }
 });
 let passage = '';
 document.addEventListener('selectionchange', () => {
  const selection = window.getSelection();
  const article = document.getElementById('document');
  const button = document.getElementById('discuss-selection');
  if (!article || !button || !selection) return;
  const inside = article.contains(selection.anchorNode) && article.contains(selection.focusNode);
  if (inside && selection.toString().trim()) { passage = selection.toString(); button.hidden = false; }
 });
 document.getElementById('discuss-selection')?.addEventListener('click', () => {
  const article = document.getElementById('document');
  document.getElementById('context-document').value = article.dataset.document;
  document.getElementById('context-revision').value = article.dataset.revision;
  document.getElementById('context-selection').value = passage;
  const preview = document.getElementById('selection-preview');
  preview.textContent = passage; preview.hidden = false;
  document.getElementById('message').focus();
  document.getElementById('discuss-selection').hidden = true;
 });
})();

// Account setup commands contain paths, never credentials.
document.addEventListener('click', async event => {
  const button = event.target.closest('[data-copy]');
  if (!button) return;
  const source = document.getElementById(button.dataset.copy);
  if (!source) return;
  try { await navigator.clipboard.writeText(source.textContent); button.textContent = 'Copied'; }
  catch { button.textContent = 'Select the command to copy'; }
});

// The graph is a projection of ADC's persisted plan. Selection/viewport belong
// to the human and survive server-rendered status patches without altering work.
(() => {
 const views = new Map();
 let pending = false;
 const root = () => document.querySelector('[data-plan-map]');
 function state(map) {
  if (!views.has(map.dataset.planId)) views.set(map.dataset.planId,{scale:0.85,key:'',x:0,y:0,initialized:false});
  return views.get(map.dataset.planId);
 }
 function paint() {
  const map=root(); if(!map)return;
  const s=state(map), svg=map.querySelector('svg'), viewport=map.querySelector('.plan-map-viewport');
  if(!svg||!viewport)return;
  svg.setAttribute('width',Number(svg.dataset.width)*s.scale);
  svg.setAttribute('height',Number(svg.dataset.height)*s.scale);
  const nodes=[...map.querySelectorAll('[data-plan-node]')];
  if(s.key&&!nodes.some(n=>n.dataset.planNode===s.key))s.key='';
  const full=!!map.closest('.plan-page');
  if(!s.hashHandled){s.hashHandled=true;const key=location.hash.startsWith('#plan-step-')?location.hash.slice(11):'';if(nodes.some(n=>n.dataset.planNode===key)){s.key=key;s.initialized=true;document.getElementById('plan-details').open=true;}}
  if(full&&!s.initialized) { s.initialized=true; s.key=(nodes.find(n=>n.classList.contains('running')||n.classList.contains('review'))||nodes.find(n=>n.classList.contains('blocked'))||nodes[0])?.dataset.planNode||''; }
  const upstream=new Set([s.key]),downstream=new Set([s.key]);
  const edges=[...map.querySelectorAll('[data-from]')];
  for(let i=0;i<nodes.length;i++) for(const e of edges){
   if(upstream.has(e.dataset.to))upstream.add(e.dataset.from);
   if(downstream.has(e.dataset.from))downstream.add(e.dataset.to);
  }
  for(const n of nodes){const k=n.dataset.planNode;n.classList.toggle('selected',k===s.key);n.classList.toggle('dimmed',!!s.key&&!upstream.has(k)&&!downstream.has(k));n.setAttribute('aria-current',k===s.key?'step':'false');}
  for(const e of edges){const related=(upstream.has(e.dataset.from)&&upstream.has(e.dataset.to))||(downstream.has(e.dataset.from)&&downstream.has(e.dataset.to));e.classList.toggle('highlight',!!s.key&&related);e.classList.toggle('dimmed',!!s.key&&!related);}
  map.querySelector('[data-plan-select]').value=s.key;
  const section=map.closest('#execution-plan');
  for(const li of section.querySelectorAll('.plan-steps > li')){li.hidden=full&&!!s.key&&li.id!=='plan-step-'+s.key;li.classList.toggle('selected-step',li.id==='plan-step-'+s.key);}
  viewport.scrollLeft=s.x;viewport.scrollTop=s.y;
 }
 function schedule(){if(!pending){pending=true;requestAnimationFrame(()=>{pending=false;paint()})}}
 document.addEventListener('click',event=>{
  const map=root();if(!map)return;const s=state(map),vp=map.querySelector('.plan-map-viewport');
  const node=event.target.closest('[data-plan-node]');
  const dependency=event.target.closest('.plan-dependencies a');
  if(node||dependency){
   event.preventDefault();s.key=node?node.dataset.planNode:dependency.hash.slice('#plan-step-'.length);
   const details=document.getElementById('plan-details');details.open=true;paint();
   const li=document.getElementById('plan-step-'+s.key);
   if(li){li.scrollIntoView({block:'nearest'});const summary=li.querySelector('summary');summary?.focus({preventScroll:true});}
  }
  const zoom=event.target.closest('[data-plan-zoom]');
  if(zoom){const old=s.scale,svg=map.querySelector('svg');
   switch(zoom.dataset.planZoom){case 'in':s.scale=Math.min(1.6,s.scale*1.25);break;case 'out':s.scale=Math.max(.12,s.scale/1.25);break;case 'reset':s.scale=1;break;case 'fit':s.scale=Math.max(.08,Math.min(1,(vp.clientWidth-12)/Number(svg.dataset.width),(vp.clientHeight-12)/Number(svg.dataset.height)));break;}
   s.x=Math.max(0,(vp.scrollLeft+vp.clientWidth/2)*s.scale/old-vp.clientWidth/2);s.y=Math.max(0,(vp.scrollTop+vp.clientHeight/2)*s.scale/old-vp.clientHeight/2);paint();
  }
  if(event.target.closest('[data-plan-clear]')){s.key='';map.querySelectorAll('.dimmed,.highlight,.selected').forEach(n=>n.classList.remove('dimmed','highlight','selected'));map.querySelectorAll('[data-plan-node]').forEach(n=>n.setAttribute('aria-current','false'));document.querySelectorAll('#plan-details .plan-steps > li').forEach(n=>{n.hidden=false;n.classList.remove('selected-step')});}
 });
 document.addEventListener('change',event=>{if(!event.target.matches('[data-plan-select]'))return;const map=root(),s=state(map),vp=map.querySelector('.plan-map-viewport');s.key=event.target.value;s.initialized=true;const node=[...map.querySelectorAll('[data-plan-node]')].find(n=>n.dataset.planNode===s.key);if(node){const m=node.transform.baseVal.consolidate().matrix;s.x=Math.max(0,(m.e+120)*s.scale-vp.clientWidth/2);s.y=Math.max(0,(m.f+60)*s.scale-vp.clientHeight/2);document.getElementById('plan-details').open=true;}paint();});
 document.addEventListener('scroll',event=>{if(event.target.matches?.('.plan-map-viewport')){const s=state(event.target.closest('[data-plan-map]'));s.x=event.target.scrollLeft;s.y=event.target.scrollTop}},true);
 new MutationObserver(schedule).observe(document.body,{childList:true,subtree:true});
 schedule();
})();

// Notes are optional for a decision, required when requesting a revision.
// Server validation remains authoritative, including clients without JavaScript.
document.addEventListener('submit', event => {
 const form=event.target;
 if(!form.matches('[data-decision-form]'))return;
 const notes=form.elements.answer;
 if(event.submitter?.value==='refine'&&!notes.value.trim()) {
  event.preventDefault();form.querySelector('.decision-notes').open=true;notes.setCustomValidity('Describe what you would like changed.');notes.reportValidity();notes.focus();
 }
});
document.addEventListener('input', event=>{
 if(event.target.matches('[data-decision-form] textarea'))event.target.setCustomValidity('');
});
document.addEventListener('click', event=>{
 const button=event.target.closest('[data-decision-form] button[name="outcome"]');
 if(button)button.form.elements.answer.setCustomValidity('');
});
