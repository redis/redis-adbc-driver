"""Count benchmark worker Redis bytes through a loopback TCP proxy.

Use only with an isolated standalone non-TLS Redis server. Monitoring uses
its own direct connection and is excluded from these byte counts.
"""
import socket
import socketserver
import threading
from urllib.parse import urlsplit


class Proxy:
    def __init__(self, uri):
        target = urlsplit(uri)
        if target.scheme != 'redis' or target.hostname not in ('localhost', '127.0.0.1'):
            raise ValueError('use an isolated standalone loopback redis:// server')
        self.bytes = {'request': 0, 'reply': 0}
        self.lock = threading.Lock()
        owner = self
        class Handler(socketserver.BaseRequestHandler):
            def handle(self):
                with socket.create_connection((target.hostname, target.port or 6379)) as remote:
                    def forward(source, dest, kind):
                        try:
                            while data := source.recv(65536):
                                with owner.lock:
                                    owner.bytes[kind] += len(data)
                                dest.sendall(data)
                        except OSError:
                            pass
                        finally:
                            try: dest.shutdown(socket.SHUT_WR)
                            except OSError: pass
                    sender = threading.Thread(target=forward,args=(self.request,remote,'request'),daemon=True)
                    sender.start()
                    forward(remote,self.request,'reply')
                    sender.join()
        class Server(socketserver.ThreadingTCPServer):
            daemon_threads = True
        self.server = Server(('127.0.0.1',0),Handler)
        self.port = self.server.server_address[1]

    def start(self):
        threading.Thread(target=self.server.serve_forever,daemon=True).start()

    def snapshot(self):
        with self.lock:
            return dict(self.bytes)
