import fs from 'node:fs';
const designs = JSON.parse(fs.readFileSync(new URL('./logo-designs.json', import.meta.url)));
const out = new URL('../resources/logo-concepts/', import.meta.url);
fs.mkdirSync(out, {recursive: true});
const cards = designs.map(([name, body], i) => {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 96 96" fill="none" color="#5260d8" stroke="currentColor" stroke-width="4.5" stroke-linejoin="round" stroke-linecap="round">${body}</svg>`;
  fs.writeFileSync(new URL(`${String(i+1).padStart(2,'0')}.svg`, out), svg);
  return `<article>${svg}<b>${String(i+1).padStart(2,'0')} · ${name}</b><div class="small">${svg}${svg}</div></article>`;
});
fs.writeFileSync(new URL('gallery.html',out), `<!doctype html><html><meta charset="utf-8"><style>body{font:14px system-ui;color:#20293e;background:#f1f3f8;margin:24px}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:16px}article{background:white;border:1px solid #dce2ed;border-radius:12px;padding:24px;text-align:center}svg{display:block;width:96px;margin:0 auto 20px}b{display:block}.small{display:flex;justify-content:center;gap:20px;margin-top:20px}.small svg{width:24px;margin:0}.small svg+svg{width:16px}</style><div class="grid">${cards.join('')}</div></html>`);
console.log('Created ten SVG concepts and gallery');
