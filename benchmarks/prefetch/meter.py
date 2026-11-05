"""Unthrottled TLS-transparent byte diagnostic, separate from direct timings."""
import socket
import socketserver
import threading


class Meter:
    def __init__(self, target):
        self.target = target
        self.counts = [0, 0]
        self.lock = threading.Lock()
        self.handlers = set()
        meter = self

        class Handler(socketserver.BaseRequestHandler):
            def handle(self):
                done = threading.Event()
                with meter.lock:
                    meter.handlers.add(done)
                try:
                    with socket.create_connection(meter.target, timeout=30) as upstream:
                        upstream.settimeout(None)
                        def relay(source, destination, direction):
                            try:
                                while data := source.recv(65536):
                                    with meter.lock:
                                        meter.counts[direction] += len(data)
                                    destination.sendall(data)
                            except OSError:
                                pass
                            finally:
                                try:
                                    destination.shutdown(socket.SHUT_WR)
                                except OSError:
                                    pass
                        forward = threading.Thread(target=relay, args=(self.request, upstream, 0))
                        forward.start()
                        relay(upstream, self.request, 1)
                        forward.join()
                finally:
                    done.set()

        class Server(socketserver.ThreadingTCPServer):
            daemon_threads = True

        self.server = Server(('127.0.0.1', 0), Handler)
        self.port = self.server.server_address[1]
        self.thread = threading.Thread(target=self.server.serve_forever)
        self.thread.start()

    def close(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()
        with self.lock:
            handlers = list(self.handlers)
        for done in handlers:
            if not done.wait(5):
                raise RuntimeError('Byte-meter connection did not drain')
        with self.lock:
            return list(self.counts)
