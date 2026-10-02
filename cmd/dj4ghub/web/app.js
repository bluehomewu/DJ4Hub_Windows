const $ = (selector) => document.querySelector(selector);
let lastSMSCount = null;
let esimHealthPollTimer = null;
let esimHealthInFlight = false;
let networkTrafficTimer = null;
let networkTrafficPrevious = null;
let networkTrafficInFlight = false;
let networkActivityTimer = null;
let networkActivityInFlight = false;
let networkActivityCountdown = 5;
let currentSIMPhoneNumber = "";
let simPhoneNumberRevealed = false;

function setThemePreference(theme) {
  if (theme === "light" || theme === "dark") {
    document.documentElement.dataset.theme = theme;
    localStorage.setItem("dj4ghub-theme", theme);
    localStorage.removeItem("djonehub-theme");
    localStorage.removeItem("vohive-theme");
  } else {
    delete document.documentElement.dataset.theme;
    localStorage.removeItem("dj4ghub-theme");
    localStorage.removeItem("djonehub-theme");
    localStorage.removeItem("vohive-theme");
  }
  document.querySelectorAll("[data-theme-option]").forEach((button) => {
    button.setAttribute("aria-pressed", String(button.dataset.themeOption === theme));
  });
}

const savedTheme = localStorage.getItem("dj4ghub-theme") || localStorage.getItem("djonehub-theme") || localStorage.getItem("vohive-theme");
setThemePreference(savedTheme === "light" || savedTheme === "dark" ? savedTheme : "auto");
document.querySelectorAll("[data-theme-option]").forEach((button) => {
  button.addEventListener("click", () => setThemePreference(button.dataset.themeOption));
});

const operatorNames = new Map([
  ["CHN-UNICOM", "中國聯通"],
  ["CHINA UNICOM", "中國聯通"],
  ["UNICOM", "中國聯通"],
  ["46001", "中國聯通"],
  ["46006", "中國聯通"],
  ["46009", "中國聯通"],
  ["CHINA MOBILE", "中國移動"],
  ["CMCC", "中國移動"],
  ["CHN-CMCC", "中國移動"],
  ["46000", "中國移動"],
  ["46002", "中國移動"],
  ["46004", "中國移動"],
  ["46007", "中國移動"],
  ["46008", "中國移動"],
  ["CHINA TELECOM", "中國電信"],
  ["CHN-CT", "中國電信"],
  ["CTCC", "中國電信"],
  ["46003", "中國電信"],
  ["46005", "中國電信"],
  ["46011", "中國電信"],
  ["CBN", "中國廣電"],
  ["CHN-CBN", "中國廣電"],
  ["CHINA BROADNET", "中國廣電"],
  ["46015", "中國廣電"],
]);

async function api(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: { "Content-Type": "application/json", ...(options.headers || {}) },
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
  return data;
}

function notice(message) {
  const el = $("#notice");
  el.textContent = message;
  el.classList.add("show");
  clearTimeout(notice.timer);
  notice.timer = setTimeout(() => el.classList.remove("show"), 2600);
}

let modalResolve = null;

function closeModal(result = null) {
  const modal = $("#app-modal");
  modal.hidden = true;
  document.body.classList.remove("modal-open");
  if (modalResolve) {
    const resolve = modalResolve;
    modalResolve = null;
    resolve(result);
  }
}

function showModal({ title, message = "", fields = [], confirmLabel = "確定", danger = false }) {
  if (modalResolve) closeModal(null);
  const modal = $("#app-modal");
  const messageElement = $("#modal-message");
  const fieldsElement = $("#modal-fields");
  const confirmButton = $("#modal-confirm");
  $("#modal-title").textContent = title;
  messageElement.textContent = message;
  messageElement.hidden = !message;
  fieldsElement.replaceChildren(...fields.map((field) => {
    const label = document.createElement("label");
    label.className = "modal-field";
    const caption = document.createElement("span");
    caption.textContent = field.label;
    const input = document.createElement("input");
    input.name = field.name;
    input.value = field.value || "";
    input.placeholder = field.placeholder || "";
    input.autocomplete = "off";
    if (field.required) input.required = true;
    label.append(caption, input);
    return label;
  }));
  confirmButton.textContent = confirmLabel;
  confirmButton.className = danger ? "danger modal-danger" : "";
  modal.hidden = false;
  document.body.classList.add("modal-open");
  const firstInput = fieldsElement.querySelector("input");
  setTimeout(() => (firstInput || confirmButton).focus(), 0);
  return new Promise((resolve) => { modalResolve = resolve; });
}

$("#modal-form").addEventListener("submit", (event) => {
  event.preventDefault();
  const values = {};
  event.currentTarget.querySelectorAll(".modal-fields input").forEach((input) => {
    values[input.name] = input.value.trim();
  });
  closeModal(values);
});
$("#modal-cancel").addEventListener("click", () => closeModal(null));
$("#modal-close").addEventListener("click", () => closeModal(null));
$("#app-modal").addEventListener("click", (event) => {
  if (event.target === event.currentTarget) closeModal(null);
});
document.addEventListener("keydown", (event) => {
  if (event.key === "Escape" && !$("#app-modal").hidden) closeModal(null);
});

async function copySMSCode(code) {
  try {
    await navigator.clipboard.writeText(code);
    notice(`驗證碼 ${code} 已複製`);
  } catch (error) {
    notice("複製失敗，請手動複製驗證碼");
  }
}

function renderHardwareDetails(status) {
  const panel = $("#hardware-details");
  const device = status.usb_device;
  if (!device) {
    panel.hidden = true;
    panel.replaceChildren();
    return;
  }

  const title = document.createElement("strong");
  title.textContent = device ? "已偵測到相容 USB 裝置" : "未偵測到可用硬體";

  const detail = document.createElement("p");
  if (device) {
    const interfaceText = Array.isArray(device.interfaces)
      ? `${device.interfaces.length} 個 USB interface`
      : "interface 未知";
    detail.textContent = [
      `${device.vendor || "相容裝置"} ${device.product || ""}`.trim(),
      `${device.vendor_id}:${device.product_id}`,
      device.mode,
      interfaceText,
    ].filter(Boolean).join(" · ");
  } else {
    detail.textContent = status.discovery_error || "裝置未列舉";
  }

  const hint = document.createElement("small");
  hint.textContent = status.discovery_error
    ? `目前限制：${status.discovery_error}`
    : "AT 串列埠可用後，簡訊和 eSIM/卡片操作會自動啟用。";

  panel.hidden = false;
  panel.replaceChildren(title, detail, hint);
}

function setSidebarDeviceState(connected, device = null) {
  const panel = $("#sidebar-device");
  panel.classList.toggle("is-offline", !connected);
  $("#sidebar-device-name").textContent = connected
    ? (device?.product || "4G 模組")
    : "等待裝置";
  $("#sidebar-device-state").textContent = connected ? "USB" : "未連線";
}

function setValue(id, text, tone = "") {
  const el = $(id);
  el.textContent = text || "--";
  el.className = tone;
}

function displayOperatorName(value) {
  const raw = String(value || "").trim();
  if (!raw) return "--";
  return operatorNames.get(raw.toUpperCase()) || raw;
}

function displayWorkMode(value) {
	if (value === null || value === undefined || value === "") {
	  return { label: "待讀取", tone: "muted" };
	}
  switch (Number(value)) {
    case 0: return { label: "簡訊模式", tone: "neutral" };
    case 1: return { label: "上網模式", tone: "neutral" };
    case 2: return { label: "實驗模式 2", tone: "warn" };
    case 3: return { label: "實驗模式 3", tone: "warn" };
    default: return { label: "待讀取", tone: "muted" };
  }
}

function setWorkModeControl(value) {
  const currentMode = value === null || value === undefined || value === "" ? -1 : Number(value);
  const smsButton = $("#workmode-sms");
  const networkButton = $("#workmode-network");
  smsButton.setAttribute("aria-pressed", currentMode === 0 ? "true" : "false");
  networkButton.setAttribute("aria-pressed", currentMode === 1 ? "true" : "false");
}

function setUSBNetModeSelector(value) {
  const currentMode = value === null || value === undefined || value === "" ? -1 : Number(value);
  [0, 1, 2, 3].forEach((mode) => {
    $("#usbnet-mode-" + mode).setAttribute("aria-pressed", currentMode === mode ? "true" : "false");
  });
}

function setHeaderDeviceState(connected, label = "裝置線上") {
  const indicator = $("#header-device-state");
  indicator.classList.toggle("is-online", connected);
  indicator.classList.toggle("is-offline", !connected);
  indicator.querySelector("span").textContent = label;
}

async function loadSidebarConnection() {
  const panel = $("#sidebar-connection");
  try {
    const connection = await api("/api/network/local");
    if (!connection?.interface) {
      panel.hidden = true;
      return;
    }
    $("#sidebar-connection-detail").textContent = [connection.interface, connection.ipv4].filter(Boolean).join(" · ");
    const state = $("#sidebar-connection-state");
    state.textContent = connection.is_default ? "Windows 出口" : "已連線";
    state.classList.toggle("is-secondary", !connection.is_default);
    panel.hidden = false;
  } catch (_) {
    panel.hidden = true;
  }
}

function signalTone(dbm) {
  const value = Number(dbm);
  if (!Number.isFinite(value) || value === 0) return "muted";
  if (value >= -65) return "good";
  if (value >= -75) return "signal-fair";
  if (value >= -85) return "warn";
  if (value >= -95) return "orange";
  return "bad";
}

function renderSIMPhoneNumber(value, simInserted) {
  const phoneNumber = String(value || "").trim();
  const empty = $("#sim-phone-empty");
  const actions = $("#sim-phone-actions");
  const toggle = $("#sim-phone-toggle");
  const copy = $("#sim-phone-copy");

  if (!phoneNumber) {
    currentSIMPhoneNumber = "";
    simPhoneNumberRevealed = false;
    empty.textContent = simInserted ? "SIM 未儲存號碼" : "卡片狀態";
    empty.hidden = false;
    actions.hidden = true;
    toggle.textContent = "--";
    copy.disabled = true;
    return;
  }

  if (phoneNumber !== currentSIMPhoneNumber) {
    currentSIMPhoneNumber = phoneNumber;
    simPhoneNumberRevealed = false;
  }
  empty.hidden = true;
  actions.hidden = false;
  toggle.textContent = simPhoneNumberRevealed ? phoneNumber : maskPhoneNumber(phoneNumber);
  toggle.title = simPhoneNumberRevealed ? "隱藏本機號碼" : "顯示完整本機號碼";
  copy.disabled = false;
}

async function loadStatus() {
  try {
    const status = await api("/api/status");
    const connected = Boolean(status.usb_device || status.imei || status.firmware);
    setHeaderDeviceState(connected, connected ? "裝置線上" : "等待裝置");
    setSidebarDeviceState(connected, status.usb_device);
    setValue("#operator", displayOperatorName(status.operator), status.operator ? "neutral" : "muted");
    setValue("#signal", status.signal_dbm ? `${status.signal_dbm} dBm` : "--", signalTone(status.signal_dbm));
    setValue("#network-mode", status.network_mode || status.reg_status_text || "--", status.network_mode ? "neutral" : "muted");
    setValue(
      "#sim",
      status.sim_inserted ? "已插入" : (status.usb_device ? "待讀取" : "未偵測到"),
      status.sim_inserted ? "good" : (status.usb_device ? "warn" : "bad"),
    );
    renderSIMPhoneNumber(status.phone_number, status.sim_inserted);
    const workMode = Object.prototype.hasOwnProperty.call(status, "usbnet_mode")
      ? displayWorkMode(status.usbnet_mode)
      : displayWorkMode(null);
    setValue("#work-mode", workMode.label, workMode.tone);
    setWorkModeControl(status.usbnet_mode);
    setUSBNetModeSelector(status.usbnet_mode);
    $("#device-summary").textContent = connected
      ? (status.hardware_status || [status.imei, status.firmware].filter(Boolean).join(" · ") || "模組初始化中")
      : "等待連線 4G 模組";
    renderHardwareDetails(status);
  } catch (error) {
    $("#device-summary").textContent = error.message;
    setHeaderDeviceState(false, "裝置離線");
    setSidebarDeviceState(false);
    renderSIMPhoneNumber("", false);
    setWorkModeControl(null);
  }
}

async function loadSMS() {
  const list = $("#sms-list");
  try {
    const [messages, status] = await Promise.all([
      api("/api/sms"),
      api("/api/sms/status"),
    ]);
    const pollText = status.polling
      ? `自動輪詢 ${status.poll_interval_s || 8}s`
      : "自動輪詢未啟用";
    const cleanupText = status.auto_cleanup_me ? "自動清理 ME 已開啟" : "自動清理 ME 未開啟";
    const errorText = status.last_poll_error ? ` · 最近錯誤：${status.last_poll_error}` : "";
    $("#sms-status").textContent = `目前快取 ${messages.length} 則簡訊 · ${pollText} · ${cleanupText}${errorText}`;
    if (lastSMSCount !== null && messages.length > lastSMSCount) {
      notice(`收到 ${messages.length - lastSMSCount} 則新簡訊`);
    }
    lastSMSCount = messages.length;
    if (!messages.length) {
      list.className = "list empty";
      list.textContent = "暫無簡訊";
      return;
    }
    list.className = "list";
    list.replaceChildren(...messages.map((message) => {
      const row = document.createElement("article");
      row.className = "item";
      const sender = document.createElement("strong");
      sender.textContent = message.sender || "未知號碼";
      const content = document.createElement("p");
      content.textContent = message.content;
      const time = document.createElement("time");
      time.textContent = new Date(message.timestamp).toLocaleString();
      if (message.code) {
        const actions = document.createElement("div");
        actions.className = "sms-actions";
        const badge = document.createElement("span");
        badge.className = "code-badge";
        badge.textContent = `驗證碼 ${message.code}`;
        const copy = document.createElement("button");
        copy.className = "secondary compact";
        copy.type = "button";
        copy.textContent = "複製";
        copy.addEventListener("click", () => copySMSCode(message.code));
        actions.append(badge, copy, time);
        row.append(sender, content, actions);
      } else {
        row.append(sender, content, time);
      }
      return row;
    }));
  } catch (error) {
    $("#sms-status").textContent = `讀取清單失敗：${error.message}`;
    notice(error.message);
  }
}

function profileRows(value) {
  const groups = Array.isArray(value) ? value : value?.profiles || [];
  return groups.flatMap((group) =>
    (group.profiles || []).map((profile) => ({ ...profile, aid: group.aid_hex || "" })),
  );
}

function profileDisplayName(profile) {
  return profile?.name || profile?.service_provider_name || profile?.iccid || "未命名 Profile";
}

function activeProfile(profiles) {
  return profiles.find((profile) => profile.state === 1) || null;
}

function maskIdentifier(value, keep = 4) {
  const text = String(value || "");
  if (text.length <= keep * 2) return text;
  return `${text.slice(0, keep)} ${"•".repeat(Math.max(4, text.length - keep * 2))} ${text.slice(-keep)}`;
}

function maskPhoneNumber(value) {
  const text = String(value || "").trim();
  const digitCount = [...text].filter((char) => /\d/.test(char)).length;
  if (digitCount <= 8) return text;
  let digitIndex = 0;
  return [...text].map((char) => {
    if (!/\d/.test(char)) return char;
    digitIndex += 1;
    return digitIndex > 4 && digitIndex <= digitCount - 4 ? "*" : char;
  }).join("");
}

async function copyIdentifier(value, label) {
  try {
    await navigator.clipboard.writeText(value);
    notice(`${label} 已複製`);
  } catch (error) {
    notice(`複製 ${label} 失敗，請手動複製`);
  }
}

async function editProfileNote(profile, note) {
  const values = await showModal({
    title: "編輯模組資料",
    message: "這些資料儲存在相容模組中，並按 ICCID 與目前 Profile 關聯。",
    confirmLabel: "儲存",
    fields: [
      { name: "label", label: "模組內名稱", value: note.label || "", placeholder: "可選" },
      { name: "phone", label: "模組號碼", value: note.phone || "", placeholder: "可選" },
      { name: "tags", label: "用途標籤", value: note.tags || "", placeholder: "例如：英國驗證碼" },
    ],
  });
  if (!values) return;
  try {
    await api("/api/esim/module-notes", {
      method: "PUT",
      body: JSON.stringify({ iccid: profile.iccid, label: values.label, phone: values.phone, tags: values.tags }),
    });
    notice("模組資料已儲存");
    await loadESIM();
  } catch (error) {
    notice(error.message);
  }
}

function phonebookCheck(label, ok, detail) {
  const card = document.createElement("div");
  card.className = `phonebook-check ${ok ? "ok" : ""}`;
  const title = document.createElement("strong");
  title.textContent = label;
  const text = document.createElement("small");
  text.textContent = detail;
  card.append(title, text);
  return card;
}

async function probeESIMPhonebook() {
  const button = $("#probe-esim-phonebook");
  const status = $("#esim-phonebook-status");
  const resultPanel = $("#esim-phonebook-result");
  button.disabled = true;
  status.textContent = "正在檢測卡內通訊錄能力，不會寫入聯絡人...";
  resultPanel.hidden = true;
  try {
    const result = await api("/api/esim/phonebook/probe", { method: "POST" });
    const supported = result.storage_supported && result.storage_selected;
    const portable = supported && result.read_supported && result.write_supported;
    status.textContent = portable
      ? "已確認目前 Profile 支援卡內通訊錄讀寫；尚未寫入任何聯絡人。"
      : "目前 Profile 未完整確認卡內通訊錄讀寫能力；不會進行寫入。";
    resultPanel.replaceChildren(
      phonebookCheck("SIM 通訊錄", result.storage_supported, result.storage_supported ? "支援 SM 卡儲存" : "未發現 SM 卡儲存"),
      phonebookCheck("目前卡片", result.storage_selected, result.storage_selected ? "已安全選取 SM 儲存" : "無法選取 SM 儲存"),
      phonebookCheck("讀取能力", result.read_supported, result.read_supported ? "模組支援讀取卡內聯絡人" : "模組未確認讀取指令"),
      phonebookCheck("寫入介面", result.write_supported, result.write_supported ? "模組宣告支援寫入介面" : "模組未確認寫入指令"),
      phonebookCheck("目前狀態", supported, result.storage_status || "未回傳容量資訊"),
    );
    resultPanel.hidden = false;
  } catch (error) {
    status.textContent = `通訊錄檢測失敗：${error.message}`;
  } finally {
    button.disabled = false;
  }
}

function esimEIDRows(value) {
  const eids = value?.chip_info?.eids;
  return Array.isArray(eids) ? eids : [];
}

function renderESIMChip(overview) {
  const panel = $("#esim-chip");
  const chip = overview?.chip_info || {};
  const eids = esimEIDRows(overview);
  if (!chip.sku_name && !chip.serial_number && !chip.firmware && !eids.length) {
    panel.hidden = true;
    panel.replaceChildren();
    return;
  }
  panel.hidden = false;
  panel.replaceChildren(
    diagnosticCard("卡片類型", chip.sku_name || "eUICC/eSIM 卡片"),
    diagnosticCard("韌體", chip.firmware || "--", chip.serial_number ? `序號 ${chip.serial_number}` : ""),
    diagnosticCard("EID", eids.map((item) => item.eid).filter(Boolean).join(" · ") || "--"),
  );
}

function renderESIMEIDList(overview) {
  const eids = esimEIDRows(overview);
  if (!eids.length) return [];
  return eids.map((item) => {
    const row = document.createElement("article");
    row.className = "item esim-info-row";
    const name = document.createElement("strong");
    name.textContent = "已識別 eUICC";
    const detail = document.createElement("p");
    detail.textContent = [
      item.eid ? `EID ${item.eid}` : "",
      item.aid ? `AID ${item.aid}` : "",
      item.free_nvram ? `可用空間 ${item.free_nvram}` : "",
      item.firmware ? `韌體 ${item.firmware}` : "",
    ].filter(Boolean).join("\n");
    const status = document.createElement("small");
    status.textContent = item.spec || item.spec_guess || "eSIM";
    row.append(name, detail, status);
    return row;
  });
}

function renderESIMEIDPanel(rows) {
  if (!rows.length) return null;
  const panel = document.createElement("details");
  panel.className = "esim-euicc-panel";
  const heading = document.createElement("summary");
  heading.className = "esim-euicc-heading";
  const title = document.createElement("strong");
  title.textContent = "已識別 eUICC";
  const hint = document.createElement("small");
  hint.textContent = rows.length > 1 ? `${rows.length} 張 eSIM 卡片` : "卡片資訊";
  heading.append(title, hint);
  panel.append(heading, ...rows);
  return panel;
}

async function loadESIMHealth() {
  if (esimHealthInFlight) return;
  esimHealthInFlight = true;
  const section = $("#esim-runtime-section");
  const panel = $("#esim-runtime");
  section.hidden = false;
  panel.replaceChildren(diagnosticCard("Profile 檢查", "正在檢測"));
  try {
    const health = await api("/api/esim/health");
    if (health.card_type === "physical_sim") {
      section.hidden = true;
      return;
    }
    if (!health.active_profile) {
      panel.replaceChildren(diagnosticCard("Profile 檢查", health.message || "未發現已啟用 Profile"));
      return;
    }
    const profile = health.active_profile;
    const signal = Number.isFinite(health.signal_dbm) ? `${health.signal_dbm} dBm` : "--";
    panel.replaceChildren(
      diagnosticCard("目前啟用", profileDisplayName(profile), profile.iccid ? `ICCID ${maskIdentifier(profile.iccid)}` : ""),
      diagnosticCard("模組實際卡", health.module_iccid ? maskIdentifier(health.module_iccid) : "--", health.imsi ? `IMSI ${health.imsi}` : ""),
      diagnosticCard("行動網路註冊", health.registration || "未註冊", [displayOperatorName(health.operator), health.network_mode].filter(Boolean).join(" · ")),
      diagnosticCard("訊號", signal, health.registered ? "模組已接管目前 Profile" : "等待網路註冊"),
    );
  } catch (error) {
    panel.replaceChildren(diagnosticCard("Profile 檢查", "暫時無法讀取", error.message));
  } finally {
    esimHealthInFlight = false;
  }
}

function setESIMHealthPolling(enabled) {
  clearInterval(esimHealthPollTimer);
  esimHealthPollTimer = null;
  if (!enabled) return;
  esimHealthPollTimer = setInterval(() => {
    if ($("#esim").classList.contains("active")) void loadESIMHealth();
  }, 30000);
}

function showESIMCardState(title, detail, tone = "") {
  const panel = $("#esim-card-state");
  panel.className = `esim-card-state${tone ? ` ${tone}` : ""}`;
  $("#esim-card-state-title").textContent = title;
  $("#esim-card-state-detail").textContent = detail;
  panel.hidden = false;
}

function hideESIMCardState() {
  $("#esim-card-state").hidden = true;
}

function diagnosticCard(label, value, detail = "") {
  const card = document.createElement("div");
  card.className = "diagnostic-card";
  const span = document.createElement("span");
  span.textContent = label;
  const strong = document.createElement("strong");
  strong.textContent = value || "--";
  card.append(span, strong);
  if (detail) {
    const small = document.createElement("small");
    small.textContent = detail;
    card.append(small);
  }
  return card;
}

function networkPathStep(label, value, detail = "", tone = "") {
  const step = document.createElement("div");
  step.className = `network-path-step ${tone}`.trim();
  const labelNode = document.createElement("span");
  labelNode.textContent = label;
  const valueNode = document.createElement("strong");
  valueNode.textContent = value || "--";
  const detailNode = document.createElement("small");
  detailNode.textContent = detail;
  step.append(labelNode, valueNode, detailNode);
  return step;
}

function networkFact(label, value) {
  const item = document.createElement("div");
  const term = document.createElement("dt");
  term.textContent = label;
  const description = document.createElement("dd");
  description.textContent = value || "--";
  item.append(term, description);
  return item;
}

function renderNetworkCheck(label, result) {
  const list = $("#network-checks");
  list.className = "list";
  const row = document.createElement("article");
  row.className = `item check-item ${result.ok ? "ok" : "bad"}`;
  const name = document.createElement("strong");
  name.textContent = label;
  const detail = document.createElement("p");
  detail.textContent = result.detail || result.summary || "";
  const status = document.createElement("small");
  status.textContent = result.ok ? "通過" : "未通過";
  row.append(name, detail, status);
  const existing = [...list.querySelectorAll(".item")].filter((item) => item.dataset.label !== label);
  row.dataset.label = label;
  list.replaceChildren(row, ...existing);
}

async function runNetworkCheck(label, path, button) {
  button.disabled = true;
  try {
    const result = await api(path, { method: "POST" });
    renderNetworkCheck(label, result);
    notice(result.summary || "檢測完成");
  } catch (error) {
    renderNetworkCheck(label, { ok: false, summary: "檢測失敗", detail: error.message });
    notice(error.message);
  } finally {
    button.disabled = false;
  }
}

function renderNetworkRecovery(diag) {
  const panel = $("#network-recovery");
  const service = diag.network_service;
  if (!service || diag.usb_network_ready) {
    panel.hidden = true;
    return;
  }

  const identity = [service.name, service.hardware_port].filter(Boolean).join(" · ");
  const disabled = Boolean(service.disabled);
  const wwan = service.kind === "wwan";
  $("#network-recovery-title").textContent = disabled
    ? "模組網卡已在 Windows 中停用"
    : (wwan ? "Windows 行動寬頻尚未連線" : "模組網卡尚未取得位址");
  $("#network-recovery-detail").textContent = disabled
    ? `${identity} 已存在，但處於停用狀態。啟用需要 Windows 管理員授權。`
    : (wwan
      ? `${identity} 已識別，但目前沒有可用 IPv4 位址。可以讓 Windows 使用已儲存的設定檔連線。`
      : `${identity} 已識別，但目前沒有可用 IPv4 位址。請檢查模組撥號狀態或重新插拔。`);
  $("#enable-network-service").textContent = disabled ? "啟用網卡" : (wwan ? "連線行動寬頻" : "重新檢查");
  panel.hidden = false;
}

async function loadNetwork() {
  const grid = $("#network-grid");
  const ifaceList = $("#network-interfaces");
  $("#network-status").textContent = "正在讀取網路診斷...";
  try {
    const diag = await api("/api/network");
    setUSBNetModeSelector(diag.usbnet_mode);
    const active = Array.isArray(diag.active_contexts) ? diag.active_contexts.join(", ") : "";
    const apns = Array.isArray(diag.pdp_contexts)
      ? diag.pdp_contexts.map((ctx) => `${ctx.id}:${ctx.apn}`).join(" · ")
      : "";
    const addresses = Array.isArray(diag.pdp_addresses) ? diag.pdp_addresses.join(" · ") : "";
    const usb = diag.usb_device
      ? `${diag.usb_device.vendor || ""} ${diag.usb_device.product || ""} (${diag.usb_device.vendor_id}:${diag.usb_device.product_id})`
      : "未偵測到";
    const service = diag.network_service;
    let usbNetworkValue = "未識別";
    let usbNetworkDetail = "Windows 網路介面";
    let usbNetworkTone = "is-bad";
    if (service?.disabled) {
      usbNetworkValue = "網卡已停用";
      usbNetworkDetail = [service.name, service.device].filter(Boolean).join(" · ");
    } else if (diag.usb_network_ready) {
      usbNetworkValue = service?.device ? `${service.device} 已連線` : "已連線";
      usbNetworkDetail = service?.ipv4 || "已取得位址";
      usbNetworkTone = "is-good";
    } else if (diag.usb_network_present) {
      usbNetworkValue = service?.kind === "wwan" ? "行動寬頻未連線" : "等待位址";
      usbNetworkDetail = service?.device || "Windows 已識別介面";
      usbNetworkTone = "is-warn";
    }
    const route = diag.default_route || {};
    const routeText = route.interface || "未知";
    const routeUsesUSB = Boolean(service?.device && route.interface === service.device);
    let routeDetail = route.gateway ? `閘道 ${route.gateway}` : "Windows 目前預設路由";
    if (routeUsesUSB) {
      routeDetail += " · 網際網路尚未驗證";
    }
    const path = document.createElement("div");
    path.className = "network-path";
    path.append(
      networkPathStep("行動數據", active ? `已啟用 ${active}` : "未啟用", addresses || "等待分配行動網路 IP", active ? "is-good" : "is-warn"),
      networkPathStep("模組網卡", usbNetworkValue, usbNetworkDetail, usbNetworkTone),
      networkPathStep("Windows 出口", routeText, routeDetail, routeUsesUSB ? "is-good" : "is-warn"),
    );
    const facts = document.createElement("dl");
    facts.className = "network-facts";
    facts.append(
      networkFact("USBNET", diag.usbnet_mode ?? "未知"),
      networkFact("APN", apns || "無"),
      networkFact("Windows 網卡", service ? `${service.name} · ${service.disabled ? "已停用" : (service.kind === "wwan" ? "行動寬頻" : "乙太網路")}` : "未識別"),
      networkFact("USB 裝置", usb),
    );
    grid.className = "network-summary";
    grid.replaceChildren(path, facts);

    const errorText = diag.errors ? ` · 錯誤：${Object.values(diag.errors).join("；")}` : "";
    if (service?.disabled) {
      $("#network-status").textContent = `模組網卡已在 Windows 中停用${errorText}`;
    } else if (diag.usb_network_ready) {
      $("#network-status").textContent = `模組網卡已連線並取得位址 · 網際網路尚未驗證${errorText}`;
    } else if (diag.usb_network_present) {
      $("#network-status").textContent = `Windows 已識別模組網卡，但尚未連線${errorText}`;
    } else {
      const driverHint = diag.usb_device?.driver_issues?.length ? `（${diag.usb_device.driver_issues.join("；")}）` : "，請確認已安裝對應網卡驅動";
      $("#network-status").textContent = `行動網路端可能已通，但 Windows 尚未識別模組網卡${driverHint}${errorText}`;
    }
    renderNetworkRecovery(diag);

    const interfaces = Array.isArray(diag.host_interfaces) ? diag.host_interfaces : [];
    if (!interfaces.length) {
      ifaceList.className = "list empty";
      ifaceList.textContent = "未讀取到網路介面";
      return;
    }
    ifaceList.className = "list";
    ifaceList.replaceChildren(...interfaces.map((item) => {
      const row = document.createElement("article");
      row.className = "item";
      const name = document.createElement("strong");
      name.textContent = item.name;
      const detail = document.createElement("p");
      detail.textContent = [item.description, item.kind, item.ipv4].filter(Boolean).join(" · ");
      const status = document.createElement("small");
      status.textContent = item.status === "active" ? "active" : "inactive";
      row.append(name, detail, status);
      return row;
    }));
  } catch (error) {
    $("#network-status").textContent = `讀取網路診斷失敗：${error.message}`;
    grid.className = "network-summary network-summary-empty";
    grid.textContent = "網路摘要暫不可用";
    ifaceList.className = "list empty";
    ifaceList.textContent = "讀取失敗";
    $("#network-recovery").hidden = true;
    notice(error.message);
  }
}

async function enableNetworkService() {
  const confirmed = await showModal({
    title: "處理模組網卡",
    message: "停用的網卡將請求管理員授權後啟用；行動寬頻會使用 Windows 已儲存的設定檔連線。",
    confirmLabel: "繼續",
  });
  if (!confirmed) return;
  const button = $("#enable-network-service");
  const originalLabel = button.textContent;
  button.disabled = true;
  button.textContent = "正在處理…";
  try {
    const result = await api("/api/network/enable-service", { method: "POST" });
    notice(result.summary || "網路服務已處理");
    await Promise.all([loadNetwork(), loadSidebarConnection()]);
  } catch (error) {
    notice(error.message);
  } finally {
    button.disabled = false;
    button.textContent = originalLabel;
  }
}

function formatTrafficBytes(value) {
  const bytes = Math.max(0, Number(value || 0));
  const units = ["B", "KB", "MB", "GB", "TB"];
  let amount = bytes;
  let unit = 0;
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024;
    unit += 1;
  }
  const digits = unit === 0 ? 0 : (amount >= 100 ? 0 : amount >= 10 ? 1 : 2);
  return `${amount.toFixed(digits)} ${units[unit]}`;
}

async function loadNetworkTraffic() {
  if (networkTrafficInFlight) return;
  networkTrafficInFlight = true;
  try {
    const sample = await api("/api/network/traffic");
    if (!sample.available) {
      networkTrafficPrevious = null;
      setValue("#traffic-rx-rate", "--", "muted");
      setValue("#traffic-tx-rate", "--", "muted");
      setValue("#traffic-session-rx", "--", "muted");
      setValue("#traffic-session-tx", "--", "muted");
      setValue("#traffic-session-total", "--", "muted");
      return;
    }

    let rxRate = 0;
    let txRate = 0;
    const previous = networkTrafficPrevious;
    if (previous && previous.interface === sample.interface) {
      const elapsed = (Number(sample.sampled_at_ms) - Number(previous.sampled_at_ms)) / 1000;
      if (elapsed > 0) {
        rxRate = Math.max(0, Number(sample.rx_bytes) - Number(previous.rx_bytes)) / elapsed;
        txRate = Math.max(0, Number(sample.tx_bytes) - Number(previous.tx_bytes)) / elapsed;
      }
    }
    networkTrafficPrevious = sample;
    setValue("#traffic-rx-rate", `${formatTrafficBytes(rxRate)}/s`, "neutral");
    setValue("#traffic-tx-rate", `${formatTrafficBytes(txRate)}/s`, "neutral");
    setValue("#traffic-session-rx", formatTrafficBytes(sample.session_rx_bytes), "neutral");
    setValue("#traffic-session-tx", formatTrafficBytes(sample.session_tx_bytes), "neutral");
    setValue("#traffic-session-total", formatTrafficBytes(sample.session_total_bytes), "neutral");
    $("#traffic-session-total").title = "本次啟動期間的下載與上傳流量之和；關閉 DJ 4G Hub 後歸零";
  } catch (error) {
    setValue("#traffic-rx-rate", "--", "muted");
    setValue("#traffic-tx-rate", "--", "muted");
    setValue("#traffic-session-total", "--", "muted");
  } finally {
    networkTrafficInFlight = false;
  }
}

function setNetworkTrafficPolling(enabled) {
  clearInterval(networkTrafficTimer);
  networkTrafficTimer = null;
  if (!enabled) {
    networkTrafficPrevious = null;
    return;
  }
  void loadNetworkTraffic();
  networkTrafficTimer = setInterval(loadNetworkTraffic, 1000);
}

function activityInitials(value) {
  return String(value || "?").trim().slice(0, 2).toUpperCase();
}

function renderNetworkActivityCountdown() {
  const label = $("#activity-updated");
  if (!label) return;
  label.textContent = networkActivityInFlight
    ? "正在重新整理…"
    : `${networkActivityCountdown} 秒後重新整理`;
}

async function loadNetworkActivity() {
  if (networkActivityInFlight) return;
  networkActivityInFlight = true;
  renderNetworkActivityCountdown();
  const list = $("#activity-list");
  try {
    const snapshot = await api("/api/network/activity");
    if (!snapshot.available) {
      $("#activity-physical").textContent = "未偵測到 4G 網卡";
      $("#activity-tunnel").textContent = "--";
      $("#activity-count").textContent = "0 個連線";
      list.className = "activity-list empty";
      list.textContent = "目前沒有可顯示的 4G 連網活動";
      return;
    }
    $("#activity-physical").textContent = `${snapshot.physical_interface}${snapshot.physical_ipv4 ? ` · ${snapshot.physical_ipv4}` : ""}${snapshot.physical_active ? " · 使用中" : ""}`;
    $("#activity-tunnel").textContent = snapshot.tunnel_interface || "直接連線";
    const connections = Array.isArray(snapshot.connections) ? snapshot.connections : [];
    $("#activity-count").textContent = `${connections.length} 個連線`;
    if (!connections.length) {
      list.className = "activity-list empty";
      list.textContent = "目前沒有使用中的應用連線";
      return;
    }
    list.className = "activity-list";
    list.replaceChildren(...connections.map((connection) => {
      const row = document.createElement("article");
      row.className = "activity-row";
      const app = document.createElement("div");
      app.className = "activity-app";
      const glyph = document.createElement("i");
      glyph.textContent = activityInitials(connection.process);
      const process = document.createElement("strong");
      process.textContent = connection.process || "系統";
      app.append(glyph, process);
      const target = document.createElement("div");
      target.className = "activity-target";
      const host = document.createElement("strong");
      host.textContent = connection.host || connection.ip;
      const detail = document.createElement("small");
      detail.textContent = connection.ip
        ? `${connection.ip}${connection.port ? `:${connection.port}` : ""}`
        : (connection.port ? `埠 ${connection.port}` : "目標主機");
      target.append(host, detail);
      const protocol = document.createElement("span");
      protocol.className = "activity-protocol";
      protocol.textContent = connection.protocol || "IP";
      const bytes = document.createElement("span");
      bytes.className = "activity-bytes";
      bytes.textContent = Number(connection.rx_bytes) || Number(connection.tx_bytes)
        ? `↓ ${formatTrafficBytes(connection.rx_bytes)} · ↑ ${formatTrafficBytes(connection.tx_bytes)}`
        : (connection.state || "--");
      row.append(app, target, protocol, bytes);
      return row;
    }));
  } catch (error) {
    list.className = "activity-list empty";
    list.textContent = `連網活動讀取失敗：${error.message}`;
  } finally {
    networkActivityInFlight = false;
    networkActivityCountdown = 5;
    renderNetworkActivityCountdown();
  }
}

function setNetworkActivityPolling(enabled) {
  clearInterval(networkActivityTimer);
  networkActivityTimer = null;
  if (!enabled) return;
  networkActivityCountdown = 5;
  void loadNetworkActivity();
  networkActivityTimer = setInterval(() => {
    if (networkActivityInFlight) {
      renderNetworkActivityCountdown();
      return;
    }
    networkActivityCountdown -= 1;
    if (networkActivityCountdown <= 0) {
      void loadNetworkActivity();
      return;
    }
    renderNetworkActivityCountdown();
  }, 1000);
}

async function setUSBNetMode(mode) {
  const label = `模式 ${mode}`;
  const confirmed = await showModal({
    title: `切換到${label}`,
    message: `將寫入 usbnet=${mode}，重啟模組後生效。`,
    confirmLabel: "繼續切換",
  });
  if (!confirmed) return;
  try {
    const result = await api("/api/network/usbnet", {
      method: "POST",
      body: JSON.stringify({ mode }),
    });
    notice(`usbnet 已寫入 ${result.mode}，請重啟模組`);
    await loadNetwork();
  } catch (error) {
    notice(error.message);
  }
}

function wait(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

async function waitForUSBNetwork(status, timeoutMilliseconds = 60000) {
  const startedAt = Date.now();
  let lastState = "等待模組重新列舉";
  while (Date.now() - startedAt < timeoutMilliseconds) {
    try {
      const diag = await api("/api/network");
      setUSBNetModeSelector(diag.usbnet_mode);
      if (diag.usb_network_ready) return diag;
      if (diag.network_service?.disabled) {
        lastState = `${diag.network_service.name} 已識別，但網卡在 Windows 中處於停用狀態`;
      } else if (diag.usb_network_present) {
        lastState = "模組網卡已出現，正在等待位址";
      } else if (diag.usb_device) {
        lastState = "模組已重新連線，正在等待模組網卡出現";
      } else {
        lastState = "USB 正在重新列舉";
      }
    } catch (error) {
      lastState = `等待裝置恢復：${error.message}`;
    }
    const secondsLeft = Math.max(1, Math.ceil((timeoutMilliseconds - (Date.now() - startedAt)) / 1000));
    status.textContent = `${lastState} · 最長等待 ${secondsLeft}s`;
    await wait(2500);
  }
  throw new Error(`${lastState}，未能在 60 秒內完成上網準備`);
}

async function switchWorkMode(mode, label, button) {
  if (button.getAttribute("aria-pressed") === "true") {
    notice(`目前已是${label}`);
    return;
  }
  const confirmed = await showModal({
    title: `切換到${label}`,
    message: mode === 1
      ? "將寫入 usbnet=1（ECM）並重啟模組，需要已安裝 ECM 驅動。Windows 在簡訊模式下已可透過 Quectel NDIS/MBIM 驅動的行動寬頻上網，通常無需切換。"
      : `將寫入 usbnet=${mode} 並重啟模組，USB 會短暫中斷。`,
    confirmLabel: "確認切換",
  });
  if (!confirmed) return;
  const status = $("#workmode-status");
  const buttons = [$("#workmode-sms"), $("#workmode-network")];
  buttons.forEach((item) => { item.disabled = true; });
  status.hidden = false;
  status.textContent = `正在切到${label}...`;
  try {
    const result = await api("/api/network/usbnet", {
      method: "POST",
      body: JSON.stringify({ mode }),
    });
    status.textContent = `usbnet 已寫入 ${result.mode}，正在重啟模組...`;
    await api("/api/network/reboot-module", { method: "POST" });
    if (mode === 1) {
      status.textContent = "上網模式已寫入，等待 USB 網卡和 DHCP...";
      notice("正在準備上網模式");
      const diag = await waitForUSBNetwork(status);
      const interfaceName = diag.network_service?.device || "模組網卡";
      status.textContent = `${interfaceName} 已取得位址，正在驗證網際網路...`;
      const connectivity = await api("/api/network/check-4g", { method: "POST" });
      renderNetworkCheck("4G 網際網路", connectivity);
      await Promise.all([loadStatus(), loadNetwork(), loadSidebarConnection()]);
      if (connectivity.ok) {
        status.textContent = `上網模式已就緒 · ${connectivity.summary}`;
        notice("上網模式已就緒");
      } else {
        status.textContent = `上網模式已切換，但${connectivity.summary}`;
        notice(connectivity.summary);
      }
    } else {
      status.textContent = `${label}已寫入，等待模組重新列舉後自動重新整理。`;
      notice(`${label}切換中`);
      await wait(9000);
      await Promise.all([loadStatus(), loadNetwork(), loadSidebarConnection()]);
      status.textContent = `${label}切換完成。`;
    }
  } catch (error) {
    status.hidden = false;
    status.textContent = `${label}切換失敗：${error.message}`;
    notice(error.message);
  } finally {
    buttons.forEach((item) => { item.disabled = false; });
  }
}

async function rebootModule() {
  const confirmed = await showModal({
    title: "重啟模組",
    message: "模組會重新列舉 USB，網頁可能短暫中斷。",
    confirmLabel: "確認重啟",
  });
  if (!confirmed) return;
  try {
    await api("/api/network/reboot-module", { method: "POST" });
    notice("模組正在重啟，稍後重新整理狀態");
    setTimeout(loadStatus, 8000);
    setTimeout(loadNetwork, 12000);
  } catch (error) {
    notice(error.message);
  }
}

async function loadESIM() {
  const list = $("#esim-list");
  const status = $("#esim-status");
  const download = $("#esim-download-section");
  const runtime = $("#esim-runtime-section");
  const profilePanel = $("#esim-profile-panel");
  const phonebook = $("#esim-phonebook-section");
  $("#esim-chip").hidden = true;
  $("#esim-chip").replaceChildren();
  runtime.hidden = true;
  download.hidden = true;
  profilePanel.hidden = true;
  phonebook.hidden = true;
  list.className = "list empty";
  list.textContent = "正在讀取 eUICC";
  status.textContent = "正在透過 AT+CCHO/CGLA 讀取 eUICC/eSIM 卡片";
  showESIMCardState("正在識別卡片", "正在確認目前卡片是否支援 eUICC Profile 管理。");
  try {
    const overview = await api("/api/esim");
    if (overview.card_type === "physical_sim") {
      status.textContent = overview.message;
      list.textContent = overview.message;
      showESIMCardState(
        "目前是實體 SIM 卡",
        "簡訊與行動上網功能可以正常使用；這張卡不包含可管理的 eUICC Profile。",
        "is-physical",
      );
      setESIMHealthPolling(false);
      return;
    }
    hideESIMCardState();
    download.hidden = false;
    profilePanel.hidden = false;
    phonebook.hidden = false;
    const notesResponse = await api("/api/esim/module-notes");
    const notes = notesResponse.notes || {};
    const profiles = profileRows(overview);
    const eidRows = renderESIMEIDList(overview);
    const eidPanel = renderESIMEIDPanel(eidRows);
    renderESIMChip(overview);
    const profileCount = profiles.length;
    const eidCount = esimEIDRows(overview).length;
    const active = activeProfile(profiles);
    status.textContent = active
      ? `已讀取：${eidCount} 個 eUICC，${profileCount} 個 Profile · 目前使用 ${profileDisplayName(active)}`
      : `已讀取：${eidCount} 個 eUICC，${profileCount} 個 Profile · 未發現已啟用 Profile`;
    if (!profiles.length) {
      if (eidRows.length) {
        list.className = "list";
        list.replaceChildren(eidPanel);
        return;
      }
      list.textContent = "未發現 eUICC/eSIM 卡片參數";
      return;
    }
    list.className = "list";
    const profileItems = profiles.map((profile) => {
      const note = notes[profile.iccid] || {};
      const row = document.createElement("article");
      row.className = `item esim-profile ${profile.state === 1 ? "active" : ""}`;
      const name = document.createElement("strong");
      name.textContent = note.label || profileDisplayName(profile);
      const detail = document.createElement("p");
      detail.textContent = [
        note.label && note.label !== profileDisplayName(profile) ? `卡內名稱：${profileDisplayName(profile)}` : "",
        profile.service_provider_name ? `服務商：${profile.service_provider_name}` : "",
        profile.class_text ? `類型：${profile.class_text}` : "",
        note.tags ? `標籤：${note.tags}` : "",
      ].filter(Boolean).join("\n");
      const metadata = document.createElement("div");
      metadata.className = "profile-metadata";
      if (note.phone) {
        const phoneRow = document.createElement("div");
        phoneRow.className = "profile-identifier-row";
        const phone = document.createElement("code");
        phone.className = "profile-iccid";
        phone.textContent = `模組號碼 ${maskPhoneNumber(note.phone)}`;
        const revealPhone = document.createElement("button");
        revealPhone.className = "secondary compact profile-toggle-button";
        revealPhone.type = "button";
        revealPhone.textContent = "顯示";
        revealPhone.addEventListener("click", () => {
          const hidden = revealPhone.textContent === "顯示";
          phone.textContent = `模組號碼 ${hidden ? note.phone : maskPhoneNumber(note.phone)}`;
          revealPhone.textContent = hidden ? "隱藏" : "顯示";
        });
        const copyPhone = document.createElement("button");
        copyPhone.className = "secondary compact profile-copy-button";
        copyPhone.type = "button";
        copyPhone.textContent = "複製號碼";
        copyPhone.addEventListener("click", () => copyIdentifier(note.phone, "模組號碼"));
        phoneRow.append(phone, revealPhone, copyPhone);
        metadata.append(phoneRow);
      }
      if (profile.iccid) {
        const iccidRow = document.createElement("div");
        iccidRow.className = "profile-identifier-row";
        const iccid = document.createElement("code");
        iccid.className = "profile-iccid";
        iccid.textContent = `ICCID ${maskIdentifier(profile.iccid)}`;
        const reveal = document.createElement("button");
        reveal.className = "secondary compact profile-toggle-button";
        reveal.type = "button";
        reveal.textContent = "顯示";
        reveal.addEventListener("click", () => {
          const hidden = reveal.textContent === "顯示";
          iccid.textContent = `ICCID ${hidden ? profile.iccid : maskIdentifier(profile.iccid)}`;
          reveal.textContent = hidden ? "隱藏" : "顯示";
        });
        const copy = document.createElement("button");
        copy.className = "secondary compact profile-copy-button";
        copy.type = "button";
        copy.textContent = "複製 ICCID";
        copy.addEventListener("click", () => copyIdentifier(profile.iccid, "ICCID"));
        iccidRow.append(iccid, reveal, copy);
        metadata.append(iccidRow);
      }
      const actionBox = document.createElement("div");
      actionBox.className = "profile-actions";
      if (profile.state !== 1) {
        const button = document.createElement("button");
        button.className = "compact";
        button.textContent = "啟用";
        button.addEventListener("click", async () => {
          const label = profileDisplayName(profile);
          const confirmed = await showModal({
            title: "啟用 Profile",
            message: `確定啟用 ${label} 嗎？目前正在使用的 eSIM Profile 會被切換。`,
            confirmLabel: "啟用",
          });
          if (!confirmed) {
            return;
          }
          button.disabled = true;
          button.textContent = "切換中";
          try {
            const result = await api("/api/esim/switch", {
              method: "POST",
              body: JSON.stringify({ iccid: profile.iccid, aid: profile.aid || "" }),
            });
            if (result.module_reboot_requested) {
              status.textContent = `已切換到 ${label}；模組正在重啟，等待新 Profile 接管（約 ${result.reconnect_wait_seconds || 10} 秒）`;
              notice(`已切換 ${label}，模組正在重新讀取新卡`);
              setTimeout(async () => {
                await loadESIM();
                await loadStatus();
              }, (result.reconnect_wait_seconds || 10) * 1000);
            } else {
              status.textContent = `Profile 已切換到 ${label}，但模組重啟未確認：${result.module_reboot_warning || "請手動重啟後再讀取號碼"}`;
              notice("Profile 已切換，模組重啟未確認");
              await loadESIM();
            }
          } catch (error) {
            status.textContent = `切換失敗：${error.message}`;
            notice(error.message);
            button.disabled = false;
            button.textContent = "啟用";
          }
        });
        actionBox.append(button);
      } else {
        const button = document.createElement("button");
        button.className = "secondary compact";
        button.type = "button";
        button.textContent = "啟用";
        button.disabled = true;
        actionBox.append(button);
      }
      const rename = document.createElement("button");
      rename.className = "secondary compact";
      rename.type = "button";
      rename.textContent = "改名";
      rename.addEventListener("click", async () => {
        const values = await showModal({
          title: "修改 Profile 名稱",
          message: "名稱將寫入 eUICC 卡片內部的 Profile nickname。",
          confirmLabel: "儲存",
          fields: [{ name: "name", label: "Profile 名稱", value: profileDisplayName(profile), required: true }],
        });
        if (!values?.name) return;
        rename.disabled = true;
        try {
          await api("/api/esim/profile", { method: "PATCH", body: JSON.stringify({ iccid: profile.iccid, aid: profile.aid || "", name: values.name }) });
          notice("Profile 名稱已修改");
          await loadESIM();
        } catch (error) { notice(error.message); } finally { rename.disabled = false; }
      });
      const localNote = document.createElement("button");
      localNote.className = "secondary compact";
      localNote.type = "button";
      localNote.textContent = "模組資料";
      localNote.addEventListener("click", () => editProfileNote(profile, note));
      const remove = document.createElement("button");
      remove.className = "secondary danger compact";
      remove.type = "button";
      remove.textContent = "刪除";
      remove.disabled = profile.state === 1;
      remove.addEventListener("click", async () => {
        const last4 = String(profile.iccid || "").slice(-4);
        const values = await showModal({
          title: "刪除 Profile",
          message: `刪除不可恢復。請輸入 ICCID 後四位 ${last4} 確認。`,
          confirmLabel: "刪除",
          danger: true,
          fields: [{ name: "confirmation", label: "ICCID 後四位", required: true }],
        });
        if (!values) return;
        if (values.confirmation !== last4) {
          notice("ICCID 後四位不符，未執行刪除");
          return;
        }
        remove.disabled = true;
        try {
          await api("/api/esim/profile", { method: "DELETE", body: JSON.stringify({ iccid: profile.iccid, aid: profile.aid || "" }) });
          notice("Profile 已刪除");
          await loadESIM();
        } catch (error) { notice(error.message); } finally { remove.disabled = false; }
      });
      actionBox.append(localNote, rename, remove);
      const description = document.createElement("div");
      description.className = "profile-description";
      description.append(detail, metadata);
      row.append(name, description, actionBox);
      return row;
    });
    list.replaceChildren(...(eidPanel ? [eidPanel] : []), ...profileItems);
    void loadESIMHealth();
    setESIMHealthPolling(true);
  } catch (error) {
    status.textContent = `讀取失敗：${error.message}`;
    list.textContent = error.message;
    showESIMCardState("暫時無法讀取卡片", error.message, "is-error");
    setESIMHealthPolling(false);
  }
}

document.querySelectorAll(".tab").forEach((tab) => {
  tab.addEventListener("click", () => {
    document.querySelectorAll(".tab").forEach((item) => {
      item.classList.remove("active");
      item.removeAttribute("aria-current");
    });
    document.querySelectorAll(".view").forEach((view) => view.classList.remove("active"));
    tab.classList.add("active");
    tab.setAttribute("aria-current", "page");
    $(`#${tab.dataset.view}`).classList.add("active");
    setNetworkActivityPolling(tab.dataset.view === "overview");
    if (tab.dataset.view === "esim") loadESIM();
    else setESIMHealthPolling(false);
    if (tab.dataset.view === "network") loadNetwork();
    if (tab.dataset.view === "at") {
      requestAnimationFrame(() => {
        const input = $("#at-command");
        input.focus();
        input.setSelectionRange(input.value.length, input.value.length);
      });
    }
  });
});

$("#esim-download-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const confirmed = await showModal({
    title: "下載新的 Profile",
    message: "將向 SM-DP+ 伺服器下載並寫入新的 eSIM Profile。寫入期間請勿拔出模組。",
    confirmLabel: "開始下載",
  });
  if (!confirmed) return;
  const button = event.currentTarget.querySelector("button[type=submit]");
  const status = $("#esim-download-status");
  button.disabled = true;
  status.textContent = "正在下載並寫入 Profile，請勿拔出模組...";
  try {
    const result = await api("/api/esim/download", { method: "POST", body: JSON.stringify({
      smdp: $("#esim-smdp").value, matching_id: $("#esim-matching-id").value,
      confirmation_code: $("#esim-confirmation-code").value, imei: $("#esim-imei").value, aid: $("#esim-aid").value,
    }) });
    status.textContent = result.message || "Profile 下載完成，正在重新讀取卡片";
    notice("Profile 下載完成");
    await loadESIM();
  } catch (error) { status.textContent = `下載失敗：${error.message}`; notice(error.message); } finally { button.disabled = false; }
});

const messageInput = $("#message");
const messageCounter = $("#message-counter");
const updateMessageCounter = () => {
  messageCounter.textContent = `${messageInput.value.length} 字 · 自動分段`;
};
messageInput.addEventListener("input", updateMessageCounter);
updateMessageCounter();

$("#send-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = event.submitter;
  const originalLabel = button.textContent;
  button.disabled = true;
  button.textContent = "傳送中";
  try {
    const result = await api("/api/sms/send", {
      method: "POST",
      body: JSON.stringify({ phone: $("#phone").value, message: $("#message").value }),
    });
    messageInput.value = "";
    updateMessageCounter();
    const segments = Number(result.segments || 1);
    notice(segments > 1 ? `簡訊已傳送（${segments} 個分段）` : "簡訊已傳送");
  } catch (error) {
    notice(error.message);
  } finally {
    button.disabled = false;
    button.textContent = originalLabel;
  }
});

$("#at-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const output = $("#at-output");
  const input = $("#at-command");
  const command = input.value.trim();
  if (!command) {
    input.value = "";
    input.focus();
    return;
  }
  output.textContent = `› ${command}\n\n執行中...`;
  input.value = "";
  input.focus();
  try {
    const result = await api("/api/at", {
      method: "POST",
      body: JSON.stringify({ command }),
    });
    output.textContent = `› ${command}\n\n${result.response || "OK"}`;
  } catch (error) {
    output.textContent = `› ${command}\n\n${error.message}`;
  }
});

$("#refresh").addEventListener("click", async () => {
  await Promise.all([loadStatus(), loadSMS(), loadSidebarConnection()]);
  notice("狀態已重新整理");
});
$("#sim-phone-toggle").addEventListener("click", () => {
  if (!currentSIMPhoneNumber) return;
  simPhoneNumberRevealed = !simPhoneNumberRevealed;
  renderSIMPhoneNumber(currentSIMPhoneNumber, true);
});
$("#sim-phone-copy").addEventListener("click", () => {
  if (currentSIMPhoneNumber) copyIdentifier(currentSIMPhoneNumber, "本機號碼");
});
$("#refresh-sms").addEventListener("click", async () => {
  const button = $("#refresh-sms");
  button.disabled = true;
  $("#sms-status").textContent = "正在讀取簡訊...";
  try {
    const result = await api("/api/sms/refresh", { method: "POST" });
    await loadSMS();
    $("#sms-status").textContent = `簡訊讀取完成：${result.count ?? "未知"} 則`;
    notice("簡訊讀取完成");
  } catch (error) {
    $("#sms-status").textContent = `讀取簡訊失敗：${error.message}`;
    notice(error.message);
  } finally {
    button.disabled = false;
  }
});
$("#clear-module-sms").addEventListener("click", async () => {
  const confirmed = await showModal({
    title: "清空模組舊簡訊",
    message: "只會清空模組內部 ME 儲存裡的舊簡訊，不會刪除 SIM 卡簡訊。",
    confirmLabel: "確認清空",
    danger: true,
  });
  if (!confirmed) return;
  const button = $("#clear-module-sms");
  button.disabled = true;
  $("#sms-status").textContent = "正在清空模組內部舊簡訊...";
  try {
    const result = await api("/api/sms/clear-module", { method: "POST" });
    $("#sms-status").textContent = `模組舊簡訊已清理：${result.before ?? 0} -> ${result.after ?? 0} 則`;
    await loadSMS();
    notice("模組舊簡訊已清理");
  } catch (error) {
    $("#sms-status").textContent = `清理模組舊簡訊失敗：${error.message}`;
    notice(error.message);
  } finally {
    button.disabled = false;
  }
});
$("#refresh-esim").addEventListener("click", loadESIM);
$("#probe-esim-phonebook").addEventListener("click", probeESIMPhonebook);
$("#refresh-network").addEventListener("click", loadNetwork);
$("#enable-network-service").addEventListener("click", enableNetworkService);
$("#workmode-sms").addEventListener("click", () =>
  switchWorkMode(0, "簡訊模式", $("#workmode-sms")));
$("#workmode-network").addEventListener("click", () =>
  switchWorkMode(1, "上網模式", $("#workmode-network")));
$("#check-4g-route").addEventListener("click", () =>
  runNetworkCheck("4G 網際網路", "/api/network/check-4g", $("#check-4g-route")));
$("#check-proxy-route").addEventListener("click", () =>
  runNetworkCheck("代理", "/api/network/check-proxy", $("#check-proxy-route")));
$("#usbnet-mode-0").addEventListener("click", () => setUSBNetMode(0));
$("#usbnet-mode-1").addEventListener("click", () => setUSBNetMode(1));
$("#usbnet-mode-2").addEventListener("click", () => setUSBNetMode(2));
$("#usbnet-mode-3").addEventListener("click", () => setUSBNetMode(3));
$("#reboot-module").addEventListener("click", rebootModule);

loadStatus();
loadSMS();
loadSidebarConnection();
setNetworkTrafficPolling(true);
setNetworkActivityPolling(true);
setInterval(loadStatus, 10000);
setInterval(loadSMS, 5000);
setInterval(loadSidebarConnection, 10000);
