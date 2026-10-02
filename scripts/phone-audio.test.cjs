const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname, '../cmd/dj4ghub/web/phone.js'), 'utf8');

function harness() {
  const elements = new Map();
  const requests = [];
  const context = {
    moduleAudioBusy: false, callPollBusy: false, phoneActionBusy: false,
    previousCallsPresent: true, callStarted: new Map(), moduleAudioToken: 'session', moduleAudioSupported: true, phoneCallActive: false, updateKeypadMode: () => {}, isCallWindow: false, callWindowHadCall: false, callWindowCloseTimer: null,
    document: {querySelectorAll: () => []},
    $: id => { if (!elements.has(id)) elements.set(id, {checked: true, dataset: {}}); return elements.get(id); },
    stopPhoneAudio: () => requests.push('stop-media'),
    ensureModuleAudio: async () => requests.push('ensure'),
    connectPhoneAudio: async () => requests.push('connect'),
    api: async (path, options) => { requests.push(options ? JSON.parse(options.body).action : path); return {calls: []}; }
  };
  vm.createContext(context);
  vm.runInContext(source.slice(source.indexOf('async function refreshCalls()'), source.indexOf("$('#phone-dial').onclick")), context);
  return {context, requests, elements};
}

test('remote hangup closes media but preserves prepared module', async () => {
  const {context, requests} = harness();
  await context.refreshCalls();
  assert.deepEqual(requests, ['/api/calls', 'stop-media']);
  assert.equal(context.moduleAudioToken, 'session');
});
test('explicit hangup does not stop module session', async () => {
  const {context, requests} = harness();
  await context.phoneAction('hangup');
  assert.equal(requests[0], 'hangup');
  assert.ok(!requests.includes('ensure'));
  assert.equal(context.moduleAudioToken, 'session');
});
test('dial initializes and connects audio before sending dial command', async () => {
  const {context, requests} = harness();
  await context.phoneAction('dial', {number: '+6421000000'});
  assert.deepEqual(requests.slice(0, 3), ['ensure', 'connect', 'dial']);
});
test('failed audio initialization never dials', async () => {
  const {context, requests} = harness();
  context.ensureModuleAudio = async () => { throw new Error('not ready'); };
  await context.phoneAction('dial', {number: '+6421000000'});
  assert.ok(!requests.includes('dial'));
  assert.ok(requests.includes('stop-media'));
});
test('explicit control-only mode works without audio dependencies', async () => {
  const {context, requests} = harness();
  context.$('#phone-use-audio').checked = false;
  await context.phoneAction('answer');
  assert.equal(requests[0], 'answer');
  assert.ok(!requests.includes('ensure'));
});
test('dial still works when the service reports no module audio', async () => {
  const {context, requests, elements} = harness();
  context.moduleAudioSupported = false;
  await context.phoneAction('dial', {number: '0912345678'});
  assert.equal(requests[0], 'dial');
  assert.ok(!requests.includes('ensure'));
  assert.match(elements.get('#phone-feedback').textContent, /未連接電腦音訊/);
});
test('keypad sends DTMF only while a call is connected', async () => {
  const {context} = harness();
  context.api = async () => ({calls: [{id: 1, state: 0, number: '0900000000'}]});
  await context.refreshCalls();
  assert.equal(context.phoneCallActive, true);
  context.api = async () => ({calls: []});
  await context.refreshCalls();
  assert.equal(context.phoneCallActive, false);
});
test('call card shows the incoming number', async () => {
  const {context, elements} = harness();
  context.api = async () => ({calls: [{id: 1, direction: 1, state: 4, number: '0900639025'}]});
  await context.refreshCalls();
  assert.equal(elements.get('#call-card').dataset.state, 'ringing');
  assert.equal(elements.get('#call-card-number').textContent, '0900639025');
  assert.equal(elements.get('#call-card-kicker').textContent, '來電');
});
test('USB preparation suppresses misleading call poll failure', async () => {
  const {context, requests, elements} = harness();
  context.moduleAudioBusy = true;
  await context.refreshCalls();
  assert.equal(requests.length, 0);
  assert.match(elements.get('#phone-status').textContent, /USB/);
});
