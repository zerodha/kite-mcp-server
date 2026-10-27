import { build } from 'esbuild';
import { readFile, readdir, writeFile } from 'node:fs/promises';
import { resolve, join } from 'node:path';
const root = new URL('./', import.meta.url);
const result = await build({
  entryPoints: [new URL('src/app.js', root).pathname], bundle: true, write: false,
  format: 'iife', platform: 'browser', target: 'es2022', minify: true,
  legalComments: 'inline', metafile: true,
});
// Include the licenses of every bundled package in the distributed Go asset.
const packages = new Set();
for (const input of Object.keys(result.metafile.inputs)) {
  const at = input.lastIndexOf('node_modules/');
  if (at < 0) continue;
  const parts = input.slice(at + 'node_modules/'.length).split('/');
  const name = parts.slice(0, parts[0].startsWith('@') ? 2 : 1).join('/');
  packages.add(resolve(input.slice(0, at), 'node_modules', name));
}
const notices = [];
for (const directory of [...packages].sort()) {
  const pkg = JSON.parse(await readFile(join(directory, 'package.json'), 'utf8'));
  const files = (await readdir(directory)).filter(name => /^(licen[cs]e|notice)(\..*)?$/i.test(name)).sort();
  if (!files.length) throw new Error(`Missing distribution license for ${pkg.name}`);
  for (const file of files) notices.push(`${pkg.name}@${pkg.version} — ${file}\n${await readFile(join(directory, file), 'utf8')}`);
}
const template = await readFile(new URL('src/index.html', root), 'utf8');
const brandAssets = await readFile(new URL('../../kc/templates/brand_assets.html', root), 'utf8');
const fontFace = brandAssets.match(/@font-face\s*\{[^}]+\}/)?.[0];
if (!fontFace) throw new Error('Missing embedded Open Sans font');
const kiteStyles = await readFile(new URL('src/kite.css', root), 'utf8');
const styledTemplate = template.replace('<!-- APP_STYLES -->', () => `<style>${fontFace}\n${kiteStyles}</style>`);
notices.push(`Open Sans — SIL Open Font License\n${await readFile(new URL('../../kc/templates/OpenSans-OFL.txt', root), 'utf8')}`);
const script = (`/*! Third-party notices\n${notices.join('\n\n').replace(/[ \t]+$/gm, '')}\n*/\n` + result.outputFiles[0].text).replace(/<\/script/gi, '<\\/script');
await writeFile(new URL('app.html', root), styledTemplate.replace('<!-- APP_SCRIPT -->', () => `<script>${script}</script>`));
