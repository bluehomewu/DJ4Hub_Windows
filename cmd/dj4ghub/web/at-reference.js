// AT command reference for the AT console. {a | b} means choose one value,
// [ ... ] marks an optional part. Clicking an entry only fills the input.
(() => {
  const RISK = {
    read: { label: "唯讀", tone: "read" },
    write: { label: "修改設定", tone: "write" },
    reboot: { label: "需重啟", tone: "reboot" },
    danger: { label: "高風險", tone: "danger" },
  };
  const groups = [
    { title: "模組資訊", commands: [
      ["ATI", "模組型號與韌體版本", "read"],
      ["AT+CGMR", "韌體版本", "read"],
      ["AT+GSN", "模組 IMEI", "read"],
      ["AT+QLTS={0 | 1 | 2}", "網路時間：0 最後同步時間、1 目前 GMT、2 目前本地時間", "read"],
    ] },
    { title: "SIM 卡", commands: [
      ["AT+CPIN?", "SIM 狀態（READY、SIM PIN、SIM PUK）", "read"],
      ['AT+CPIN="{PIN}"', "輸入 SIM PIN；輸入錯誤會減少嘗試次數，建議改用概覽頁的解鎖功能", "danger"],
      ['AT+QPINC="SC"', "剩餘 PIN／PUK 嘗試次數", "read"],
      ["AT+QCCID", "ICCID", "read"],
      ["AT+CIMI", "IMSI", "read"],
      ["AT+CNUM", "本機號碼（僅 SIM 有儲存時）", "read"],
      ["AT+QSIMSTAT?", "SIM 插入偵測狀態", "read"],
      ["AT+QINISTAT", "SIM 初始化進度（7 為完成）", "read"],
    ] },
    { title: "網路與訊號", commands: [
      ["AT+CSQ", "訊號強度（RSSI）", "read"],
      ["AT+QCSQ", "依制式顯示詳細訊號（RSRP、SINR、RSRQ）", "read"],
      ['AT+QENG="servingcell"', "服務基地台：頻段、EARFCN、Cell ID、RSRP／RSRQ／SINR", "read"],
      ["AT+CEREG?", "LTE 註冊狀態（1 本地、5 漫遊）", "read"],
      ["AT+CREG?", "電路交換網路註冊狀態", "read"],
      ["AT+COPS?", "目前電信業者", "read"],
      ["AT+COPS=?", "搜尋可用電信業者；需要數分鐘，期間可能中斷連線", "write"],
      ["AT+QSPN", "網路名稱與 PLMN", "read"],
      ["AT+QNWINFO", "目前制式、PLMN 與頻段", "read"],
      ['AT+QCFG="nwscanmode"[,{0 | 1 | 2 | 3}]', "網路掃描模式：0 自動、1 僅 GSM、2 僅 WCDMA、3 僅 LTE", "write"],
    ] },
    { title: "數據連線", commands: [
      ["AT+CGATT?", "數據附著狀態", "read"],
      ["AT+CGDCONT?", "PDP 設定與 APN", "read"],
      ['AT+CGDCONT={cid},"{IP | IPV6 | IPV4V6}","{APN}"', "設定 PDP 與 APN；建議改用網路頁的 APN 設定", "write"],
      ["AT+CGACT?", "已啟用的 PDP", "read"],
      ["AT+CGPADDR={cid}", "指定 PDP 的 IP 位址", "read"],
      ['AT+QCFG="usbnet"[,{0 | 1 | 2 | 3}]', "USB 網卡模式：0 簡訊／行動寬頻、1 ECM、2／3 實驗；寫入後需重啟模組", "reboot"],
    ] },
    { title: "VoLTE／IMS", commands: [
      ['AT+QCFG="ims"[,{0 | 1}]', "IMS 設定；回應第二個數字為 1 代表已註冊；寫入後需重啟模組", "reboot"],
      ['AT+QCFG="volte_disable"[,{0 | 1}]', "停用 VoLTE（1 停用、0 允許）", "write"],
      ['AT+QMBNCFG="list"', "模組內的 MBN 設定檔清單", "read"],
      ['AT+QMBNCFG="select"', "目前使用的 MBN", "read"],
      ['AT+QMBNCFG="autosel"[,{0 | 1}]', "依 SIM 自動選擇 MBN", "write"],
    ] },
    { title: "簡訊", commands: [
      ["AT+CPMS?", "簡訊儲存區與使用量", "read"],
      ["AT+CSCA?", "簡訊中心號碼", "read"],
      ["AT+CMGL={0 | 1 | 2 | 3 | 4}", "列出 PDU 模式簡訊：0 未讀、1 已讀、2 未傳送、3 已傳送、4 全部", "read"],
      ["AT+CMGF={0 | 1}", "簡訊格式：0 PDU、1 文字；服務使用 PDU，改成 1 會影響收發", "write"],
      ["AT+CMGD={index}[,{0 | 1 | 2 | 3 | 4}]", "刪除簡訊；第二個參數會批次刪除，無法復原", "danger"],
    ] },
    { title: "通話", commands: [
      ["AT+CLCC", "目前通話列表", "read"],
      ["ATD{號碼};", "撥打電話（號碼後的分號代表語音通話）", "write"],
      ["ATA", "接聽來電", "write"],
      ["AT+CHUP", "掛斷所有通話", "write"],
      ["AT+VTS={0-9 | * | #}", "通話中送出按鍵音", "write"],
    ] },
    { title: "系統", commands: [
      ["AT+CFUN?", "功能模式", "read"],
      ["AT+CFUN={0 | 1 | 4}", "0 最小功能、1 完整功能、4 飛航模式（關閉射頻）", "write"],
      ["AT+CFUN=1,1", "重新啟動模組，USB 會短暫中斷", "reboot"],
      ['AT+QCFG="usbcfg"', "USB 組成設定（讀取）；寫入錯誤可能讓模組無法被電腦辨識", "read"],
      ["AT+QADBKEY?", "ADB 授權挑戰碼", "read"],
    ] },
  ];

  const list = document.querySelector("#at-reference-list");
  const search = document.querySelector("#at-reference-search");
  const input = document.querySelector("#at-command");
  if (!list || !search || !input) return;

  function fill(syntax) {
    input.value = syntax;
    input.focus();
    // Select the first {placeholder} so typing replaces it.
    const start = syntax.indexOf("{");
    const end = start >= 0 ? syntax.indexOf("}", start) : -1;
    if (start >= 0 && end > start) input.setSelectionRange(start, end + 1);
    else input.setSelectionRange(syntax.length, syntax.length);
    document.querySelector("#at").scrollIntoView({ behavior: "smooth", block: "start" });
  }

  function render() {
    const query = search.value.trim().toLowerCase();
    const sections = groups.map((group) => {
      const rows = group.commands.filter(([syntax, description]) =>
        !query || syntax.toLowerCase().includes(query) || description.toLowerCase().includes(query) || group.title.toLowerCase().includes(query));
      if (!rows.length) return null;
      const section = document.createElement("section");
      section.className = "at-reference-group";
      const heading = document.createElement("h3");
      heading.textContent = group.title;
      section.append(heading);
      rows.forEach(([syntax, description, risk]) => {
        const button = document.createElement("button");
        button.type = "button";
        button.className = "at-reference-item";
        button.title = "填入輸入框（不會自動執行）";
        const code = document.createElement("code");
        code.textContent = syntax;
        const text = document.createElement("span");
        text.textContent = description;
        const badge = document.createElement("small");
        badge.className = `at-risk at-risk-${RISK[risk].tone}`;
        badge.textContent = RISK[risk].label;
        button.append(code, badge, text);
        button.addEventListener("click", () => fill(syntax));
        section.append(button);
      });
      return section;
    }).filter(Boolean);
    if (!sections.length) {
      list.textContent = "找不到符合的指令";
      return;
    }
    list.replaceChildren(...sections);
  }

  search.addEventListener("input", render);
  render();
})();
