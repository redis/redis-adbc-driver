const element = id => document.getElementById(id);
const format = n => Number(n || 0).toLocaleString();

function table(id, headings, rows) {
  const table = document.createElement('table');
  const header = document.createElement('tr');
  for (const text of headings) {
    const cell = document.createElement('th');
    cell.textContent = text;
    header.append(cell);
  }
  table.append(header);
  for (const values of rows) {
    const row = document.createElement('tr');
    for (const value of values) {
      const cell = document.createElement('td');
      // Labels are user supplied. Never interpret them as HTML.
      cell.textContent = value ?? '—';
      row.append(cell);
    }
    table.append(row);
  }
  element(id).replaceChildren(table);
  if (!rows.length) {
    const message = document.createElement('p');
    message.textContent = 'No data reported yet.';
    message.className = 'muted';
    element(id).append(message);
  }
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

function render(data) {
  const metrics = Object.values(data.metrics);
  const attempts = metrics.reduce((n, m) => n + m.attempts, 0);
  const errors = metrics.reduce((n, m) => n + m.errors, 0);
  const finished = metrics.reduce((n, m) => n + m.success + m.errors + m.canceled + m.abandoned, 0);
  const retention = data.retention_week4;
  const cards = [
    ['Weekly active installs', data.weekly_active_installations],
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
      kind,
      ...['attempts', 'success', 'errors', 'canceled', 'abandoned'].map(key => format(m[key])),
      format(m.rows), format(m.affected),
      ...[.5, .95, .99].map(p => percentile(m.latency, data.latency_bounds_ms, p)),
      percentile(m.ready_latency, data.latency_bounds_ms, .95),
    ]);
  table('queries', ['Type', 'Attempts', 'Success', 'Errors', 'Canceled', 'Abandoned',
    'Rows read', 'Rows affected', 'p50', 'p95', 'p99', 'p95 ready'], queryRows);
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

async function load(event) {
  event.preventDefault();
  element('status').textContent = 'Loading…';
  try {
    const response = await fetch('/api/summary', {
      headers: { Authorization: 'Bearer ' + element('token').value },
    });
    if (!response.ok) {
      throw Error(response.status === 401 ? 'Invalid read token' : 'Could not load metrics');
    }
    render(await response.json());
    element('status').textContent = 'Updated ' + new Date().toLocaleTimeString();
  } catch (error) {
    // Clear previously authorized data if a later request fails.
    for (const id of ['cards', 'queries', 'orgs', 'features', 'daily', 'downloads']) {
      element(id).replaceChildren();
    }
    element('status').textContent = error.message;
  }
}

element('login').addEventListener('submit', load);
