"""Read-only capability/precision diagnostic; not a native-export prototype.

Read a private {"uri": ..., "ca": ...} configuration from stdin. Run only
against a verified owned test endpoint. No credentials or endpoint identifiers
are emitted. An empty name match is not an exhaustive server capability proof.
"""
import json
import sys

import redis


def probe(client):
    exact = '9007199254740993'
    # EVAL_RO touches no keys; this is independent of table contents and slots.
    response = client.eval_ro('return {ARGV[1], tonumber(ARGV[1])}', 0, exact)
    modules = client.module_list()
    def text(value):
        return value.decode() if isinstance(value, bytes) else value
    return {
        'server_version': client.info('server').get('redis_version'),
        'resp': client.client_info().get('resp'),
        'modules': [{text(k): text(v) for k, v in module.items() if text(k) in ('name', 'ver')}
                    for module in modules],
        'arrow_command_name_matches': [text(name) for name in client.execute_command('COMMAND LIST', 'FILTERBY', 'PATTERN', '*arrow*')],
        'naive_lua_numeric_export': {'input_exact_string': exact, 'returned_string': text(response[0]),
                                     'returned_number': response[1], 'exact': response[1] == int(exact)},
        'interpretation': 'Name matching is diagnostic only, not exhaustive detection. The proposed versioned shard export has no implementation here. Naive Lua numeric conversion fails exact BIGINT semantics; returning the original string preserves that value but retains client typed parsing. No native server capability, elapsed speedup or server-owner approval is established.'
    }


if __name__ == '__main__':
    cfg = json.load(sys.stdin)
    with redis.Redis.from_url(cfg['uri'], ssl_ca_certs=cfg['ca'], protocol=3) as client:
        print(json.dumps(probe(client), indent=2))
