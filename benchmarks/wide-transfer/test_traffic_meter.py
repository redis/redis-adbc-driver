"""Local-socket integration check for both byte counters and proxy cleanup."""
import socket
import socketserver
import threading
import unittest

from traffic_meter import TrafficMeter


class Echo(socketserver.BaseRequestHandler):
    def handle(self):
        while data := self.request.recv(65536):
            self.request.sendall(data)


class TrafficMeterTests(unittest.TestCase):
    def test_both_directions_and_clean_shutdown(self):
        with socketserver.TCPServer(('127.0.0.1', 0), Echo) as server:
            thread = threading.Thread(target=server.serve_forever)
            thread.start()
            try:
                with TrafficMeter(server.server_address[1]) as meter:
                    payload = b'payload\x00' * 20000
                    with socket.create_connection(('127.0.0.1', meter.port), timeout=5) as client:
                        client.sendall(payload)
                        client.shutdown(socket.SHUT_WR)
                        chunks = []
                        while data := client.recv(65536):
                            chunks.append(data)
                    self.assertEqual(b''.join(chunks), payload)
                    self.assertEqual(meter.snapshot(), [len(payload), len(payload)])
                self.assertFalse(meter.process.is_alive())
            finally:
                server.shutdown()
                thread.join(5)


if __name__ == '__main__':
    unittest.main()
