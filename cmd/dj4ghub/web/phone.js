let callPollBusy = false;
let phoneActionBusy = false;
let callStarted = new Map();
let audioStreams = [];
let audioPlayers = [];
let audioMuted = false;
let previousCallsPresent = false;
let moduleAudioToken = sessionStorage.getItem('dj4hub-module-audio-token') || '';
let moduleAudioBusy = false;
let moduleAudioLeaseBusy = false;
let moduleAudioPreparation = null;
async function refreshCalls() {
  if (moduleAudioBusy) { $('#phone-status').textContent = '模組音訊正在初始化，等待 USB 重新連線…'; return; }
  if (callPollBusy) return;
  callPollBusy = true;
  try {
    const result = await api('/api/calls');
    const states = ['通話中', '保持', '撥號中', '響鈴中', '來電', '呼叫等待'];
    const calls = result.calls || [];
    const ids = new Set(calls.map(c => c.id));
    for (const id of callStarted.keys()) if (!ids.has(id)) callStarted.delete(id);
    $('#phone-status').textContent = calls.map(c => {
      if (c.state === 0 && !callStarted.has(c.id)) callStarted.set(c.id, Date.now());
      const elapsed = callStarted.has(c.id) ? ` · ${Math.floor((Date.now()-callStarted.get(c.id))/1000)}s` : '';
      return `${c.number || '未知號碼'} · ${states[c.state] || '未知狀態'}${elapsed}`;
    }).join(' / ') || '無語音通話';
    $('#phone-dial').disabled = calls.length > 0;
    $('#phone-answer').disabled = !calls.some(c => c.state === 4 || c.state === 5);
    $('#phone-hangup').disabled = !calls.length;
    document.querySelectorAll('#phone-keypad button').forEach(b => b.disabled = !calls.some(c => c.state === 0));
    if (!calls.length && previousCallsPresent) {
      stopPhoneAudio();
    }
    previousCallsPresent = calls.length > 0;
  } catch (e) { $('#phone-status').textContent = moduleAudioBusy || /NO_DEVICE|NOT_FOUND/i.test(e.message) ? 'USB 正在重新連線，稍後自動恢復…' : `通話狀態不可用：${e.message}`; }
  finally { callPollBusy = false; }
}
async function phoneAction(action, extra = {}) {
  if (phoneActionBusy) return;
  phoneActionBusy = true;
  try {
    if ((action === 'dial' || action === 'answer') && $('#phone-use-audio').checked) {
      if (action === 'dial' && !/^\+?[0-9]{1,20}$/.test(extra.number || '')) throw new Error('請輸入有效電話號碼');
      await ensureModuleAudio();
      await connectPhoneAudio();
    }
    await api('/api/calls', {method:'POST',body:JSON.stringify({action,...extra})});
    $('#phone-feedback').textContent = '操作已提交';
    if (action === 'hangup') {
      stopPhoneAudio();
    }
    await refreshCalls();
  } catch (e) { if (action === 'dial' || action === 'answer') stopPhoneAudio(); $('#phone-feedback').textContent = e.message; }
  finally { phoneActionBusy = false; }
}
$('#phone-dial').onclick = () => phoneAction('dial',{number:$('#phone-number').value.trim()});
$('#phone-answer').onclick = () => phoneAction('answer');
$('#phone-hangup').onclick = () => phoneAction('hangup');
for (const digit of '123456789*0#') {
  const button = document.createElement('button'); button.type='button'; button.className='secondary'; button.textContent=digit; button.disabled=true;
  button.onclick=()=>phoneAction('dtmf',{digit}); $('#phone-keypad').append(button);
}
document.querySelector('[data-view="calls"]').addEventListener('click',refreshCalls);
setInterval(()=> { if ($('#calls').classList.contains('active') || callStarted.size) void refreshCalls(); },2000);
$('#apn-preset').onchange = e => { if (e.target.value) $('#apn-value').value=e.target.value; };
$('#apn-read').onclick = async () => {
  try { const result=await api('/api/network'); const current=result.pdp_contexts?.find(c=>c.id===1); if(current){ $('#apn-value').value=current.apn; $('#apn-pdn').value=current.pdn; $('#apn-pdn').dispatchEvent(new Event('change', {bubbles:true})); $('#apn-preset').value=''; $('#apn-preset').dispatchEvent(new Event('change', {bubbles:true})); $('#apn-feedback').textContent='已讀取目前主資料 APN'; } }
  catch(e){ $('#apn-feedback').textContent=e.message; }
};
$('#apn-save').onclick = async () => {
  const apn=$('#apn-value').value.trim();
  if (!await showModal({title:'儲存 APN',message:`主資料 APN 將改為 ${apn}。可能需要重新連線資料網路才能生效。`,confirmLabel:'儲存'})) return;
  $('#apn-save').disabled=true;
  try { const result=await api('/api/network/apn',{method:'POST',body:JSON.stringify({apn,pdn:$('#apn-pdn').value})}); $('#apn-feedback').textContent=result.summary; }
  catch(e){$('#apn-feedback').textContent=e.message;}
  finally{$('#apn-save').disabled=false;}
};
function stopPhoneAudio(){
  audioPlayers.forEach(p=>{p.pause();p.srcObject=null;});audioPlayers=[];
  audioStreams.forEach(s=>s.getTracks().forEach(t=>t.stop()));audioStreams=[];
  $('#audio-feedback').textContent='音訊已斷開';
}
let audioPermissionRequested = false;
let audioDiscoveryBusy = false;
// Labels identify candidates only; enumeration does not prove a working call route.
function isModemAudio(device) {
  return /quectel|baiwang|qdc507|eg25|\b[as]c? interface\b|\bas interface\b/i.test(device.label || '');
}
function updateAudioAvailability() {
  $('#audio-connect').disabled = typeof HTMLMediaElement.prototype.setSinkId !== 'function';
}
$('#audio-connect').disabled = true;
for (const id of ['audio-mic','audio-speaker','audio-modem-in','audio-modem-out']) {
  $('#'+id).addEventListener('change', updateAudioAvailability);
}
async function discoverPhoneAudio(){
  if (audioDiscoveryBusy) return;
  audioDiscoveryBusy = true;
  audioPermissionRequested = true;
  $('#audio-discover').disabled = true;
  $('#audio-feedback').textContent = '請允許瀏覽器使用麥克風，以讀取音訊裝置。不會自動連線通話。';
  try{
    if (!navigator.mediaDevices?.getUserMedia) throw new Error('目前瀏覽器無法請求麥克風權限，請使用 localhost 或 HTTPS 開啟');
    const permission=await navigator.mediaDevices.getUserMedia({audio:true});permission.getTracks().forEach(t=>t.stop());
    const devices=await navigator.mediaDevices.enumerateDevices();
    for(const [id,kind] of [['audio-mic','audioinput'],['audio-modem-in','audioinput'],['audio-speaker','audiooutput'],['audio-modem-out','audiooutput']]){
      const select=$('#'+id), previous=select.value;select.replaceChildren(new Option('請選擇裝置',''));
      const moduleField = id.startsWith('audio-modem');
      const choices = devices.filter(d => d.kind === kind && (moduleField ? isModemAudio(d) : !isModemAudio(d)));
      choices.forEach(d=>select.add(new Option(d.label||d.deviceId,d.deviceId)));
      select.disabled = choices.length === 0;
      if (!choices.length) select.options[0].textContent = moduleField ? '未偵測到模組音效卡' : '未偵測到裝置';
      if (previous && Array.from(select.options).some(option => option.value === previous)) select.value = previous;
      else if (moduleField && choices.length === 1) select.value = choices[0].deviceId;
      else if (!moduleField) select.value = choices.find(d => d.deviceId === 'default')?.deviceId || choices[0]?.deviceId || '';
      select.dispatchEvent(new Event('change', {bubbles:true}));
    }
    updateAudioAvailability();
    const hasModule = !$('#audio-modem-in').disabled && !$('#audio-modem-out').disabled;
    $('#audio-feedback').textContent = !hasModule ? '模組音訊尚未就緒，撥號或連線音訊時會自動初始化。無需選擇電腦自帶音效卡代替模組音效卡。' : '已識別模組音效卡，撥號時自動連線電腦音訊。建議使用耳機避免回聲。';
  }catch(e){$('#audio-feedback').textContent=e.name === 'NotAllowedError' ? '麥克風權限未獲允許。可在瀏覽器站點設定中允許後，點選“查詢音訊裝置”重試。' : `無法讀取音訊裝置：${e.message}`;}
  finally { audioDiscoveryBusy = false; $('#audio-discover').disabled = false; }
}
$('#audio-discover').onclick=discoverPhoneAudio;
document.querySelector('[data-view="calls"]').addEventListener('click', () => {
  if (!audioPermissionRequested) void discoverPhoneAudio();
});
async function connectPhoneAudio(){
  updateAudioAvailability();
  if ($('#audio-connect').disabled) throw new Error('音訊裝置尚未就緒或瀏覽器不支援輸出選擇，請檢查裝置選項');
  stopPhoneAudio();
  try{
    if(typeof HTMLMediaElement.prototype.setSinkId!=='function')throw new Error('瀏覽器不支援輸出裝置選擇');
    const mic=$('#audio-mic').value, modemIn=$('#audio-modem-in').value, speaker=$('#audio-speaker').value, modemOut=$('#audio-modem-out').value;
    if(!mic||!modemIn||!speaker||!modemOut||mic===modemIn||speaker===modemOut)throw new Error('請選擇不同的電腦與模組音訊裝置');
    for(const [input,output] of [[mic,modemOut],[modemIn,speaker]]){
      const stream=await navigator.mediaDevices.getUserMedia({audio:{deviceId:{exact:input},echoCancellation:input===mic,noiseSuppression:input===mic}});audioStreams.push(stream);
      const player=new Audio();audioPlayers.push(player);player.srcObject=stream;await player.setSinkId(output);player.volume=input===mic?1:Number($('#audio-volume').value);await player.play();
    }
    audioMuted=false;$('#audio-mute').setAttribute('aria-pressed','false');$('#audio-feedback').textContent='電腦與模組的音訊流已連線，請在通話中確認雙方聲音。';
  }catch(e){stopPhoneAudio();$('#audio-feedback').textContent=`連線失敗：${e.message}`;throw e;}
}
$('#audio-connect').onclick=async()=>{
  try { await ensureModuleAudio(); await connectPhoneAudio(); }
  catch(e) { $('#audio-feedback').textContent = e.message; }
};
$('#audio-stop').onclick=stopPhoneAudio;
$('#audio-mute').onclick=()=>{audioMuted=!audioMuted;audioStreams[0]?.getAudioTracks().forEach(t=>t.enabled=!audioMuted);$('#audio-mute').setAttribute('aria-pressed',String(audioMuted));};
$('#audio-volume').oninput=e=>{if(audioPlayers[1])audioPlayers[1].volume=Number(e.target.value);};
function moduleAudioRequest(path, token = moduleAudioToken) {
  return api('/api/calls/audio/' + path, {method:'POST',headers:{'X-DJ4Hub-Audio':'1','X-DJ4Hub-Audio-Token':token,...(path === 'prepare' ? {'X-DJ4Hub-Initialize':'1'} : {})}});
}
function clearModuleAudioToken() {
  moduleAudioToken = '';
  sessionStorage.removeItem('dj4hub-module-audio-token');
  $('#audio-release').disabled = true;
}
async function refreshModuleAudio() {
  try {
    const result = await api('/api/calls/audio');
    $('#audio-release').disabled = !moduleAudioToken || moduleAudioBusy;
    if (!result.active && moduleAudioToken) clearModuleAudioToken();
    $('#audio-module-status').textContent = !result.configured ? (result.summary || '本機音訊依賴未就緒，請執行 dj4ghub audio-check。') : result.active ? '音訊待機就緒，不代表 IMS 已註冊或電信業者通話可用。掛斷後保持待機；關閉頁面或失去心跳後恢復 USB。' : '撥號時自動初始化音訊；首次允許後，進入電話頁面也會自動就緒。';
  } catch(e) { $('#audio-module-status').textContent = e.message; }
}
async function releaseModuleAudio() {
  if (!moduleAudioToken || moduleAudioBusy) return;
  moduleAudioBusy = true;
  stopPhoneAudio();
  $('#audio-release').disabled = true;
  $('#audio-module-status').textContent = '正在停止音訊並確認 USB 恢復…';
  try {
    const result = await moduleAudioRequest('stop');
    clearModuleAudioToken();
    $('#audio-module-status').textContent = result.summary;
  } catch(e) {
    clearModuleAudioToken();
    $('#audio-module-status').textContent = e.message + '。已停止續期；必要時重新插拔模組。';
  } finally { moduleAudioBusy = false; }
}
function ensureModuleAudio() {
  if (!moduleAudioPreparation) {
    moduleAudioPreparation = prepareAutomaticAudio().finally(() => { moduleAudioPreparation = null; });
  }
  return moduleAudioPreparation;
}
async function prepareAutomaticAudio() {
  if (moduleAudioBusy) throw new Error('音訊正在初始化，請稍候');
  if (moduleAudioToken) {
    try { await moduleAudioRequest('lease'); return; }
    catch (_) { stopPhoneAudio(); clearModuleAudioToken(); }
  }
  const status = await api('/api/calls/audio');
  if (!status.configured) throw new Error(status.summary || '本機音訊依賴未配置');
  if (status.active) throw new Error('音訊由另一個頁面使用，請在原頁面停止後重試');
  const current = await api('/api/calls');
  if ((current.calls || []).length) throw new Error('目前有通話或來電，不能重連 USB 初始化音訊；請在無通話時進入電話頁完成自動初始化');
  if (localStorage.getItem('dj4hub-auto-audio-consent') !== '2') {
    if (!await showModal({title:'啟用自動通話音訊',message:'首次使用新模組會備份配置、授權並開啟 ADB，必要時重啟；該授權會保留，不會自動撤銷。隨後臨時載入已校驗的驅動，可能短暫中斷上網。不會刷韌體。掛斷關閉麥克風並保留待機。',confirmLabel:'允許初始化和自動音訊'})) throw new Error('已取消自動音訊');
    localStorage.setItem('dj4hub-auto-audio-consent', '2');
  }
  moduleAudioBusy = true;
  $('#audio-module-status').textContent = '正在校驗裝置、載入音訊並等待 USB 重新連線…';
  try {
    const result = await moduleAudioRequest('prepare', '');
    moduleAudioToken = result.token;
    sessionStorage.setItem('dj4hub-module-audio-token', moduleAudioToken);
    $('#audio-release').disabled = false;
    $('#audio-module-status').textContent = result.summary;
    moduleAudioBusy = false;
    await discoverPhoneAudio();
  } catch(e) { $('#audio-module-status').textContent = e.message; throw e; }
  finally { moduleAudioBusy = false; }
}
$('#audio-release').onclick = releaseModuleAudio;
setInterval(async () => {
  if (moduleAudioBusy || moduleAudioLeaseBusy) return;
  if (!moduleAudioToken) {
    if ($('#calls').classList.contains('active')) void refreshModuleAudio();
    return;
  }
  moduleAudioLeaseBusy = true;
  try { await moduleAudioRequest('lease'); }
  catch(e) { stopPhoneAudio(); clearModuleAudioToken(); $('#audio-module-status').textContent = e.message + '。音訊已斷開，下次撥號會重新初始化。'; }
  finally { moduleAudioLeaseBusy = false; }
}, 10000);
document.querySelector('[data-view="calls"]').addEventListener('click', refreshModuleAudio);
document.querySelector('[data-view="calls"]').addEventListener('click', () => {
  if ($('#phone-use-audio').checked && localStorage.getItem('dj4hub-auto-audio-consent') === '2' && !moduleAudioToken && !moduleAudioBusy) {
    void ensureModuleAudio().catch(e => { $('#audio-module-status').textContent = e.message; });
  }
});
void refreshModuleAudio();
window.addEventListener('pagehide', () => {
  stopPhoneAudio();
  if (moduleAudioToken) {
    void fetch('/api/calls/audio/stop', {method:'POST',keepalive:true,headers:{'X-DJ4Hub-Audio':'1','X-DJ4Hub-Audio-Token':moduleAudioToken}}).catch(()=>{});
    clearModuleAudioToken();
  }
});
navigator.mediaDevices?.addEventListener('devicechange', () => {
  if (audioStreams.length) { stopPhoneAudio(); $('#audio-feedback').textContent = '音訊裝置發生變化，已安全斷開，請重新查詢並連線。'; }
});
// Keep native values and change handlers as the source of truth.
document.querySelectorAll('.phone-audio-fields select, .apn-settings select').forEach(select => {
  const isAPN = select.closest('.apn-settings') !== null;
  const wrapper = document.createElement('div');
  wrapper.className = 'audio-select';
  select.before(wrapper);
  wrapper.append(select);
  const trigger = document.createElement('button');
  trigger.type = 'button';
  trigger.className = 'audio-select-trigger';
  trigger.setAttribute('aria-haspopup', 'listbox');
  trigger.setAttribute('aria-expanded', 'false');
  const caption = document.createElement('span');
  trigger.append(caption);
  const menu = document.createElement('div');
  menu.className = 'audio-select-menu';
  menu.id = select.id + '-menu';
  menu.role = 'listbox';
  menu.setAttribute('aria-label', select.getAttribute('aria-label'));
  menu.hidden = true;
  trigger.setAttribute('aria-controls', menu.id);
  wrapper.append(trigger, menu);
  const close = () => { menu.hidden = true; trigger.setAttribute('aria-expanded', 'false'); };
  const render = () => {
    trigger.disabled = select.disabled;
    caption.textContent = select.selectedOptions[0]?.textContent || '請選擇裝置';
    trigger.setAttribute('aria-label', select.getAttribute('aria-label') + '：' + caption.textContent);
    menu.replaceChildren();
    Array.from(select.options).forEach(option => {
      if (!option.value && !isAPN) return;
      const item = document.createElement('button');
      item.type = 'button';
      item.role = 'option';
      item.className = 'audio-select-option';
      item.textContent = option.textContent;
      item.setAttribute('aria-selected', String(option.selected));
      item.disabled = option.disabled;
      // Safari can blur the menu before click when a button is pressed.
      item.onmousedown = event => event.preventDefault();
      item.onclick = event => {
        event.preventDefault();
        event.stopPropagation();
        select.value = option.value;
        select.dispatchEvent(new Event('change', { bubbles: true }));
        close(); trigger.focus();
      };
      menu.append(item);
    });
    if (!menu.children.length) {
      const empty = document.createElement('span');
      empty.textContent = '尚無可選裝置，請先查詢音訊裝置';
      empty.className = 'audio-select-option';
      menu.append(empty);
    }
  };
  trigger.onclick = event => {
    event.preventDefault();
    if (!menu.hidden) { close(); return; }
    document.dispatchEvent(new Event('audio-select-close'));
    render(); menu.hidden = false; trigger.setAttribute('aria-expanded', 'true');
    (menu.querySelector('[aria-selected="true"]') || menu.firstElementChild)?.focus();
  };
  trigger.onkeydown = event => {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); trigger.click(); }
  };
  menu.onkeydown = event => {
    if (event.key === 'Escape') { event.preventDefault(); close(); trigger.focus(); return; }
    const items = Array.from(menu.querySelectorAll('button:not(:disabled)'));
    const index = items.indexOf(document.activeElement);
    let next = index;
    if (event.key === 'ArrowDown') next = (index + 1) % items.length;
    else if (event.key === 'ArrowUp') next = (index - 1 + items.length) % items.length;
    else if (event.key === 'Home') next = 0;
    else if (event.key === 'End') next = items.length - 1;
    else return;
    event.preventDefault(); items[next]?.focus();
  };
  wrapper.addEventListener('focusout', event => { if (!wrapper.contains(event.relatedTarget)) close(); });
  document.addEventListener('click', event => { if (!wrapper.contains(event.target)) close(); });
  document.addEventListener('audio-select-close', close);
  select.addEventListener('change', render);
  new MutationObserver(() => { close(); render(); }).observe(select, { childList: true, subtree: true, attributes: true });
  render();
});
