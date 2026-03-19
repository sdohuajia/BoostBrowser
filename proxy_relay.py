import sys
import asyncio
import socket
import socks
import argparse

# Robust HTTP-to-SOCKS/HTTP Relay for Local Authentication Bypass
# Chrome connects to this relay via HTTP Proxy (no auth)
# This relay connects to the remote proxy using Auth (SOCKS or HTTP)

async def pipe(reader, writer):
    try:
        while True:
            data = await reader.read(8192)
            if not data:
                break
            writer.write(data)
            await writer.drain()
    except Exception:
        pass
    finally:
        try:
            writer.close()
            await writer.wait_closed()
        except: pass

async def handle_client(reader, writer, remote_host, remote_port, username, password, proxy_type):
    try:
        # 1. Parse HTTP Request Line (e.g. "CONNECT google.com:443 HTTP/1.1" or "GET http://... HTTP/1.1")
        data = await reader.readuntil(b'\r\n')
        line = data.decode().strip()
        if not line:
            writer.close()
            return
            
        parts = line.split()
        if len(parts) < 3:
            writer.close()
            return
            
        method, target, version = parts
        
        # 2. Extract Destination Host and Port
        if method == "CONNECT":
            # Target is "host:port"
            if ":" in target:
                dest_host, dest_port = target.split(":")
                dest_port = int(dest_port)
            else:
                dest_host = target
                dest_port = 443
            
            # Read rest of headers
            while True:
                h = await reader.readuntil(b'\r\n')
                if h == b'\r\n': break
                
        else:
            # Regular GET/POST (Target is full URL)
            import urllib.parse
            url = urllib.parse.urlparse(target)
            dest_host = url.hostname
            dest_port = url.port if url.port else 80
            # Note: For simple forwarding, we should probably keep the headers
            # But we'll just handle it as a TCP stream for now.
            # This works for most proxies.
            
        # 3. Connect to Destination via Remote Proxy
        loop = asyncio.get_event_loop()
        def connect_via_proxy():
            s = socks.socksocket()
            # Set target proxy type (SOCKS5 or HTTP)
            ptype = socks.SOCKS5 if proxy_type.lower().startswith('socks') else socks.HTTP
            # rdns=True ensures DNS is resolved on the proxy server (fixes DNS leaks/poisoning)
            s.set_proxy(ptype, remote_host, remote_port, username=username, password=password, rdns=True)
            s.settimeout(20)
            try:
                s.connect((dest_host, dest_port))
                s.setblocking(False)
            except Exception as e:
                with open("proxy_relay.log", "a") as f:
                    f.write(f"Connect failed to {dest_host}: {e}\n")
                raise e
            return s
            
        try:
            remote_sock = await loop.run_in_executor(None, connect_via_proxy)
            remote_reader, remote_writer = await asyncio.open_connection(sock=remote_sock)
            
            # 4. Success Response to Chrome
            if method == "CONNECT":
                writer.write(b"HTTP/1.1 200 Connection Established\r\n\r\n")
                await writer.drain()
            else:
                # For non-CONNECT, we need to send the original request to the remote
                remote_writer.write(f"{line}\r\n".encode())
                # (Remaining headers will be handled by pipe)
                pass

            # 5. Start Piping
            await asyncio.gather(
                pipe(reader, remote_writer),
                pipe(remote_reader, writer)
            )
            
        except Exception as e:
            # Send error to Chrome
            if not writer.is_closing():
                writer.write(f"HTTP/1.1 502 Bad Gateway\r\n\r\nRelay Error: {str(e)}".encode())
                await writer.drain()
            writer.close()

    except Exception as e:
        try:
            writer.close()
            await writer.wait_closed()
        except: pass

async def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--local-port", type=int, required=True)
    parser.add_argument("--remote-host", required=True)
    parser.add_argument("--remote-port", type=int, required=True)
    parser.add_argument("--user")
    parser.add_argument("--pwd")
    parser.add_argument("--type", default="socks5")
    args = parser.parse_args()

    # Start as HTTP Proxy
    server = await asyncio.start_server(
        lambda r, w: handle_client(r, w, args.remote_host, args.remote_port, args.user, args.pwd, args.type),
        '127.0.0.1', args.local_port
    )

    async with server:
        await server.serve_forever()

if __name__ == "__main__":
    with open("proxy_relay.log", "a") as f:
        import datetime
        f.write(f"\n--- Relay Started at {datetime.datetime.now()} ---\n")
    try:
        asyncio.run(main())
    except Exception as e:
        with open("proxy_relay.log", "a") as f:
            f.write(f"Relay crashed on startup: {e}\n")
