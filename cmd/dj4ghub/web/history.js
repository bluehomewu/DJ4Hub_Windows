(() => {
  const dialog = document.createElement('dialog');
  dialog.style.cssText = 'width:min(760px,90vw);max-height:80vh;border:1px solid #ddd;border-radius:16px;padding:24px;color:inherit;background:var(--surface,#fff)';
  dialog.innerHTML = '<h2>通訊記錄</h2><p>記錄儲存在本機。無法確認來源的舊簡訊歸入“未歸屬”；時長為觀測值。</p><select aria-label="篩選 SIM 卡"></select> <button type="button" data-refresh>重新整理</button> <button type="button" data-close>關閉</button><p role="status"></p><div data-rows></div><button type="button" data-more>載入更多</button>';
  document.body.append(dialog);
  const select = dialog.querySelector('select');
  const rows = dialog.querySelector('[data-rows]');
  const status = dialog.querySelector('[role=status]');
  const more = dialog.querySelector('[data-more]');
  let kind = 'sms', offset = 0, loading = false;
  const labels = { received:'已接收', sent:'已提交模組', failed_or_partial:'失敗或部分傳送', failed:'撥號失敗', observed:'呼叫中', connected:'通話中', ended:'已結束', unanswered:'未接通', missed:'未接來電', interrupted:'記錄中斷' };
  function options(cards, value) {
    select.replaceChildren();
    for (const [key, label] of [['current','目前卡片'],['all','全部卡片'],['unknown','未歸屬'],...Object.entries(cards)]) {
      const option = document.createElement('option'); option.value = key; option.textContent = label; select.append(option);
    }
    select.value = value;
  }
  async function load(append = false) {
    if (loading) return;
    loading = true; select.disabled = true; more.disabled = true;
    if (!append) { offset = 0; rows.replaceChildren(); }
    const card = select.value || 'current';
    try {
      const response = await fetch(`/api/history?kind=${kind}&card=${encodeURIComponent(card)}&offset=${offset}`);
      if (!response.ok) throw new Error('無法讀取記錄，請確認服務已更新');
      const data = await response.json();
      options(data.cards, card);
      for (const record of data.records) {
        const article = document.createElement('article'); article.style.cssText = 'padding:16px 0;border-bottom:1px solid #ddd';
        const title = document.createElement('strong'); title.textContent = `${record.number} · ${record.direction === 'incoming' ? '接收 / 來電' : '傳送 / 去電'} · ${labels[record.state] || record.state}`;
        const meta = document.createElement('p'); meta.textContent = `${record.iccid ? 'SIM · ' + record.iccid.slice(-4) : '未歸屬'} · ${new Date(record.started).toLocaleString()}`;
        const body = document.createElement('p'); body.style.whiteSpace = 'pre-wrap'; body.textContent = kind === 'sms' ? record.content : record.state === 'ended' ? `觀測通話時長：${record.duration_seconds} 秒` : '';
        article.append(title, meta, body); rows.append(article);
      }
      offset = data.next_offset; status.textContent = `共 ${data.total} 條`; more.hidden = offset >= data.total;
    } catch (error) { status.textContent = error.message; more.hidden = true; }
    finally { loading = false; select.disabled = false; more.disabled = false; }
  }
  document.querySelectorAll('[data-history]').forEach(button => button.addEventListener('click', () => {
    if (loading) return;
    kind = button.dataset.history; options({}, 'current'); dialog.showModal(); load();
  }));
  select.addEventListener('change', () => load());
  dialog.querySelector('[data-refresh]').onclick = () => load();
  dialog.querySelector('[data-close]').onclick = () => dialog.close();
  more.onclick = () => load(true);
  setInterval(() => { if (dialog.open && select.value === 'current' && offset <= 100) load(); }, 10000);
})();
