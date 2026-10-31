(() => {
  const input = document.querySelector('#search-input');
  const results = document.querySelector('#search-results');
  const status = document.querySelector('#search-status');
  if (!input || !results || !status || !input.dataset.index) return;
  let pending;
  let generation = 0;
  async function loadIndex() {
    if (!pending) pending = fetch(input.dataset.index).then(response => {
      if (!response.ok) throw new Error('Search index unavailable');
      return response.json();
    }).catch(error => { pending = undefined; throw error; });
    return pending;
  }
  input.addEventListener('input', async () => {
    const request = ++generation;
    const query = input.value.trim().toLowerCase();
    results.replaceChildren();
    results.hidden = true;
    status.textContent = '';
    if (query.length < 2) return;
    status.textContent = status.dataset.loading;
    try {
      const pages = await loadIndex();
      if (request !== generation) return;
      const matches = pages.filter(page => `${page.title} ${page.content}`.toLowerCase().includes(query)).slice(0, 6);
      for (const page of matches) {
        const link = document.createElement('a');
        link.href = page.href;
        const title = document.createElement('strong');
        title.textContent = page.title;
        const excerpt = document.createElement('span');
        excerpt.textContent = `${page.content.slice(0, 120)}…`;
        link.append(title, excerpt);
        results.append(link);
      }
      results.hidden = !matches.length;
      status.textContent = matches.length ? '' : status.dataset.empty;
    } catch {
      if (request === generation) status.textContent = status.dataset.error;
    }
  });
  input.addEventListener('keydown', event => {
    if (event.key === 'Escape') { ++generation; results.hidden = true; status.textContent = ''; }
  });
})();
