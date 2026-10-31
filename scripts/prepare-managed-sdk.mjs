// Maintainer-only: embed the reviewed SDK into each Launch distribution.
// Consumers install Launch; they never run this preparation step.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import {fileURLToPath} from 'node:url';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const source = path.resolve(process.argv[2] || path.join(root, 'examples/vendor/sdk'));
const upstreamVersion = fs.readFileSync(path.join(root, 'sdk/UPSTREAM_VERSION'), 'utf8').trim();
if (fs.readFileSync(path.join(source, 'VERSION'), 'utf8').trim() !== upstreamVersion) throw Error(`Expected reviewed SDK ${upstreamVersion}`);
const required = fs.readFileSync(path.join(root, `sdk/upstream-files-${upstreamVersion}.txt`), 'utf8').trim().split(/\r?\n/);
for (const name of required) {
  if (!fs.existsSync(path.join(source, name))) throw Error(`Missing reviewed SDK component: ${name}`);
}
const records = [];
function copy(from, to, transform = value => value) {
  const input = path.join(source, from), output = path.join(root, to);
  fs.mkdirSync(path.dirname(output), {recursive:true});
  const raw = fs.readFileSync(input), result = transform(raw.toString('utf8')).replace(/\r\n/g, '\n');
  fs.writeFileSync(output, result);
  records.push({source:from, destination:to.replaceAll('\\','/'), source_sha256:crypto.createHash('sha256').update(raw).digest('hex'), sha256:crypto.createHash('sha256').update(result).digest('hex')});
}
function tree(from, to, accept = () => true, transform = value => value) {
  for (const entry of fs.readdirSync(path.join(source, from), {withFileTypes:true})) {
    const rel = `${from}/${entry.name}`, dest = `${to}/${entry.name}`;
    if (entry.isDirectory()) {
      if (!['bin','obj','target','__pycache__','node_modules','.git'].includes(entry.name)) tree(rel,dest,accept,transform);
    } else if (accept(rel)) copy(rel,dest,value=>transform(value,rel));
  }
}
function notices(to) {
  copy('LICENSE',`${to}/LICENSE`); copy('NOTICE',`${to}/NOTICE`); copy('VERSION',`${to}/VERSION`);
}
function nativePortability(value, name) {
  if (name !== 'c/device_display.h' || /#\s*(?:define|ifndef)\s+PROCESSOR_ARCHITECTURE_ARM64\b/.test(value)) return value;
  // Older MinGW headers omit this documented SYSTEM_INFO value:
  // https://learn.microsoft.com/windows/win32/api/sysinfoapi/ns-sysinfoapi-system_info
  return value.replace('#define LICENTIVO_DEVICE_DISPLAY_H', '#define LICENTIVO_DEVICE_DISPLAY_H\n\n#if defined(_WIN32) && !defined(PROCESSOR_ARCHITECTURE_ARM64)\n#define PROCESSOR_ARCHITECTURE_ARM64 12\n#endif');
}
function javaDeviceNameDefault(value, name) {
  if (name !== 'java/src/licentivo/Client.java' || value.includes('cfg.putIfAbsent("device_name","")')) return value;
  // Device names are optional in public product settings. Supply the empty
  // default before the upstream SDK chooses the current computer's name.
  return value.replace('readDisplayDetails();if(Json.text(cfg,"device_name").isBlank())', 'cfg.putIfAbsent("device_name","");readDisplayDetails();if(Json.text(cfg,"device_name").isBlank())');
}
const go = 'pkg/launchclient/internal/licentivo';
for (const name of fs.readdirSync(path.join(source,'go'))) {
  if (name.endsWith('.go') && !name.endsWith('_test.go')) copy(`go/${name}`,`${go}/${name}`,text => text.replaceAll('licentivo/sdk/go/internal/', 'licentivo-launch/pkg/launchclient/internal/licentivo/internal/'));
}
tree('go/internal',`${go}/internal`,name=>name.endsWith('.go') && !name.endsWith('_test.go'));
notices(go);
const py = 'sdk/python/src/licentivo_launch/_vendor';
copy('python/licentivo.py',`${py}/licentivo.py`,text=>text.replace('import licentivo_tasks','from . import licentivo_tasks').replace('import licentivo_logout','from . import licentivo_logout'));
for (const name of fs.readdirSync(path.join(source,'python'))) {
  if (/^licentivo_.+\.py$/.test(name)) copy(`python/${name}`,`${py}/${name}`);
}
notices(py);
fs.writeFileSync(path.join(root,py,'__init__.py'),'"""Private, pinned Licentivo SDK implementation."""\n');
const js = 'sdk/javascript/src/vendor';
for (const name of fs.readdirSync(path.join(source,'javascript'))) {
  if (name.endsWith('.mjs') && !name.includes('test') && name !== 'demo.mjs') copy(`javascript/${name}`,`${js}/${name}`);
}
notices(js);
for (const name of fs.readdirSync(path.join(source,'csharp'))) if (name.endsWith('.cs')) copy(`csharp/${name}`,`sdk/csharp/vendor/${name}`);
notices('sdk/csharp/vendor');
tree('java/src','sdk/java/src',name=>name.endsWith('.java'),javaDeviceNameDefault);
notices('sdk/java/vendor');
copy('rust/src/lib.rs','sdk/rust/vendor/rust/lib.rs');
tree('c','sdk/rust/vendor/c',name=>/\.(c|h)$/.test(name)||/LICEN[CS]E|NOTICE/i.test(name),nativePortability);
notices('sdk/rust/vendor');
const rb = 'sdk/ruby/lib/licentivo_launch/vendor';
copy('ruby/lib/licentivo.rb',`${rb}/licentivo.rb`,text=>text
  .replace("ENV['LICENTIVO_NATIVE_LIBRARY'] || ",'')
  .replace("path = [suffix, 'so', 'dll', 'dylib'].uniq.map { |ext| File.join(folder, \"licentivo_native.#{ext}\") }", "folders = [folder]\n        spec = Gem.loaded_specs['licentivo-launch'] if defined?(Gem)\n        folders << File.join(spec.extension_dir, 'licentivo_launch/vendor/licentivo/native') if spec\n        path = folders.product([suffix, 'so', 'dll', 'dylib'].uniq).map { |dir, ext| File.join(dir, \"licentivo_native.#{ext}\") }")
  .replace('Licentivo native core is missing. Run ruby sdk/ruby/build_native.rb for a source bundle, or install the compiled gem. See sdk/ruby/README.md.', 'Launch private native core is missing. Install the Launch gem for your platform or build its source gem with the normal C11/curl development tools.'));
tree('c','sdk/ruby/vendor/c',name=>/\.(c|h)$/.test(name)||/LICEN[CS]E|NOTICE/i.test(name),nativePortability);
notices('sdk/ruby/vendor');
fs.mkdirSync(path.join(root,rb,'licentivo/native'),{recursive:true});
for (const name of ['CMakeLists.txt','LicentivoConfig.cmake.in','LICENSE','NOTICE','VERSION']) copy(name,`sdk/native/vendor/sdk/${name}`);
tree('c','sdk/native/vendor/sdk/c',name=>/\.(c|h)$/.test(name)||/LICEN[CS]E|NOTICE/i.test(name),nativePortability);
tree('cpp','sdk/native/vendor/sdk/cpp',name=>name.endsWith('.hpp'));
fs.writeFileSync(path.join(root,'sdk/managed-sdk-manifest.json'),JSON.stringify({schema_version:1,sdk_version:upstreamVersion,source_revision:process.argv[3]||'release',changes:'Private import/native-library relocation, a Windows SDK architecture-constant compatibility guard and an optional Java device-name default, with embedded text line endings normalized to LF; authorization and cryptographic logic are unchanged. Account-lifecycle revisions are released together with the matching Licentivo server.',files:records},null,2)+'\n');
console.log(`Embedded SDK ${upstreamVersion}: ${records.length} files, with source and destination SHA-256 records.`);
