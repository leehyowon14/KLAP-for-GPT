import { build } from 'esbuild';
import { mkdir, writeFile } from 'node:fs/promises';

const bundle = await build({entryPoints:['src/main.tsx'], bundle:true, minify:true, write:false, outdir:'dist', format:'iife', target:'es2022', define:{'process.env.NODE_ENV':'"production"'}, legalComments:'none'});
const js = bundle.outputFiles.find(f => f.path.endsWith('.js')).text.replace(/<\/script/gi, '<\\/script');
const css = bundle.outputFiles.find(f => f.path.endsWith('.css'))?.text || '';
const html = `<!doctype html><html lang="ko"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="color-scheme" content="light dark"><title>KLAP · 나의 캠퍼스</title><style>${css}</style></head><body><div id="root"></div><script>${js}</script></body></html>`;
await mkdir('dist', {recursive:true});
await writeFile('dist/index.html', html);
console.log(`Built self-contained MCP App (${Math.round(Buffer.byteLength(html)/1024)} KiB)`);
