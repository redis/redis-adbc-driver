"""Transport-only RESP3/TLS GET/PING control; no ADBC or snapshot claim."""
import argparse,hashlib,importlib.metadata,itertools,json,os,platform,time,uuid
from pathlib import Path
import redis
p=argparse.ArgumentParser();p.add_argument('--out',type=Path,required=True);a=p.parse_args()
r=redis.Redis.from_url(os.environ['REDIS_URI'],ssl_ca_certs=os.environ['SSL_CERT_FILE'],socket_connect_timeout=10,socket_timeout=10)
lengths=(4096,8000,8191,8192,8193,16384,32768)
prefix=('codex:round3:raw-transport:'+uuid.uuid4().hex+':').encode()
keys={n:prefix+str(n).encode() for n in lengths};values={n:b'x'*n for n in lengths}
proof=other=None;samples=[];started=time.monotonic()
try:
    assert r.ping() and int(r.client_info()['resp'])==3
    assert not r.execute_command('FT._LIST'), 'Transport control requires the exclusively owned empty fixture'
    for n in lengths:
        assert r.set(keys[n],values[n])
    # Holding both borrowed connections makes same/different routing explicit.
    proof=r.connection_pool.get_connection('PING')
    other=r.connection_pool.get_connection('PING')
    assert proof is not other
    cases=list(itertools.product(lengths,(1,3),('same_connection','different_connection')))
    for repeat in range(8):
        order=cases if repeat%2==0 else list(reversed(cases))
        for n,count,route in order:
            response=proof if route=='same_connection' else other
            for _ in range(3):
                response.send_command('PING');assert response.read_response()==b'PONG'
            begin=time.perf_counter_ns()
            # Raw send/read never uses Redis.retry or transparent command replay.
            proof.send_packed_command(proof.pack_commands([('GET',keys[n])]*count))
            for _ in range(count):assert proof.read_response()==values[n]
            proof_ns=time.perf_counter_ns()-begin;ping=[]
            for _ in range(3):
                one=time.perf_counter_ns();response.send_command('PING');assert response.read_response()==b'PONG';ping.append(time.perf_counter_ns()-one)
            samples.append({'repeat':repeat,'payload_bytes':n,'get_commands':count,'route':route,'proof_ns':proof_ns,'ping_ns':ping,'sequence_ns':time.perf_counter_ns()-begin,'estimated_get_resp_bytes':count*(n+len(str(n))+5)})
    assert len(samples)==224
finally:
    if proof is not None:r.connection_pool.release(proof)
    if other is not None:r.connection_pool.release(other)
    try:r.delete(*keys.values())
    finally:r.close()
result={'scope':'Raw redis-py RESP3/TLS transport control; held explicit same/different connections; no ADBC, SQL, snapshot, server CPU or production speedup claim. No raw-connection send/read retry. All GET bytes and PONG replies checked.','samples':samples,'complete':len(samples)==224,'caller_retries':0,'wall_seconds':time.monotonic()-started,'redis_py_version':importlib.metadata.version('redis'),'python_version':platform.python_version(),'source_sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest()}
a.out.write_text(json.dumps(result,indent=2)+'\n')
print('Raw transport control complete:',len(samples),'validated sequences')
