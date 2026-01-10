from http.server import BaseHTTPRequestHandler, HTTPServer
import time

# This is a simple HTTP server that sends "HELLO" every second 0.1 seconds.

class StreamingHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header('Content-Type', 'text/plain')
        self.end_headers()

        for _ in range(10):
            self.wfile.write(b"HELLO\n")
            self.wfile.flush()
            time.sleep(0.1)

def run_server(port=19999):
    server_address = ('', port)
    httpd = HTTPServer(server_address, StreamingHandler)
    print(f"Server running on port {port}")
    httpd.serve_forever()

if __name__ == '__main__':
    run_server()