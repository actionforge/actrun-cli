import asyncio
import base64
import json
import os
import re
import hashlib
import struct
import secrets
import requests
import websockets 
from cryptography.hazmat.primitives.ciphers.aead import AESGCM


ACTRUN_PATH = "actrun"
GATEWAY_URL = "https://app.actionforge.dev"

# just a random hello world graph, should be enough for testing the basics
GRAPH_CONTENT = """{
  "editor": {
    "version": {
      "created": "v1.34.0"
    }
  },
  "entry": "start",
  "type": "generic",
  "nodes": [
    {
      "id": "start",
      "type": "core/start@v1",
      "position": {
        "x": 20,
        "y": 10
      }
    },
    {
      "id": "core-print-v1-crab-red-gold",
      "type": "core/print@v1",
      "position": {
        "x": 310,
        "y": 70
      },
      "inputs": {
        "values[0]": null
      }
    },
    {
      "id": "core-const-string-v1-zebra-turquoise-lime",
      "type": "core/const-string@v1",
      "position": {
        "x": 30,
        "y": 210
      },
      "inputs": {
        "value": "Hello World! 🚀"
      }
    }
  ],
  "connections": [
    {
      "src": {
        "node": "core-const-string-v1-zebra-turquoise-lime",
        "port": "result"
      },
      "dst": {
        "node": "core-print-v1-crab-red-gold",
        "port": "values[0]"
      }
    }
  ],
  "executions": [
    {
      "src": {
        "node": "start",
        "port": "exec"
      },
      "dst": {
        "node": "core-print-v1-crab-red-gold",
        "port": "exec"
      }
    }
  ]
}
"""

def clean_and_print(text):
    if not text:
        return

    timestamp_pattern = r'\[?\d{4}[/-]\d{2}[/-]\d{2}\s+\d{2}:\d{2}:\d{2}\]?'
    duration_pattern = r'\d+(?:\.\d+)?s'

    text = re.sub(timestamp_pattern, "", text)
    text = re.sub(duration_pattern, "", text)
    
    # remove empty lines left over from the redaction
    lines = [line.strip() for line in text.splitlines() if line.strip()]

    print("\n".join(lines))


async def stream_reader(stream):
    while True:
        line = await stream.readline()
        if not line:
            break
        clean_and_print(line.decode('utf-8', errors='replace'))


def generate_session_token(session_id: str):
    raw_key = secrets.token_bytes(32)
    session_bytes = session_id.encode("utf-8")
    data_to_hash = session_bytes + raw_key
    full_hash = hashlib.sha256(data_to_hash).digest()
    checksum = full_hash[:4]
    packet = struct.pack("B", len(session_bytes)) + session_bytes + raw_key + checksum
    return base64.b64encode(packet).decode("utf-8"), raw_key

def encrypt_payload(data_dict, key):
    aesgcm = AESGCM(key)
    nonce = os.urandom(12)
    plaintext = json.dumps(data_dict).encode("utf-8")
    ciphertext = aesgcm.encrypt(nonce, plaintext, None)
    return base64.b64encode(nonce + ciphertext).decode("utf-8")

def decrypt_payload(b64_data, key):
    try:
        raw_data = base64.b64decode(b64_data)
        nonce = raw_data[:12]
        ciphertext = raw_data[12:]
        aesgcm = AESGCM(key)
        return json.loads(aesgcm.decrypt(nonce, ciphertext, None).decode("utf-8"))
    except Exception as e:
        print(f"Decryption Error: {e}")
        return None

async def run_browser_session(session_id, shared_key):
    ws_url = GATEWAY_URL.replace("http", "ws") + f"/api/v2/ws/browser/{session_id}"
    clean_and_print("Connecting to Browser WS")
    
    pause_count = 0

    async with websockets.connect(ws_url) as websocket:
        clean_and_print("browser connected")

        async for message in websocket:
            msg_json = json.loads(message)

            if msg_json.get("type") == "control":
                if msg_json["message"] == "runner_connected":
                    clean_and_print("Runner connected! Sending Graph (Paused)...")
                    
                    run_payload = {
                        "type": "run",
                        "payload": GRAPH_CONTENT,
                        "start_paused": True,
                        "ignore_breakpoints": False,
                        "breakpoints": [],
                        "required_version": "v0.0.0"
                    }
                    await websocket.send(json.dumps({
                        "type": "data",
                        "payload": encrypt_payload(run_payload, shared_key)
                    }))

            elif msg_json.get("type") == "data":
                decrypted = decrypt_payload(msg_json["payload"], shared_key)
                if not decrypted:
                    continue

                m_type = decrypted.get("type")
                
                if m_type == "log":
                    clean_and_print(f"Log: {decrypted['message']}")

                elif m_type == "debug_state":
                    pause_count += 1
                    node = decrypted.get('fullPath', 'unknown')
                    
                    clean_and_print(f"\nDEBUG PAUSED #{pause_count} at node: {node}")
                    clean_and_print("⏳ Waiting 2 seconds...")
                    await asyncio.sleep(2)
                    
                    if pause_count == 1:
                        clean_and_print("Sending STEP command...")
                        step_payload = {"type": "debug_step"}
                        await websocket.send(json.dumps({
                            "type": "data",
                            "payload": encrypt_payload(step_payload, shared_key)
                        }))
                    else:
                        clean_and_print("▶Sending RESUME command...")
                        resume_payload = {"type": "debug_resume"}
                        await websocket.send(json.dumps({
                            "type": "data",
                            "payload": encrypt_payload(resume_payload, shared_key)
                        }))

                elif m_type == "job_finished":
                    clean_and_print("\nJob Finished Successfully!")
                    return 

                elif m_type == "job_error":
                    print(f"❌ Job Error: {decrypted['error']}")
                    return

async def main():
    clean_and_print(f"Requesting ID from {GATEWAY_URL}...")
    try:
        resp = requests.post(f"{GATEWAY_URL}/api/v2/session/start")
        resp.raise_for_status()
        session_id = resp.json()["debug_session_id"]

        clean_and_print("Debug Session started")
    except Exception as e:
        clean_and_print(f"❌ Failed to get session: {e}")
        return

    token, shared_key = generate_session_token(session_id)

    env = os.environ.copy()
    clean_gateway = GATEWAY_URL.replace("https://", "").replace("http://", "")
    env["ACT_SESSION_GATEWAY"] = clean_gateway
    env["ACT_SESSION_TOKEN"] = token
    env["ACT_NOCOLOR"] = "true"

    clean_and_print("🏃 Launching Runner (Subprocess)...")

    # 1. Start Subprocess with PIPES (Intercept output)
    process = await asyncio.create_subprocess_exec(
        ACTRUN_PATH,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
        env=env
    )

    stdout_task = asyncio.create_task(stream_reader(process.stdout))
    stderr_task = asyncio.create_task(stream_reader(process.stderr))

    try:
        await run_browser_session(session_id, shared_key)
    finally:
        clean_and_print("Cleaning up...")
        try:
            process.terminate()
            await process.wait()
        except ProcessLookupError:
            pass
        
        stdout_task.cancel()
        stderr_task.cancel()

if __name__ == "__main__":
    asyncio.run(main())