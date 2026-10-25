#!/usr/bin/env python3
"""Deterministic test-only gateway; NOT a real-model acceptance substitute.

Run only on an isolated verification network. Requires APITEST_GATEWAY_TOKEN;
never reads MCP credentials, logs request bodies, or forwards network requests.
The public question marker 参数校验演练 produces one intentionally invalid call.
"""
import argparse
import hmac
import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def decision(body):
    messages = body.get("messages", [])
    previous = [m for m in messages if m.get("role") == "tool"]
    question = next((m.get("content", "") for m in reversed(messages) if m.get("role") == "user"), "")
    tools = body.get("tools", [])
    bad_arguments = "参数校验演练" in question
    count = len(previous)
    if not tools or count >= (1 if bad_arguments else 2):
        return {"role": "assistant", "content": "工具阶段结束，请继续引导学生。"}
    function = tools[min(count, len(tools) - 1)]["function"]
    arguments = {"query": 7} if bad_arguments else {"query": "勾股公式适用条件" if count == 0 else "长度单位关系和换算方向"}
    return {"role": "assistant", "content": "", "tool_calls": [{
        "id": "fixture_" + str(count + 1), "type": "function",
        "function": {"name": function["name"], "arguments": json.dumps(arguments, ensure_ascii=False)},
    }]}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass  # No access logs: headers, prompts and results stay out of logs.

    def send_json(self, status, value):
        data = json.dumps(value, ensure_ascii=False).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_POST(self):
        expected = ("Bearer " + self.server.token).encode()
        if not hmac.compare_digest(self.headers.get("Authorization", "").encode(), expected):
            self.send_json(401, {"error": "unauthorized"})
            return
        try:
            size = int(self.headers.get("Content-Length", "0"))
            if not 0 < size <= 1024 * 1024:
                raise ValueError()
            body = json.loads(self.rfile.read(size))
            if not isinstance(body, dict):
                raise ValueError()
        except (ValueError, UnicodeDecodeError):
            self.send_json(400, {"error": "invalid test request"})
            return
        if self.path == "/v1/embeddings":
            inputs = body.get("input", [])
            inputs = [inputs] if isinstance(inputs, str) else inputs
            if not isinstance(inputs, list) or not 0 < len(inputs) <= 64:
                self.send_json(400, {"error": "invalid test input"})
                return
            vector = [1.0] + [0.0] * (self.server.dimension - 1)
            self.send_json(200, {"data": [{"index": i, "embedding": vector} for i in range(len(inputs))]})
        elif self.path == "/v1/chat/completions":
            if body.get("stream"):
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.end_headers()
                for text in ("先检查已知量和公式适用条件。", "再统一单位，尝试列出下一步关系。"):
                    value = {"choices": [{"delta": {"content": text}}]}
                    self.wfile.write(("data: " + json.dumps(value, ensure_ascii=False) + "\n\n").encode())
                    self.wfile.flush()
                self.wfile.write(b"data: [DONE]\n\n")
            else:
                self.send_json(200, {"choices": [{"message": decision(body)}]})
        else:
            self.send_json(404, {"error": "not found"})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", required=True, help="private verification bridge address, not a public listener")
    parser.add_argument("--port", type=int, default=18091)
    parser.add_argument("--dimension", type=int, default=1536)
    args = parser.parse_args()
    token = os.environ.get("APITEST_GATEWAY_TOKEN", "")
    if not token or not 1 <= args.dimension <= 65536:
        parser.error("a test token and valid dimension are required")
    server = ThreadingHTTPServer((args.host, args.port), Handler)
    server.token = token
    server.dimension = args.dimension
    server.serve_forever()


if __name__ == "__main__":
    main()
