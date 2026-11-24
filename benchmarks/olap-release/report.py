"""Report a saved isolated run (including a single query) without executing SQL."""

import argparse
import json
from pathlib import Path

from run import safe_error


def render(folder):
    metadata = folder / 'metadata.json'
    meta = json.loads(metadata.read_text()) if metadata.exists() else {}
    records = [json.loads(line) for line in (folder / 'queries.jsonl').read_text().splitlines()]
    lines = ['# Isolated query results', '',
             f"Scope: {meta.get('scope', 'only the query records saved in this directory')}. No full-suite score is implied.", '',
             'Each successful engine has one warm-up and three fully fetched, validated timed runs.',
             'Values above 1 mean slower than the native DuckDB reference. Failed validation gets no ratio.', '',
             '| Query | Engine | Median (ms) | Ratio to native DuckDB | Status |',
             '|---|---|---:|---:|---|']
    labels = {'duckdb': 'Native DuckDB', 'redis': 'Direct Redis ADBC',
              'duckdb_adbc': 'DuckDB over Redis ADBC'}
    for record in records:
        for engine, label in labels.items():
            status = record.get(f'{engine}_status')
            if status is None:
                continue
            valid = status == 'ok' and record.get('duckdb_status') == 'ok'
            value = record.get(f'{engine}_median_seconds') if valid else None
            median = f'{value * 1000:.3f}' if value is not None else '—'
            ratio = 1 if engine == 'duckdb' and valid else record.get(f'{engine}_over_duckdb') if valid else None
            ratio_text = f'{ratio:.2f}×' if ratio is not None else '—'
            lines.append(f"| {record['query']} | {label} | {median} | {ratio_text} | {status} |")
    errors = [(r['query'], e, r[f'{e}_error']) for r in records for e in labels if f'{e}_error' in r]
    if errors:
        lines.extend(['', 'Failure details:', ''])
        for query, engine, error in errors:
            lines.append(f'- Query {query}, {labels[engine]}: {safe_error(error).replace(chr(10), " ")}')
    if any('duckdb_adbc_status' in r for r in records):
        lines.extend(['', 'The third engine uses the DuckDB planner over a read-only Redis ADBC catalog.',
                      'Whole-query remote pushdown is disabled; scan pushdown remains extension-dependent.'])
    return '\n'.join(lines) + '\n'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input', required=True, type=Path, help='suite/query folder containing queries.jsonl')
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    args.output.write_text(render(args.input))


if __name__ == '__main__':
    main()
