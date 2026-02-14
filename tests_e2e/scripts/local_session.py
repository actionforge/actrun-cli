import asyncio
import json
import os
import re
import websockets


ACTRUN_PATH = "actrun"


def clean_and_print(text):
    if not text:
        return

    timestamp_pattern = r'\[?\d{4}[/-]\d{2}[/-]\d{2}\s+\d{2}:\d{2}:\d{2}\]?'
    duration_pattern = r'\d+(?:\.\d+)?s'

    text = re.sub(timestamp_pattern, "", text)
    text = re.sub(duration_pattern, "", text)
    text = re.sub(r'actrun-debug-\d+', 'actrun-debug-[REDACTED]', text)

    # remove empty lines left over from the redaction
    lines = [line.strip() for line in text.splitlines() if line.strip()]

    print("\n".join(lines))


async def drain_stream(stream):
    """Read and discard stream output to prevent buffer blocking."""
    while True:
        line = await stream.readline()
        if not line:
            break


async def main():
    graph_dir = os.environ.get("ACT_GRAPH_FILES_DIR", ".")
    graph_path = os.path.join(graph_dir, "local_session.act")

    with open(graph_path, "r") as f:
        graph_content = f.read()

    clean_and_print("Launching local runner")

    env = os.environ.copy()
    env["ACT_NOCOLOR"] = "true"
    env["ACT_LOGLEVEL"] = "warn"

    process = await asyncio.create_subprocess_exec(
        ACTRUN_PATH, "--local",
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
        env=env,
    )

    drain_out = None
    drain_err = None

    try:
        # Read stdout lines until we find LOCAL_WS_PORT
        port = None
        while True:
            line = await asyncio.wait_for(process.stdout.readline(), timeout=10)
            if not line:
                clean_and_print("ERROR: Runner exited before printing port")
                return
            text = line.decode().strip()
            match = re.search(r'LOCAL_WS_PORT=(\d+)', text)
            if match:
                port = int(match.group(1))
                break

        # Drain remaining subprocess output in background
        drain_out = asyncio.create_task(drain_stream(process.stdout))
        drain_err = asyncio.create_task(drain_stream(process.stderr))

        clean_and_print("Connecting to WebSocket")

        pause_count = 0

        async with websockets.connect(f"ws://127.0.0.1:{port}/ws") as websocket:
            async for message in websocket:
                msg = json.loads(message)
                msg_type = msg.get("type")

                if msg_type == "control":
                    if msg["message"] == "runner_connected":
                        clean_and_print("Runner connected! Sending Graph (Paused)")

                        run_payload = {
                            "type": "run",
                            "payload": graph_content,
                            "start_paused": True,
                            "ignore_breakpoints": False,
                            "breakpoints": [],
                        }
                        await websocket.send(json.dumps(run_payload))

                elif msg_type == "log":
                    clean_and_print(f"Log: {msg['message']}")

                elif msg_type == "log_error":
                    clean_and_print(f"LogError: {msg['message']}")

                elif msg_type == "debug_state":
                    pause_count += 1
                    node = msg.get("fullPath", "unknown")
                    clean_and_print(f"DEBUG PAUSED #{pause_count} at node: {node}")

                    await asyncio.sleep(0.2)

                    if pause_count < 3:
                        clean_and_print("Sending STEP command")
                        await websocket.send(json.dumps({"type": "debug_step"}))
                    else:
                        clean_and_print("Sending RESUME command")
                        await websocket.send(json.dumps({"type": "debug_resume"}))

                elif msg_type == "job_finished":
                    clean_and_print("Job Finished Successfully!")
                    break

                elif msg_type == "job_error":
                    clean_and_print(f"Job Error: {msg.get('error', 'unknown')}")
                    break

    finally:
        clean_and_print("Cleaning up")
        try:
            process.terminate()
            await process.wait()
        except ProcessLookupError:
            pass
        if drain_out:
            drain_out.cancel()
        if drain_err:
            drain_err.cancel()


if __name__ == "__main__":
    asyncio.run(main())
