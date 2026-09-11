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
