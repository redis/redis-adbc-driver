"""Optional unthrottled TCP byte meter, in a separate process.

This measures application TCP payload in both directions, including connection
setup. It excludes TCP/IP headers; this experiment uses plaintext. Proxy timings are a
separate diagnostic, never mixed with direct-path benchmark timings.
"""
import asyncio
import contextlib
import multiprocessing


def serve(target_port, counts, stop, ready):
    async def run():
        handlers = set()

        async def relay(reader, writer, direction):
            while data := await reader.read(65536):
                with counts.get_lock():
                    counts[direction] += len(data)
                writer.write(data)
                await writer.drain()
            if writer.can_write_eof():
                writer.write_eof()

        async def handle(reader, writer):
            upstream = None
            tasks = []
            try:
                remote, upstream = await asyncio.open_connection('127.0.0.1', target_port)
                tasks = [asyncio.create_task(relay(reader, upstream, 0)),
                         asyncio.create_task(relay(remote, writer, 1))]
                await asyncio.gather(*tasks)
            except (ConnectionError, OSError):
                pass
            finally:
                for task in tasks:
                    task.cancel()
                await asyncio.gather(*tasks, return_exceptions=True)
                for stream in (writer, upstream):
                    if stream is not None:
                        stream.close()
                        with contextlib.suppress(ConnectionError, OSError):
                            await stream.wait_closed()

        def accept(reader, writer):
            task = asyncio.create_task(handle(reader, writer))
            handlers.add(task)
            task.add_done_callback(handlers.discard)

        server = await asyncio.start_server(accept, '127.0.0.1', 0)
        ready.send(server.sockets[0].getsockname()[1])
        ready.close()
        async with server:
            while not stop.is_set():
                await asyncio.sleep(.05)
        for task in list(handlers):
            task.cancel()
        await asyncio.gather(*handlers, return_exceptions=True)

    asyncio.run(run())


class TrafficMeter:
    def __init__(self, target_port):
        ctx = multiprocessing.get_context('spawn')
        self.counts = ctx.Array('Q', [0, 0])
        self.stop = ctx.Event()
        read, write = ctx.Pipe(duplex=False)
        self.process = ctx.Process(target=serve, args=(target_port, self.counts, self.stop, write))
        self.process.start()
        write.close()
        if not read.poll(15):
            self.close()
            raise RuntimeError('Traffic meter failed to start')
        self.port = read.recv()
        read.close()

    def snapshot(self):
        with self.counts.get_lock():
            return list(self.counts[:])

    def close(self):
        self.stop.set()
        self.process.join(5)
        if self.process.is_alive():
            self.process.terminate()
            self.process.join(5)
            raise RuntimeError('Traffic meter required forced termination')

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.close()
