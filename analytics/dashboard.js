const element = id => document.getElementById(id);
const format = n => Number(n || 0).toLocaleString();

const tableStates = new Map();
const cellText = value => String(value && typeof value === 'object' ? value.text : (value ?? '—'));
function sortValue(value, heading) {
  const text = cellText(value);
  if (['Unavailable', 'Not applicable', '—', 'Not measured'].includes(text)) return null;
  if (/date|completed|received|captured/i.test(heading)) {
    const date = Date.parse(text);
    if (!Number.isNaN(date)) return date;
  }
  const number = text.replaceAll(',', '').match(/^(?:[≤>]\s*)?(-?\d+(?:\.\d+)?)(?:\s*(?:ms|%|driver candidates|aggregate rows represented))?$/);
  return number ? Number(number[1]) : text;
}
function tableRows(rows, headings, state) {
  const filtered = rows.filter(row => row.some(v => cellText(v).toLowerCase().includes(state.search))
    && state.filters.every((filter, i) => cellText(row[i]).toLowerCase().includes(filter)));
  if (state.sort !== null) filtered.sort((a, b) => {
    const av = sortValue(a[state.sort], headings[state.sort]);
    const bv = sortValue(b[state.sort], headings[state.sort]);
    if (av === null || bv === null) return av === bv ? 0 : (av === null ? 1 : -1);
    const order = typeof av === 'number' && typeof bv === 'number'
      ? av - bv : String(av).localeCompare(String(bv), undefined, {numeric: true});
    return order * state.direction;
  });
  return filtered;
}
function table(id, headings, rows) {
  const state = tableStates.get(id) || {search: '', filters: headings.map(() => ''), sort: null, direction: 1, page: 0, size: 25};
  tableStates.set(id, state);
  const container = element(id);
  const controls = document.createElement('div');
  controls.className = 'table-controls';
  const search = document.createElement('input');
  search.type = 'search'; search.placeholder = 'Search this table'; search.value = state.search;
  search.setAttribute('aria-label', id + ': search all columns');
  const size = document.createElement('select');
  size.setAttribute('aria-label', id + ': rows per page');
  for (const n of [10, 25, 50, 100]) {
    const option = document.createElement('option'); option.value = String(n); option.textContent = n + ' rows'; size.append(option);
  }
  size.value = String(state.size);
  const reset = document.createElement('button'); reset.type = 'button'; reset.textContent = 'Reset table';
  controls.append(search, size, reset);
  const grid = document.createElement('table');
  const head = document.createElement('thead'); const header = document.createElement('tr');
  const filters = document.createElement('tr');
  const headers = []; const inputs = [];
  headings.forEach((text, i) => {
    const cell = document.createElement('th'); cell.scope = 'col';
    const button = document.createElement('button'); button.type = 'button'; button.textContent = text;
    button.setAttribute('aria-label', 'Sort by ' + text);
    button.addEventListener('click', () => {state.direction = state.sort === i ? -state.direction : 1; state.sort = i; state.page = 0; draw();});
    cell.append(button); header.append(cell); headers.push(cell);
    const filterCell = document.createElement('th');
    const input = document.createElement('input'); input.type = 'search'; input.placeholder = 'Filter'; input.value = state.filters[i];
    input.setAttribute('aria-label', id + ': filter ' + text);
    input.addEventListener('input', () => {state.filters[i] = input.value.trim().toLowerCase(); state.page = 0; draw();});
    filterCell.append(input); filters.append(filterCell); inputs.push(input);
  });
  head.append(header, filters); grid.append(head);
  const body = document.createElement('tbody'); grid.append(body);
  const pager = document.createElement('div'); pager.className = 'table-controls';
  const previous = document.createElement('button'); previous.type = 'button'; previous.textContent = 'Previous';
  previous.setAttribute('aria-label', id + ': previous page');
  const next = document.createElement('button'); next.type = 'button'; next.textContent = 'Next';
  next.setAttribute('aria-label', id + ': next page');
  const status = document.createElement('span'); status.setAttribute('role', 'status');
  pager.append(previous, status, next);
  function draw() {
    const visible = tableRows(rows, headings, state);
    const pages = Math.max(1, Math.ceil(visible.length / state.size)); state.page = Math.min(state.page, pages - 1);
    const offset = state.page * state.size;
    body.replaceChildren();
    for (const values of visible.slice(offset, offset + state.size)) {
      const row = document.createElement('tr');
      for (const value of values) {
        const cell = document.createElement('td');
        // Labels are user supplied. Never interpret them as HTML.
        if (value && typeof value === 'object' && value.details) {
          const details = document.createElement('details');
          details.className = 'measurement-details';
          const summary = document.createElement('summary');
          summary.textContent = 'Details';
          summary.setAttribute('aria-label', 'Measurement notes for execution ' + value.executionId);
          const notes = document.createElement('div');
          notes.textContent = cellText(value);
          details.append(summary, notes);
          cell.append(details);
        } else {
          cell.textContent = cellText(value);
        }
        if (value && typeof value === 'object' && value.reason) cell.title = value.reason;
        row.append(cell);
      }
      body.append(row);
    }
    if (!visible.length) {
      const row = document.createElement('tr'); const cell = document.createElement('td'); cell.colSpan = headings.length;
      cell.textContent = rows.length ? 'No rows match your search or filters.' : 'No data reported yet.'; row.append(cell); body.append(row);
    }
    headers.forEach((cell, i) => cell.setAttribute('aria-sort', state.sort === i ? (state.direction === 1 ? 'ascending' : 'descending') : 'none'));
    previous.disabled = state.page === 0; next.disabled = state.page >= pages - 1;
    status.textContent = 'Page ' + (state.page + 1) + ' of ' + pages + ' · ' + (visible.length ? offset + 1 : 0)
      + '–' + Math.min(offset + state.size, visible.length) + ' of ' + visible.length + ' matching rows (' + rows.length + ' loaded)';
  }
  search.addEventListener('input', () => {state.search = search.value.trim().toLowerCase(); state.page = 0; draw();});
  size.addEventListener('change', () => {state.size = Number(size.value); state.page = 0; draw();});
  previous.addEventListener('click', () => {state.page--; draw();});
  next.addEventListener('click', () => {state.page++; draw();});
  reset.addEventListener('click', () => {
    state.search = ''; state.filters.fill(''); state.sort = null; state.page = 0; state.size = 25;
    search.value = ''; size.value = '25'; inputs.forEach(input => {input.value = '';}); draw();
  });
  container.replaceChildren(controls, grid, pager); draw();
}

function percentile(histogram, bounds, fraction) {
  const count = histogram.reduce((a, b) => a + b, 0);
  if (!count) return '—';
  let total = 0;
  for (let i = 0; i < histogram.length; i++) {
    total += histogram[i];
    if (total >= count * fraction) {
      return i < bounds.length
        ? '≤ ' + format(bounds[i]) + ' ms'
        : '> ' + format(bounds.at(-1)) + ' ms';
    }
  }
}

const kindLabel = kind => ({select: 'SELECT · query', insert: 'INSERT', update: 'UPDATE', delete: 'DELETE',
  ingest: 'Arrow ingestion', ddl: 'Schema change', metadata: 'Metadata', script: 'SQL script',
  session: 'Session settings', merge: 'MERGE', unknown: 'Unclassified', other: 'Other'})[kind] || kind;
const hasMeasurement = n => n !== null && n !== undefined;
function missingMeasurement(e, field) {
  if (field === 'rows_affected' && ['select', 'metadata', 'session'].includes(e.kind)) {
    return {text: 'Not applicable', reason: 'This statement type does not change data rows.'};
  }
  if (field !== 'rows_affected' && ['ingest', 'metadata', 'session'].includes(e.kind)) {
    return {text: 'Not applicable', reason: 'This operation does not use the query input-work counters.'};
  }
  const end = ['error', 'canceled', 'abandoned'].includes(e.status)
    ? ' The execution ended as ' + e.status + '; counters may be incomplete.' : '';
  const reasons = {
    indexed_rows: 'No index population was captured by existing readiness checks. Index size is not rows scanned.',
    input_work: 'No supported driver-candidate or aggregate counter was reported. Redis internal scan work is not measured.',
    rows_affected: 'The driver did not report an affected-row count.',
  };
  return {text: 'Unavailable', reason: reasons[field] + end};
}
const measured = (e, field) => hasMeasurement(e[field]) ? format(e[field]) : missingMeasurement(e, field);
let latest = null;

function renderExecutions(data) {
  const filter = element('execution-filter').value.trim().toLowerCase();
  const all = data.executions || [];
  const rows = all.filter(e => [e.kind, e.source_hostname, e.reverse_dns, e.public_ip, e.application, e.status, e.id, e.driver_version, e.traffic_class, e.execution_path, e.error_class,
    ...(e.features || [])].some(v => String(v || '').toLowerCase().includes(filter)));
  const inputWork = e => {
    const parts = [];
    if (e.rows_examined !== null && e.rows_examined !== undefined) parts.push(format(e.rows_examined) + ' driver candidates');
    if (e.rows_represented !== null && e.rows_represented !== undefined) parts.push(format(e.rows_represented) + ' aggregate rows represented');
    return parts.join(' · ') || missingMeasurement(e, 'input_work');
  };
  table('executions', ['Completed (your time)', 'Reporting host', 'Statement / features',
    'Index documents observed', 'Measured input work', 'Rows returned', 'Rows changed', 'Outcome', 'Elapsed', 'Execution ID', 'Measurement notes', 'Driver version', 'Traffic', 'Observed path', 'Error category', 'Group-limit retries', 'Aggregate row fallbacks'], rows.map(e => [
    new Date(e.completed_at).toLocaleString(), e.source_hostname || e.reverse_dns || 'Not reported',
    kindLabel(e.kind) + ((e.features || []).length ? ' · ' + e.features.join(', ') : ''),
    measured(e, 'indexed_rows'), inputWork(e), format(e.rows_returned), measured(e, 'rows_affected'), e.status,
    format(e.duration_ms) + ' ms', e.id.slice(0, 8),
    executionNotes(e), e.driver_version || 'unknown', trafficLabel(e.traffic_class), e.execution_path || 'Not reported', e.error_class || (['error','canceled'].includes(e.status) ? 'Not reported' : '—'),
    e.group_limit_retries ?? 'Not reported', e.row_fallbacks ?? 'Not reported',
  ]));
  element('execution-count').textContent = rows.length + ' shown of ' + all.length + ' recent samples. '
    + 'Clients retain their latest 24 completed executions per report; this is not a complete query log.';
}

function executionNotes(e) {
  const text = [
    !hasMeasurement(e.indexed_rows) && ['Index size', missingMeasurement(e, 'indexed_rows')],
    !hasMeasurement(e.rows_examined) && !hasMeasurement(e.rows_represented) && ['Input work', missingMeasurement(e, 'input_work')],
    !hasMeasurement(e.rows_affected) && ['Rows changed', missingMeasurement(e, 'rows_affected')],
  ].filter(v => v && v[1].text === 'Unavailable').map(([label, value]) => label + ': ' + value.reason).join(' ');
  return text ? {text, details: true, executionId: e.id} : '—';
}

const trafficLabel = value => ({ci:'CI / test',ci_inferred:'CI (inferred)',benchmark:'Benchmark',application:'Application (self-reported)',unknown:'Unknown'})[value] || 'Unknown';
const outcomes = m => m.success + m.errors + m.canceled + m.abandoned;
function renderInsights(data) {
  const comparisons = data.comparisons || [];
  table('comparisons', ['Traffic', 'Driver version', 'Statement', 'Attempts', 'Success', 'Errors', 'Error rate', 'Canceled', 'Abandoned', 'p50 elapsed', 'p95 elapsed'], comparisons.map(c => {
    const m = c.metrics, completed = outcomes(m);
    return [trafficLabel(c.traffic_class), c.driver_version, kindLabel(c.kind), format(m.attempts), format(m.success), format(m.errors), completed ? (100*m.errors/completed).toFixed(1)+'%' : '—', format(m.canceled), format(m.abandoned), percentile(m.latency,data.latency_bounds_ms,.5), percentile(m.latency,data.latency_bounds_ms,.95)];
  }));
  table('diagnostics', ['Traffic','Driver version','Statement','Diagnostic coverage','Observed paths','Error categories','Group-limit retries','Aggregate row fallbacks'], comparisons.map(c => {
    const m=c.metrics, paths=m.execution_paths || {}, classes=m.error_classes || {};
    const categories = counts => Object.entries(counts).map(([key,count])=>key+': '+format(count)).join(' · ') || 'Not reported';
    const covered=Object.values(paths).reduce((a,b)=>a+b,0);
    return [trafficLabel(c.traffic_class),c.driver_version,kindLabel(c.kind),format(covered)+' / '+format(outcomes(m)),categories(paths),Object.keys(classes).length ? categories(classes) : covered && !m.errors && !m.canceled ? 'None' : 'Not reported',covered ? format(m.group_limit_retries) : 'Not reported',covered ? format(m.row_fallbacks) : 'Not reported'];
  }));
  const groups = new Map();
  for (const e of data.executions || []) {
    const dimensions=[e.traffic_class || 'unknown',e.driver_version || 'unknown',e.kind,e.workload_size || 'unmeasured'];
    const key=JSON.stringify(dimensions);
    if (!groups.has(key)) groups.set(key,{dimensions,durations:[],errors:0,abandoned:0});
    const g=groups.get(key); g.durations.push(e.duration_ms); g.errors+=e.status==='error'; g.abandoned+=e.status==='abandoned';
  }
  table('workload-comparisons',['Traffic','Driver version','Statement','Measured input size','Samples','Errors','Abandoned','Sample p50','Sample p95'],Array.from(groups.values(),g=>{
    g.durations.sort((a,b)=>a-b);
    const quantile=p=>format(g.durations[Math.ceil(p*g.durations.length)-1])+' ms';
    return [trafficLabel(g.dimensions[0]),g.dimensions[1],kindLabel(g.dimensions[2]),g.dimensions[3],format(g.durations.length),format(g.errors),format(g.abandoned),quantile(.5),quantile(.95)];
  }));
}

function render(data) {
  latest = data;
  renderInsights(data);
  const metrics = Object.values(data.metrics);
  const attempts = metrics.reduce((n, m) => n + m.attempts, 0);
  const errors = metrics.reduce((n, m) => n + m.errors, 0);
  const finished = metrics.reduce((n, m) => n + m.success + m.errors + m.canceled + m.abandoned, 0);
  const retention = data.retention_week4;
  const cards = [
    ['Distinct active installation IDs · 7 days', data.weekly_active_installations],
    ['Associated active deployments · 7 days', data.weekly_active_deployments || 0],
    ['Identified reporting sources · 28 days', (data.hosts || []).filter(h => h.hostname || h.public_ip).length],
    ['Active organizations', data.self_reported_organizations],
    ['Active teams', data.self_reported_teams],
    ['Query attempts', format(attempts)],
    ['Error rate', finished ? (100 * errors / finished).toFixed(1) + '%' : '—'],
    ['Week-4 retention', retention.rate === null
      ? 'Awaiting cohorts'
      : (100 * retention.rate).toFixed(1) + '% (' + retention.retained + '/' + retention.eligible + ')'],
  ];
  element('cards').replaceChildren();
  for (const [label, value] of cards) {
    const card = document.createElement('div');
    card.className = 'card';
    const title = document.createElement('div');
    title.textContent = label;
    const number = document.createElement('div');
    number.className = 'value';
    number.textContent = value;
    card.append(title, number);
    element('cards').append(card);
  }
  const queryRows = Object.entries(data.metrics)
    .sort((a, b) => b[1].attempts - a[1].attempts)
    .map(([kind, m]) => [
      kindLabel(kind),
      ...['attempts', 'success', 'errors', 'canceled', 'abandoned'].map(key => format(m[key])),
      ...[.5, .95, .99].map(p => percentile(m.latency, data.latency_bounds_ms, p)),
      percentile(m.ready_latency, data.latency_bounds_ms, .95),
    ]);
  table('queries', ['Statement type', 'Attempts', 'Success', 'Errors', 'Canceled', 'Abandoned',
    'p50 elapsed', 'p95 elapsed', 'p99 elapsed', 'p95 ready'], queryRows);
  renderExecutions(data);
  const basisLabel = value => ({runtime_instance:'Runtime ID (temporary installation IDs)',ecs_service:'ECS service identity',kubernetes_controller:'Kubernetes controller identity',
    'installation_id:local_file':'Saved installation ID','installation_id:explicit':'Application installation ID',
    'installation_id:temporary':'Temporary installation ID','installation_id:unknown':'Legacy installation ID (storage unknown)'})[value] || value;
  table('deployments', ['Deployment ID', 'Applications', 'Association basis', 'Installation IDs', 'Runtime instances',
    'Legacy handles', 'Hostnames', 'Public IPs', 'Query attempts', 'Last received'], (data.deployments || [])
    .sort((a,b) => b.attempts-a.attempts).map(d => [d.deployment_id, d.applications.join(', ') || 'Not labeled',
      d.bases.map(basisLabel).join(', '),format(d.installation_ids),format(d.runtime_instances),format(d.legacy_handles),
      d.hostnames.join(', ') || 'Not reported',d.public_ips.join(', ') || 'Not recorded',format(d.attempts),
      new Date(d.last_seen).toLocaleString()]));
  table('associations', ['Network evidence', 'Observed value', 'Possible related deployment IDs', 'Confidence', 'Merged?'],
    (data.probable_associations || []).map(a => [a.match_type === 'public_ip' ? 'Shared public IP' : 'Shared reverse DNS',
      a.value,a.deployment_ids.join(', '),'Probable · network only','No']));
  table('hosts', ['Reported hostname', 'Public reverse DNS (PTR)', 'Public egress IP', 'Distinct installation IDs',
    'ID storage', 'Query attempts', 'Last received'], (data.hosts || []).sort((a,b) => b.attempts-a.attempts).map(h => [
      h.hostname || 'Not reported', h.reverse_dns || 'Unavailable / pending', h.public_ip || 'Not recorded',
      format(h.installations), (h.id_sources || []).map(s => ({local_file:'Saved locally',explicit:'Application supplied',temporary:'Temporary',unknown:'Older client / unknown'})[s] || s).join(', '),
      format(h.attempts), new Date(h.last_seen).toLocaleString(),
    ]));
  table('orgs', ['Organization', 'Team', 'Application', 'Environment', 'Integration',
    'Use case', 'Installs', 'Attempts', 'Success', 'Errors'], data.organizations
    .sort((a, b) => b.attempts - a.attempts)
    .map(o => [o.organization || 'Unattributed', o.team, o.application, o.environment,
      o.integration, o.use_case, o.installations, format(o.attempts), format(o.success), format(o.errors)]));
  table('features', ['Feature', 'Executions'], Object.entries(data.features)
    .sort((a, b) => b[1] - a[1]).map(([key, value]) => [key, format(value)]));
  table('daily', ['Date (UTC)', 'Attempts', 'Success'], Object.entries(data.daily)
    .sort().reverse().map(([day, m]) => [day, format(m.attempts), format(m.success)]));
  table('downloads', ['Source', 'Captured', 'Detail'], data.downloads.map(snapshot => [
    snapshot.source,
    snapshot.captured_at,
    snapshot.source === 'github'
      ? 'Release asset downloads: ' + format(snapshot.total_asset_downloads)
      : 'Last day: ' + format(snapshot.data.last_day) + ' · Last week: '
        + format(snapshot.data.last_week) + ' · Last month: ' + format(snapshot.data.last_month),
  ]));
}

let loadSequence = 0;
async function load(event) {
  const sequence = ++loadSequence;
  event.preventDefault();
  element('status').textContent = 'Loading…';
  try {
    const response = await fetch('/api/summary?traffic=' + encodeURIComponent(element('traffic-filter').value || 'all'), {
      headers: { Authorization: 'Bearer ' + element('token').value },
    });
    if (!response.ok) {
      throw Error(response.status === 401 ? 'Invalid read token' : 'Could not load metrics');
    }
    const data = await response.json();
    if (sequence !== loadSequence) return;
    render(data);
    element('status').textContent = 'Updated ' + new Date().toLocaleTimeString();
  } catch (error) {
    if (sequence !== loadSequence) return;
    // Clear previously authorized data if a later request fails.
    for (const id of ['cards', 'queries', 'orgs', 'features', 'daily', 'downloads', 'hosts', 'executions', 'deployments', 'associations', 'comparisons', 'diagnostics', 'workload-comparisons']) {
      element(id).replaceChildren();
    }
    latest = null;
    element('execution-count').textContent = '';
    element('status').textContent = error.message;
  }
}

element('login').addEventListener('submit', load);

element('execution-filter').addEventListener('input', () => { if (latest) renderExecutions(latest); });

element('traffic-filter').addEventListener('change', event => { if (element('token').value) load(event); });
