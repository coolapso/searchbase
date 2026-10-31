(() => {
  const toggle = document.querySelector('.nav-toggle');
  const sidebar = document.querySelector('.sidebar');
  if (!toggle || !sidebar) return;
  function close() { sidebar.classList.remove('open'); toggle.setAttribute('aria-expanded', 'false'); }
  toggle.addEventListener('click', () => {
    toggle.setAttribute('aria-expanded', String(sidebar.classList.toggle('open')));
  });
  document.addEventListener('keydown', event => { if (event.key === 'Escape' && sidebar.classList.contains('open')) { close(); toggle.focus(); } });
  sidebar.addEventListener('click', event => { if (event.target.closest('a')) close(); });
})();
