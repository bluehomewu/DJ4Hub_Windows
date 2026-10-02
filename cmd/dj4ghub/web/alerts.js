(() => {
  // Alert sounds are on unless the user turned them off.
  const storageKey = 'dj4hub-alert-sound';
  let audio, enabled = readPreference(), initialized = false, busy = false;
  const sms = new Set(), calls = new Set();
  const buttons = document.querySelectorAll('[data-enable-alerts]');
  function readPreference() {
    try { return localStorage.getItem(storageKey) !== 'off'; } catch { return true; }
  }
  function savePreference() {
    try { localStorage.setItem(storageKey, enabled ? 'on' : 'off'); } catch { /* Preference is per session only. */ }
  }
  function renderButtons() {
    const waiting = enabled && (!audio || audio.state !== 'running');
    buttons.forEach(b => {
      b.textContent = !enabled ? '開啟來電／簡訊提示音' : (waiting ? '提示音已開啟（點一下頁面即可發聲）' : '關閉來電／簡訊提示音');
    });
  }
  // Browsers only start audio after a user gesture, so unlock on the first one.
  async function unlockAudio() {
    if (!enabled) return;
    try {
      if (!audio) audio = new AudioContext();
      if (audio.state !== 'running') await audio.resume();
    } catch { /* Retried on the next gesture. */ }
    renderButtons();
  }
  ['pointerdown', 'keydown'].forEach(type => document.addEventListener(type, () => { void unlockAudio(); }, { capture: true }));
  buttons.forEach(button => button.addEventListener('click', async () => {
    enabled = !enabled;
    savePreference();
    if (enabled) await unlockAudio();
    renderButtons();
  }));
  renderButtons();
  void unlockAudio();
  function beep(call) {
    if (!enabled || !audio || audio.state !== 'running') return;
    const oscillator = audio.createOscillator(), gain = audio.createGain();
    oscillator.frequency.value = call ? 660 : 880;
    gain.gain.setValueAtTime(0.08, audio.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.001, audio.currentTime + 0.5);
    oscillator.connect(gain); gain.connect(audio.destination);
    oscillator.start(); oscillator.stop(audio.currentTime + 0.5);
    oscillator.onended = () => { oscillator.disconnect(); gain.disconnect(); };
  }
  async function poll() {
    if (busy) return; busy = true;
    try {
      const response = await fetch('/api/alerts'); if (!response.ok) return;
      const data = await response.json();
      const newSMS = initialized && data.sms.some(id => !sms.has(id));
      const newCall = data.ringing.some(id => !calls.has(id));
      data.sms.forEach(id => sms.add(id)); data.ringing.forEach(id => calls.add(id));
      initialized = true;
      if (newCall || newSMS) beep(newCall);
    } catch { /* Preserve event identities across transient disconnections. */ }
    finally { busy = false; }
  }
  void poll(); setInterval(poll, 3000);
})();
