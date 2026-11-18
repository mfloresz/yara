const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const { webcrypto } = require('node:crypto');
const path = require('node:path');
const { execFileSync } = require('node:child_process');
const readSource = relative => process.env.YARA_EXTENSION_BASELINE === '1'
  ? execFileSync('git', ['show', 'HEAD:extensions/'+relative], {cwd: path.join(__dirname, '..'), encoding:'utf8'})
  : fs.readFileSync(path.join(__dirname,relative),'utf8');
const roots = ['browser-worker-chrome', 'browser-worker-firefox', 'browser-worker-chrome-debug', 'browser-worker-firefox-debug'];
function load(root, overrides = {}) {
  const stored = {}; const navigations = []; const rules = [];
  const local = { get: async () => ({...stored}), set: (v, cb) => { Object.assign(stored, v); cb?.(); return Promise.resolve(); }, remove: async k => { delete stored[k]; } };
  const chrome = { storage: {local}, runtime: {id:'extension', sendMessage:async()=>{}}, tabs: {create:async v=>{navigations.push(v);return {id:42}}, update:async(id,v)=>{navigations.push(v);return {id}}, remove:async()=>{}}, declarativeNetRequest: {updateSessionRules:async v=>rules.push(v)} };
  const context = vm.createContext({console, URL, URLSearchParams, Uint8Array, Date, crypto:webcrypto, chrome, navigator:{userAgent:'test'}, getConfig:async()=>({serverAddr:'https://yara.example',autoConnect:false}), setConfig:async()=>{}, WorkerState:{DISCONNECTED:'disconnected'}, setTimeout,clearTimeout,...overrides});
  let shared = readSource(root+'/shared/supported-sites.js').replace(/^export /gm,'');
  let worker = readSource(root+'/background/service-worker.js').replace(/^import .*;\n/gm,'').replace(/\ninit\(\);\s*$/, '');
  const network = fs.readFileSync(path.join(__dirname,root,'shared/network-safety.js'),'utf8').replace(/^export /gm,'');
  const authPath=path.join(__dirname,root,'shared/worker-auth.js');
  const auth=fs.existsSync(authPath)?fs.readFileSync(authPath,'utf8').replace(/^export /gm,''):'';
  vm.runInContext(shared+'\n'+network+'\n'+auth+'\n'+worker,context);
  return {context, stored, chrome, navigations, rules, run:code=>vm.runInContext(code,context)};
}
for (const root of roots) {
  test(`${root}: remote addresses default to TLS, loopback stays usable`,()=>{
    const h=load(root);
    assert.equal(h.run("yaraBaseUrl('yara.example:5176')"),'https://yara.example:5176');
    assert.equal(h.run("yaraBaseUrl('localhost:5176')"),'http://localhost:5176');
    assert.throws(()=>h.run("yaraBaseUrl('http://yara.example')"));
    assert.equal(h.run("browserWorkerWebSocketURL('yara.example')"),`wss://yara.example/ws/browser-worker${root.endsWith('debug')?'-debug':''}`);
  });
  test(`${root}: private initial fetch rejected before network; redirects not automatically followed`,async()=>{
    const requests=[];
    const h=load(root,{fetch:async(url,opts)=>{requests.push({url,opts}); return {ok:true,url,headers:{get:()=> 'image/png'},arrayBuffer:async()=>new ArrayBuffer(0)}}});
    h.context.bytesToBase64=()=>({dataBase64:''});
    await assert.rejects(h.run("fetchImageBackground('http://127.0.0.1/admin')"));
    assert.equal(requests.length,0);
    await h.run("fetchImageBackground('https://public.example/image')");
    assert.equal(requests[0].opts.redirect,'manual');
  });
  test(`${root}: private redirect hop is never requested`,async()=>{
    const requests=[];
    const h=load(root,{fetch:async(url,opts)=>{
      requests.push(url);
      if(opts.redirect==='manual') return {status:302,headers:{get:()=> 'http://127.0.0.1/admin'}};
      requests.push('http://127.0.0.1/admin');
      return {ok:true,url:'http://127.0.0.1/admin',headers:{get:()=> 'image/png'},arrayBuffer:async()=>new ArrayBuffer(0)};
    }});
    await assert.rejects(h.run("fetchImageBackground('https://public.example/redirect')"));
    assert.deepEqual(requests,['https://public.example/redirect']);
  });
  test(`${root}: opaque manual redirects fail closed and do not issue another request`,async()=>{
    let count=0;
    const h=load(root,{fetch:async()=>{count++;return {type:'opaqueredirect'}}});
    await assert.rejects(h.run("fetchImageBackground('https://public.example/redirect')"));
    assert.equal(count,1);
  });
  test(`${root}: tab guard installed before navigation and unavailable guard fails closed`,async()=>{
    const h=load(root);
    await h.run("createSafeWorkerTab('https://public.example')");
    assert.ok(h.rules.length);
    const block = new RegExp(h.rules[0].addRules[2].condition.regexFilter,'i');
    for(const host of ['127.0.0.1','10.0.0.1','[::1]','[fd00::1]','localhost','x.localhost.','x.local','x.internal.']) {
      assert.ok(block.test('https://'+host+':8080/path'),host);
    }
    assert.equal(block.test('https://cdn.public.example/path'),false);
    assert.equal(h.navigations[0].url,'about:blank');
    assert.equal(h.navigations[1].url,'https://public.example');
    delete h.chrome.declarativeNetRequest;
    await assert.rejects(h.run("createSafeWorkerTab('https://public.example')"));
    assert.equal(h.navigations.length,2);
    const failing=load(root);
    failing.chrome.declarativeNetRequest.updateSessionRules=async()=>{throw new Error('permission denied')};
    await assert.rejects(failing.run("createSafeWorkerTab('https://public.example')"));
    assert.equal(failing.navigations.length,1);
    assert.equal(failing.navigations[0].url,'about:blank');
  });
  if (!root.endsWith('debug')) {
    test(`${root}: unsolicited callback cannot replace account`,async()=>{
      const h=load(root); h.stored.workerToken='original';
      h.run('connect = async () => {}; disconnect = () => {};');
      await h.run("handleTabUpdate(7,{status:'complete'},{url:'https://evil.example/api/v1/worker-auth/callback?token=attacker&user=evil'})");
      assert.equal(h.stored.workerToken,'original');
    });
    test(`${root}: content scripts cannot initiate authorization or change configuration`,()=>{
      const h=load(root);
      for(const type of ['BEGIN_AUTH','UPDATE_CONFIG','CONNECT']) {
        h.context.input={type,config:{serverAddr:'https://evil.example'}};
        assert.equal(h.run("handleInternalMessage(input,{id:'extension',tab:{id:4}},()=>{})"),false);
      }
      assert.equal(h.navigations.length,0);
    });
    test(`${root}: nonce binds origin, tab, timeout and is consumed once`,async()=>{
      const h=load(root); h.stored.workerToken='original';
      await h.run('beginWorkerAuth()');
      const auth=new URL(h.navigations.at(-1).url); const nonce=auth.searchParams.get('client_state');
      assert.match(nonce,/^[a-f0-9]{64}$/);
      const callback=`https://yara.example/api/v1/worker-auth/callback?user=u&client_state=${nonce}&token=new`;
      for (const [id,url] of [[9,callback],[42,callback.replace('yara.example','evil.example')],[42,callback.replace(nonce,'bad')]]) {
        h.context.testURL=url; await h.run(`handleTabUpdate(${id},{status:'complete'},{url:testURL})`);
        assert.equal(h.stored.workerToken,'original');
      }
      h.context.testURL=callback;
      // Avoid actual socket reconnect; callback processing remains real.
      h.run('connect = async () => {}; disconnect = () => {};');
      await Promise.all([h.run("handleTabUpdate(42,{status:'complete'},{url:testURL})"),h.run("handleTabUpdate(42,{status:'complete'},{url:testURL})")]);
      assert.equal(h.stored.workerToken,'new');
      h.stored.workerToken='later';
      await h.run("handleTabUpdate(42,{status:'complete'},{url:testURL})");
      assert.equal(h.stored.workerToken,'later');
      await h.run('beginWorkerAuth()');
      h.stored.pendingWorkerAuth.expiresAt=Date.now()-1;
      h.context.testURL=callback.replace(nonce,h.stored.pendingWorkerAuth.state);
      await h.run("handleTabUpdate(42,{status:'complete'},{url:testURL})");
      assert.equal(h.stored.workerToken,'later');
    });
  }
}
