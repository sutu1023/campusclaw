"""Tests for the explicit fixture, not evidence of a real model."""
import json
import secrets
import threading
import unittest
import urllib.error
import urllib.request
from http.server import ThreadingHTTPServer

from gateway import Handler, decision


class GatewayFixtureTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        cls.server.token = secrets.token_urlsafe(32)
        cls.server.dimension = 4
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.base = "http://127.0.0.1:" + str(cls.server.server_port)

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join()

    def post(self, endpoint, body, authorized=True):
        headers = {"Content-Type": "application/json"}
        if authorized:
            headers["Authorization"] = "Bearer " + self.server.token
        request = urllib.request.Request(self.base + endpoint, json.dumps(body).encode(), headers)
        try:
            with urllib.request.urlopen(request) as response:
                return response.status, response.headers, response.read()
        except urllib.error.HTTPError as response:
            with response:
                return response.code, response.headers, response.read()

    def test_authentication_precedes_parse(self):
        self.assertEqual(self.post("/v1/embeddings", {}, False)[0], 401)

    def test_bounded_embedding_and_unknown_route(self):
        status, _, raw = self.post("/v1/embeddings", {"input": ["one", "two"]})
        self.assertEqual(status, 200)
        self.assertEqual([len(v["embedding"]) for v in json.loads(raw)["data"]], [4, 4])
        self.assertEqual(self.post("/v1/embeddings", {"input": [""] * 65})[0], 400)
        self.assertEqual(self.post("/unexpected", {})[0], 404)

    def test_explicit_two_step_and_invalid_argument_decisions(self):
        body = {"messages": [{"role": "user", "content": "勾股问题"}], "tools": [
            {"function": {"name": "first"}}, {"function": {"name": "second"}}]}
        first = decision(body)["tool_calls"][0]
        self.assertEqual(first["function"]["name"], "first")
        body["messages"].append({"role": "tool", "tool_call_id": first["id"], "content": "formula data"})
        second = decision(body)["tool_calls"][0]
        self.assertEqual(second["function"]["name"], "second")
        body["messages"].append({"role": "tool", "tool_call_id": second["id"], "content": "unit data"})
        self.assertNotIn("tool_calls", decision(body))
        body["messages"] = [{"role": "user", "content": "勾股参数校验演练"}]
        self.assertEqual(json.loads(decision(body)["tool_calls"][0]["function"]["arguments"]), {"query": 7})
        body["messages"].append({"role": "tool", "content": "local error"})
        self.assertNotIn("tool_calls", decision(body))

    def test_final_response_is_sse(self):
        status, headers, raw = self.post("/v1/chat/completions", {"stream": True, "tool_choice": "none"})
        self.assertEqual(status, 200)
        self.assertEqual(headers["Content-Type"], "text/event-stream")
        self.assertEqual(raw.count(b"data:"), 3)
        self.assertTrue(raw.endswith(b"data: [DONE]\n\n"))


if __name__ == "__main__":
    unittest.main()
