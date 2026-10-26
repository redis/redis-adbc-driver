import asyncio, json, sys, time
sys.path.insert(0, 'benchmarks/search-hmget')
import link
from types import SimpleNamespace

async def main():
    size=8*1024*1024
    payload=b'x'*size
    async def backend(reader,writer):
        request=await reader.readexactly(1)
        writer.write(b'p' if request==b'p' else payload)
        await writer.drain()
        writer.close()
        await writer.wait_closed()
    server=await asyncio.start_server(backend,'127.0.0.1',18114)
    task=asyncio.create_task(link.main(SimpleNamespace(target=18114,listen=18115,mbps=1000,rtt_ms=10)))
    await asyncio.sleep(.1)
    results=[]
    try:
        for _ in range(3):
            reader,writer=await asyncio.open_connection('127.0.0.1',18115)
            start=time.perf_counter();writer.write(b'p');await writer.drain()
            assert await reader.readexactly(1)==b'p'
            rtt=time.perf_counter()-start;writer.close();await writer.wait_closed()
            reader,writer=await asyncio.open_connection('127.0.0.1',18115)
            start=time.perf_counter();writer.write(b'd');await writer.drain()
            actual=await reader.readexactly(size);elapsed=time.perf_counter()-start
            assert actual==payload
            writer.close();await writer.wait_closed()
            results.append({'bytes':size,'ping_seconds':rtt,'transfer_seconds':elapsed,'ideal_seconds':.010+size/125000000,'effective_mbps_including_latency':size*8/elapsed/1000000})
        print(json.dumps(results,indent=2))
    finally:
        task.cancel()
        await asyncio.gather(task,return_exceptions=True)
        server.close();await server.wait_closed()

asyncio.run(main())
