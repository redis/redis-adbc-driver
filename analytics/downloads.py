#!/usr/bin/env python3
"""Snapshot GitHub release assets and PyPI Stats, keeping channels separate."""
import argparse
from datetime import datetime, timezone
import json
import os
import urllib.error
import urllib.parse
import urllib.request
from collector import Store


def fetch(url, token=None):
    headers = {'Accept': 'application/json', 'User-Agent': 'redis-adbc-adoption-analytics'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    with urllib.request.urlopen(urllib.request.Request(url, headers=headers), timeout=30) as response:
        return json.load(response)


def github(repo, get=fetch):
    if len(repo.split('/')) != 2 or any(not part or any(c not in 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-' for c in part) for part in repo.split('/')):
        raise ValueError('repo must be OWNER/REPO')
    assets = []
    page = 1
    while True:
        releases = get(f'https://api.github.com/repos/{repo}/releases?per_page=100&page={page}', os.environ.get('GITHUB_TOKEN'))
        for release in releases:
            # Release listings may truncate asset lists: paginate assets too.
            asset_page = 1
            while True:
                entries = get(f'https://api.github.com/repos/{repo}/releases/{release["id"]}/assets?per_page=100&page={asset_page}', os.environ.get('GITHUB_TOKEN'))
                for asset in entries:
                    assets.append({'id': asset['id'], 'version': release['tag_name'], 'name': asset['name'], 'downloads': asset['download_count']})
                if len(entries) < 100:
                    break
                asset_page += 1
        if len(releases) < 100:
            break
        page += 1
    return {'source': 'github', 'repo': repo, 'captured_at': datetime.now(timezone.utc).isoformat(), 'assets': assets, 'total_asset_downloads': sum(a['downloads'] for a in assets)}


def pypi(package, get=fetch):
    data = get('https://pypistats.org/api/packages/' + urllib.parse.quote(package, safe='') + '/recent')
    return {'source': 'pypistats', 'package': package, 'captured_at': datetime.now(timezone.utc).isoformat(), 'data': data['data']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--db', default='analytics.sqlite3')
    parser.add_argument('--repo', default='redis/redis-adbc-driver')
    parser.add_argument('--package', default='adbc-driver-redis')
    args = parser.parse_args()
    store = Store(args.db)
    errors = []
    for name, work in [('github', lambda: github(args.repo)), ('pypistats', lambda: pypi(args.package))]:
        try:
            payload = work()
            with store.connect() as db:
                db.execute('INSERT OR REPLACE INTO downloads VALUES(?,?,?)', (name, payload['captured_at'], json.dumps(payload)))
            print(json.dumps(payload))
        except (OSError, ValueError, KeyError) as error:
            errors.append(f'{name}: {type(error).__name__}')
    if errors:
        raise SystemExit('; '.join(errors))


if __name__ == '__main__':
    main()
