"""Local TCP link emulator for controlled transfer experiments (no TLS).

Each connection/direction has independent bandwidth and a fixed propagation
delay of half --rtt-ms. Chunk serialization is scheduled before propagation;
propagation delays overlap instead of being charged serially per chunk.
This is a userspace model, not a production network or kernel TCP emulator.
"""
import argparse
import asyncio
import contextlib


async def relay(reader, writer, rate, latency):
    queue = asyncio.Queue(maxsize=64)  # At most ~4 MiB buffered per direction.
    loop = asyncio.get_running_loop()

    async def receive():
        tail = loop.time()
        while data := await reader.read(65536):
            tail = max(tail, loop.time()) + len(data) / rate
            await queue.put((tail + latency, data))
        await queue.put((tail + latency, None))

    async def send():
        while True:
            due, data = await queue.get()
            await asyncio.sleep(max(0, due - loop.time()))
            if data is None:
                if writer.can_write_eof():
                    writer.write_eof()
                return
            writer.write(data)
            await writer.drain()

    tasks = [asyncio.create_task(receive()), asyncio.create_task(send())]
    try:
        await asyncio.gather(*tasks)
    finally:
        for task in tasks:
            task.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)


async def main(args):
    connections = set()

    async def handle(reader, writer):
        upstream = None
        directions = []
        try:
            remote, upstream = await asyncio.open_connection('127.0.0.1', args.target)
            directions = [asyncio.create_task(relay(reader, upstream, args.mbps * 1_000_000 / 8, args.rtt_ms / 2000)),
                          asyncio.create_task(relay(remote, writer, args.mbps * 1_000_000 / 8, args.rtt_ms / 2000))]
            await asyncio.gather(*directions)
        except (ConnectionError, OSError):
            pass  # Timed client shutdown can reset a connection.
        finally:
            for task in directions:
                task.cancel()
            await asyncio.gather(*directions, return_exceptions=True)
            for connection in (writer, upstream):
                if connection is not None:
                    connection.close()
                    with contextlib.suppress(ConnectionError, OSError):
                        await connection.wait_closed()

    def accept(reader, writer):
        # Keep handlers alive, and explicitly finish them at server shutdown.
        task = asyncio.create_task(handle(reader, writer))
        connections.add(task)
        task.add_done_callback(connections.discard)

    server = await asyncio.start_server(accept, '127.0.0.1', args.listen)
    print(f'Listening on 127.0.0.1:{args.listen}: {args.mbps} Mb/s per connection/direction, '
          f'{args.rtt_ms} ms added RTT', flush=True)
    async with server:
        try:
            await server.serve_forever()
        finally:
            for task in connections:
                task.cancel()
            await asyncio.gather(*connections, return_exceptions=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--listen', type=int, default=18112)
    parser.add_argument('--target', type=int, default=18111)
    parser.add_argument('--mbps', type=float, default=1000)
    parser.add_argument('--rtt-ms', type=float, default=10)
    options = parser.parse_args()
    if options.mbps <= 0 or options.rtt_ms < 0:
        parser.error('bandwidth must be positive and added RTT nonnegative')
    try:
        asyncio.run(main(options))
    except KeyboardInterrupt:
        pass
