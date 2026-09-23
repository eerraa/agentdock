// Browser-only acceptance driver. Uses a fresh profile and loopback fixture;
// never connects to a user's browser, account, or running AgentDock service.
import assert from 'node:assert/strict';
import {spawn, execFile} from 'node:child_process';
import {once} from 'node:events';
import {createServer} from 'node:http';
import {mkdtemp, readFile, writeFile, mkdir, access} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join, dirname} from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';

const [fixturePath, resultPath] = process.argv.slice(2);
assert(fixturePath && resultPath, 'Usage: node browser-product-renderer.mjs fixture.json result.json');
const browserPath = process.env.AGENTDOCK_ACCEPTANCE_BROWSER;
assert(browserPath, 'AGENTDOCK_ACCEPTANCE_BROWSER must name an actual Chromium browser executable');
await access(browserPath);
assert.equal(typeof WebSocket, 'function', 'Node with a built-in WebSocket client is required for this developer test');
const cases = JSON.parse(await readFile(fixturePath, 'utf8'));
assert(Array.isArray(cases) && cases.length > 0 && cases.length <= 100, 'Invalid fixture count');
for (const item of cases) assert(typeof item.html === 'string' && item.html.length < 1024 * 1024 && item.name && item.locale && Array.isArray(item.expected), 'Invalid browser fixture');
await mkdir(dirname(resultPath), {recursive:true});
const profile = await mkdtemp(join(tmpdir(), 'agentdock-browser-test-'));
const scriptJSON = value => JSON.stringify(value).replaceAll('<', '\u003c').replaceAll('>', '\u003e').replaceAll('&', '\u0026');
const failures = [], metrics = [], requests = [], errors = [];
let child, socket, server, browserVersion, sessionID, sequence = 0;
const pending = new Map();
function rpc(method, params = {}, sessionId) {
  return new Promise((resolve, reject) => {
    const id = ++sequence;
    const timer = setTimeout(() => {pending.delete(id); reject(new Error(`CDP timed out: ${method}`));}, 10000);
    pending.set(id, {resolve, reject, timer});
    socket.send(JSON.stringify({id, method, params, ...(sessionId ? {sessionId} : {})}));
  });
}
async function evaluate(expression) {
  const answer = await rpc('Runtime.evaluate', {expression, returnByValue:true, awaitPromise:true}, sessionID);
  if (answer.exceptionDetails) throw new Error(JSON.stringify(answer.exceptionDetails));
  return answer.result.value;
}
async function until(check, description, timeout = 10000) {
  const end = Date.now() + timeout;
  let last;
  while (Date.now() < end) {try {last = await check(); if (last) return last;} catch (error) {last = String(error);} await delay(60);}
  throw new Error(`Timed out waiting for ${description}; last=${JSON.stringify(last)}`);
}
function parentPage(item, index, theme, width) {
  return `<!doctype html><html><head><meta charset="utf-8"><title>Isolated AgentDock browser fixture</title></head><body style="margin:0"><script>
    window.fixture=${scriptJSON({locale:item.locale, output:item.output, theme})};window.received=[];window.initialized=false;
    window.addEventListener('message',event=>{const frame=document.getElementById('app');if(!frame||event.source!==frame.contentWindow)return;
      const message=event.data;if(!message||message.jsonrpc!=='2.0')return;window.received.push(message.method||'response');
      if(message.method==='ui/initialize')event.source.postMessage({jsonrpc:'2.0',id:message.id,result:{protocolVersion:'2026-01-26',hostContext:{locale:fixture.locale,theme:fixture.theme}}},'*');
      if(message.method==='ui/notifications/initialized')window.initialized=true;
    });
    window.deliverFixture=()=>document.getElementById('app').contentWindow.postMessage({jsonrpc:'2.0',method:'ui/notifications/tool-result',params:{structuredContent:fixture.output}},'*');
    window.setFixtureLocale=locale=>document.getElementById('app').contentWindow.postMessage({jsonrpc:'2.0',method:'ui/notifications/host-context-changed',params:{locale,theme:fixture.theme}},'*');
  </script><iframe id="app" title="MCP App acceptance" style="display:block;border:0;width:${width}px;height:1400px" src="/content/${index}"></iframe></body></html>`;
}
try {
  server = createServer((request,response) => {
    const url = new URL(request.url, 'http://127.0.0.1');requests.push(url.pathname);
    const index = Number(url.pathname.split('/')[2]);const item = cases[index];
    if (!item || !/^\/(page|content)\/\d+$/.test(url.pathname)) {response.writeHead(404);response.end();return;}
    response.setHeader('Content-Type', 'text/html; charset=utf-8');response.setHeader('Cache-Control', 'no-store');
    if (url.pathname.startsWith('/content/') || item.kind !== 'mcp') {
      for (const [name,value] of Object.entries(item.headers || {})) response.setHeader(name,value);
      response.end(item.html);
    } else response.end(parentPage(item,index,url.searchParams.get('theme') || 'light',Number(url.searchParams.get('width')) || 1000));
  });
  server.listen(0, '127.0.0.1');await once(server,'listening');
  const origin = `http://127.0.0.1:${server.address().port}`;
  child = spawn(browserPath, ['--headless=new','--remote-debugging-port=0','--remote-debugging-address=127.0.0.1',`--user-data-dir=${profile}`,'--no-first-run','--no-default-browser-check','--disable-background-networking','--lang=en-US','about:blank'], {stdio:['ignore','ignore','pipe'],windowsHide:true});
  child.on('error', error=>errors.push(String(error)));
  let browserLog='';child.stderr.on('data',data=>{browserLog=(browserLog+data.toString()).slice(-12000);});
  // Windows launchers may exit before their browser child; observe the fresh profile endpoint.
  const port = await until(async()=>{const text=await readFile(join(profile,'DevToolsActivePort'),'utf8');const p=Number(text.split('\n')[0]);return p>0?p:false;}, 'the isolated browser debugging endpoint',20000);
  const endpoint=await (await fetch(`http://127.0.0.1:${port}/json/version`,{signal:AbortSignal.timeout(5000)})).json();
  assert(new URL(endpoint.webSocketDebuggerUrl).hostname === '127.0.0.1' || new URL(endpoint.webSocketDebuggerUrl).hostname === 'localhost', 'Non-loopback browser endpoint');
  browserVersion=endpoint.Browser;socket=new WebSocket(endpoint.webSocketDebuggerUrl);
  socket.addEventListener('message',event=>{const message=JSON.parse(event.data);if(message.id){const request=pending.get(message.id);if(!request)return;pending.delete(message.id);clearTimeout(request.timer);if(message.error)request.reject(new Error(JSON.stringify(message.error)));else request.resolve(message.result);} else if(message.method==='Runtime.exceptionThrown')errors.push(JSON.stringify(message.params.exceptionDetails));});
  await once(socket,'open');
  const target=await rpc('Target.createTarget',{url:'about:blank'});
  sessionID=(await rpc('Target.attachToTarget',{targetId:target.targetId,flatten:true})).sessionId;
  await rpc('Runtime.enable',{},sessionID);await rpc('Page.enable',{},sessionID);
  for (let index=0;index<cases.length;index++) {
    const item=cases[index];
    const layouts=item.locale==='ko-KR'?[{width:1000,theme:'light'},{width:400,theme:'dark'}]:[{width:1000,theme:'light'}];
    for(const {width,theme} of layouts) {
      const name=`${item.name}-${width}-${theme}`;
      try {
        await rpc('Emulation.setDeviceMetricsOverride',{width,height:1400,deviceScaleFactor:1,mobile:false},sessionID);
        await rpc('Emulation.setEmulatedMedia',{features:[{name:'prefers-color-scheme',value:theme}]},sessionID);
        const url=`${origin}/page/${index}?width=${width}&theme=${theme}`;
        await rpc('Page.navigate',{url},sessionID);
        await until(()=>evaluate(`location.href===${JSON.stringify(url)}&&document.readyState==='complete'`),name+' page load');
        const doc=item.kind==='mcp'?`document.getElementById('app').contentDocument`:'document';
        if(item.kind==='mcp') {
          await until(()=>evaluate(`window.initialized===true&&${doc}.documentElement.lang===${JSON.stringify(item.locale)}`),name+' MCP initialization');
          const waiting=await evaluate(`${doc}.body.innerText`);
          assert(waiting.includes(item.waiting),`${name}: initial language did not update the waiting message: ${waiting}`);
          await evaluate('window.deliverFixture()');
          await until(()=>evaluate(`Boolean(${doc}.querySelector('.compact-toggle'))`),name+' loaded card');
          await evaluate(`${doc}.querySelector('.compact-toggle').click()`);
        }
        const value=await evaluate(`(()=>{const d=${doc};return {language:d.documentElement.lang,title:d.title,text:d.body.innerText,overflow:d.documentElement.scrollWidth>d.documentElement.clientWidth+2,links:[...d.querySelectorAll('a')].map(a=>({href:a.getAttribute('href'),rel:a.rel})),injected:Boolean(d.querySelector('img[src="x"]')),messages:window.received||[]}})()`);
        assert.equal(value.language,item.locale,`${name}: wrong language`);
        for(const expected of item.expected)assert(value.text.includes(expected),`${name}: missing ${JSON.stringify(expected)} in ${JSON.stringify(value.text)}`);
        for(const absent of item.absent||[])assert(!value.text.includes(absent),`${name}: unexpected ${absent}`);
        assert(!value.injected,`${name}: tool output injected an HTML element`);
        assert(!value.overflow,`${name}: localized page overflows its viewport`);
        if(item.kind==='mcp') {
          assert(value.messages.every(method=>['ui/initialize','ui/notifications/initialized','ui/notifications/size-changed'].includes(method)),`${name}: rendering issued a mutating/unknown RPC`);
          assert(value.links.every(link=>!link.href||!/^javascript:/i.test(link.href)),`${name}: unsafe artifact URL`);
          const before=await evaluate(`${doc}.body.innerText`);
          await evaluate(`(()=>{const w=document.getElementById('app').contentWindow;w.dispatchEvent(new MessageEvent('message',{source:w,data:{jsonrpc:'2.0',method:'ui/notifications/tool-result',params:{structuredContent:{...window.fixture.output,view:'fixture-unrelated-view'}}}}));w.postMessage({jsonrpc:'1.0',method:'ui/notifications/tool-result',params:{structuredContent:{...window.fixture.output,view:'fixture-unrelated-view'}}},'*')})()`);
          await delay(80);assert.equal(await evaluate(`${doc}.body.innerText`),before,`${name}: untrusted source/protocol changed the view`);
          for(const locale of ['en','zh-CN','ko-KR',item.locale]){
            await evaluate(`window.setFixtureLocale(${JSON.stringify(locale)})`);
            await until(()=>evaluate(`${doc}.documentElement.lang===${JSON.stringify(locale)}`),name+' language change');
            if(item.preserved)for(const original of item.preserved)assert((await evaluate(`${doc}.body.textContent`)).includes(original),`${name}: locale switch rewrote original data`);
          }
        }
        if(item.kind==='mcp') await evaluate(`(()=>{const b=${doc}.querySelector('.compact-toggle');if(b&&b.getAttribute('aria-expanded')!=='true')b.click()})()`);
        if(item.locale==='ko-KR'&&width===1000){
          const screenshot=await rpc('Page.captureScreenshot',{format:'png'},sessionID);
          await writeFile(join(dirname(resultPath),name.replace(/[^a-zA-Z0-9_.-]/g,'_')+'.png'),Buffer.from(screenshot.data,'base64'));
        }
        metrics.push({name,language:value.language,title:value.title,loaded:true,width,theme,read_only:item.kind==='mcp',rendering:'real Chromium DOM; 1x device emulation, not physical Windows DPI'});
      } catch(error) {failures.push({name,error:String(error)});}
    }
  }
  if(errors.length)failures.push({name:'browser-runtime-errors',error:errors.join('\n')});
} catch(error) {failures.push({name:'driver',error:String(error)});}
finally {
  if(socket?.readyState===WebSocket.OPEN) {try{await rpc('Browser.close');}catch{} socket.close();}
  if(child&&child.exitCode===null) {
    await Promise.race([once(child,'exit'),delay(5000)]);
    if(child.exitCode===null) {
      // Only the child created above and its descendants, never a process-name match.
      await new Promise(resolve=>execFile('taskkill.exe',['/PID',String(child.pid),'/T','/F'],{windowsHide:true},()=>resolve()));
    }
  }
  for(const request of pending.values()){clearTimeout(request.timer);request.reject(new Error('Browser fixture closed'));}pending.clear();
  if(server){server.closeAllConnections();await new Promise(resolve=>server.close(resolve));}
  const result={passed:failures.length===0,browser:browserVersion,profile,fixtures:cases.length,passed_cases:metrics.length,metrics,failures,requests:requests.length};
  await writeFile(resultPath,JSON.stringify(result,null,2)+'\n');
  console.log(JSON.stringify({passed:result.passed,browser:browserVersion,passed_cases:metrics.length,failures,resultPath}));
  if(failures.length)process.exitCode=1;
}
