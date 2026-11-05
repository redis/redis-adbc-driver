const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
function dashboard() {
  const elements = new Map();
  const node = () => ({children: [], value: '', append(...children) { this.children.push(...children); },
    attributes: {}, events: {}, setAttribute(k,v) {this.attributes[k]=v;},
    replaceChildren(...children) { this.children = children; }, addEventListener(event, callback) {this.events[event]=callback;}});
  const document = {createElement: node, getElementById(id) {
    if (!elements.has(id)) elements.set(id, node());
    return elements.get(id);
  }};
  const context = vm.createContext({document});
  vm.runInContext(fs.readFileSync(__dirname + '/dashboard.js', 'utf8'), context);
  return {context, elements};
}
test('missing counters distinguish inapplicable from unavailable and preserve zero', () => {
  const {context} = dashboard();
  assert.equal(vm.runInContext("measured({kind:'select',rows_affected:null}, 'rows_affected').text", context), 'Not applicable');
  assert.equal(vm.runInContext("measured({kind:'select',indexed_rows:0}, 'indexed_rows')", context), '0');
  assert.equal(vm.runInContext("missingMeasurement({kind:'ingest'}, 'input_work').text", context), 'Not applicable');
  assert.equal(vm.runInContext("missingMeasurement({kind:'ddl'}, 'input_work').text", context), 'Unavailable');
  assert.match(vm.runInContext("missingMeasurement({kind:'select',status:'canceled'}, 'input_work').reason", context), /canceled.*incomplete/);
});
test('execution table exposes explanations and treats labels as text', () => {
  const {context, elements} = dashboard();
  vm.runInContext(`renderExecutions({executions:[{kind:'select',status:'success',id:'abc',
    completed_at:'2026-11-05T18:00:00Z',duration_ms:1,rows_returned:0,
    rows_examined:0,indexed_rows:null,rows_affected:null,source_hostname:'<script>unsafe</script>'}]})`, context);
  const rows = elements.get('executions').children[1].children[1].children;
  assert.equal(rows[0].children[1].textContent, '<script>unsafe</script>');
  assert.equal(rows[0].children[3].textContent, 'Unavailable');
  assert.match(rows[0].children[3].title, /readiness checks/);
  assert.equal(rows[0].children[4].textContent, '0 driver candidates');
  assert.equal(rows[0].children[6].textContent, 'Not applicable');
  assert.match(rows[0].children[10].textContent, /Index size:/);
  assert.doesNotMatch(rows[0].children[10].textContent, /Rows changed:/);
});

test('table controls page, combine search/filters, sort numbers, and reset', () => {
  const {context, elements} = dashboard();
  vm.runInContext("table('test', ['Host', 'Rows'], Array.from({length:60}, (_,i) => ['host-'+i, (1000-i).toLocaleString('en-US')]))", context);
  const [controls, grid, pager] = elements.get('test').children;
  const [search, size, reset] = controls.children;
  const [previous, status, next] = pager.children;
  assert.equal(grid.children[1].children.length, 25);
  next.events.click(); assert.match(status.textContent, /Page 2 of 3/);
  size.value = '10'; size.events.change(); assert.match(status.textContent, /Page 1 of 6/);
  const headings = grid.children[0].children[0].children;
  headings[1].children[0].events.click();
  assert.equal(grid.children[1].children[0].children[1].textContent, '941');
  headings[1].children[0].events.click();
  assert.equal(grid.children[1].children[0].children[1].textContent, '1,000');
  search.value = 'host-1'; search.events.input();
  assert.match(status.textContent, /11 matching rows/);
  const filter = grid.children[0].children[1].children[1].children[0];
  filter.value = '999'; filter.events.input();
  assert.match(status.textContent, /1 matching rows/);
  search.value = 'no-match'; search.events.input();
  assert.equal(grid.children[1].children[0].children[0].textContent, 'No rows match your search or filters.');
  assert.equal(previous.disabled, true); assert.equal(next.disabled, true);
  reset.events.click(); assert.match(status.textContent, /Page 1 of 3/);
  assert.equal(search.value, ''); assert.equal(filter.value, '');
});
