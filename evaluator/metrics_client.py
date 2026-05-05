#!/usr/bin/env python3
"""
Langfuse Metrics Client — query traces, scores, and observations.

Used by analyze.py for CLI analysis, or import as a library:

    from metrics_client import LangfuseMetricsClient
    client = LangfuseMetricsClient()
    traces = client.get_traces(tags=["investing"], limit=50)
"""

import json
import os
import base64
from datetime import datetime, timedelta, timezone
from urllib.request import Request, urlopen
from urllib.error import HTTPError
from collections import defaultdict


def load_env():
    """Load .env file if present."""
    env_path = os.path.join(os.path.dirname(__file__), ".env")
    if os.path.exists(env_path):
        with open(env_path) as f:
            for line in f:
                line = line.strip()
                if line and not line.startswith("#") and "=" in line:
                    key, _, val = line.partition("=")
                    os.environ.setdefault(key.strip(), val.strip())


class LangfuseMetricsClient:
    def __init__(self, url=None, pk=None, sk=None):
        load_env()
        self.base = (url or os.environ.get("LANGFUSE_URL", "http://localhost:3100")).rstrip("/")
        self.pk = pk or os.environ.get("LANGFUSE_PK", "pk-orch-2026-lf-a1b2c3d4e5f6")
        self.sk = sk or os.environ.get("LANGFUSE_SK", "sk-orch-2026-lf-a1b2c3d4e5f6")

    def _req(self, path, params=None):
        """GET request to Langfuse public API."""
        url = f"{self.base}/api/public{path}"
        if params:
            q = "&".join(f"{k}={v}" for k, v in params.items() if v is not None)
            if q:
                url += f"?{q}"
        req = Request(url)
        cred = base64.b64encode(f"{self.pk}:{self.sk}".encode()).decode()
        req.add_header("Authorization", f"Basic {cred}")
        try:
            with urlopen(req, timeout=30) as resp:
                if resp.status == 204:
                    return None
                data = json.loads(resp.read())
                if isinstance(data, dict) and "data" in data:
                    return data["data"]
                return data
        except HTTPError as e:
            body = e.read().decode() if e.fp else ""
            print(f"API error {e.code}: {body[:200]}")
            return None

    def _post(self, path, body):
        """POST request to Langfuse public API."""
        url = f"{self.base}/api/public{path}"
        data = json.dumps(body).encode()
        req = Request(url, data=data, method="POST")
        req.add_header("Content-Type", "application/json")
        cred = base64.b64encode(f"{self.pk}:{self.sk}".encode()).decode()
        req.add_header("Authorization", f"Basic {cred}")
        try:
            with urlopen(req, timeout=30) as resp:
                if resp.status == 204:
                    return None
                return json.loads(resp.read())
        except HTTPError as e:
            body_text = e.read().decode() if e.fp else ""
            print(f"API error {e.code} on POST {path}: {body_text[:200]}")
            return None

    def post_score(self, trace_id, name, value, data_type, comment=""):
        """Post a score to a trace."""
        body = {
            "traceId": trace_id,
            "name": name,
            "value": value,
            "dataType": data_type,
            "source": "API",
            "comment": comment,
        }
        return self._post("/scores", body)

    def get_traces(self, tags=None, user_id=None, session_id=None,
                   from_date=None, to_date=None, limit=100):
        """Get traces with optional filters."""
        params = {"limit": str(limit)}
        if tags:
            params["tags"] = json.dumps(tags) if isinstance(tags, list) else tags
        if user_id:
            params["userId"] = user_id
        if session_id:
            params["sessionId"] = session_id
        if from_date:
            params["fromTimestamp"] = from_date if isinstance(from_date, str) else from_date.isoformat()
        if to_date:
            params["toTimestamp"] = to_date if isinstance(to_date, str) else to_date.isoformat()
        return self._req("/traces", params) or []

    def get_trace(self, trace_id):
        """Get a single trace with observations."""
        return self._req(f"/traces/{trace_id}")

    def get_observations(self, trace_id=None, type=None, name=None, limit=100):
        """Get observations with optional filters."""
        params = {"limit": str(limit)}
        if trace_id:
            params["traceId"] = trace_id
        if type:
            params["type"] = type
        if name:
            params["name"] = name
        return self._req("/observations", params) or []

    def get_scores(self, trace_id=None, name=None, data_type=None, limit=100):
        """Get scores with optional filters."""
        params = {"limit": str(limit)}
        if trace_id:
            params["traceId"] = trace_id
        if name:
            params["name"] = name
        if data_type:
            params["dataType"] = data_type
        return self._req("/scores", params) or []

    def aggregate_scores(self, score_name=None, group_by="day",
                         from_date=None, to_date=None):
        """Fetch scores and compute aggregates grouped by time period."""
        scores = self.get_scores(name=score_name, limit=100)
        if not scores:
            return {}

        # Filter by date range
        if from_date:
            if isinstance(from_date, str):
                from_date = datetime.fromisoformat(from_date.replace("Z", "+00:00"))
            scores = [s for s in scores if self._parse_ts(s.get("timestamp", "")) >= from_date]
        if to_date:
            if isinstance(to_date, str):
                to_date = datetime.fromisoformat(to_date.replace("Z", "+00:00"))
            scores = [s for s in scores if self._parse_ts(s.get("timestamp", "")) <= to_date]

        # Group by time period
        groups = defaultdict(list)
        for s in scores:
            ts = self._parse_ts(s.get("timestamp", ""))
            if group_by == "day":
                key = ts.strftime("%Y-%m-%d")
            elif group_by == "week":
                key = f"{ts.isocalendar()[0]}-W{ts.isocalendar()[1]:02d}"
            elif group_by == "hour":
                key = ts.strftime("%Y-%m-%d %H:00")
            else:
                key = ts.strftime("%Y-%m")
            groups[key].append(s)

        # Compute aggregates per group
        result = {}
        for key, group_scores in sorted(groups.items()):
            values = [s["value"] for s in group_scores if isinstance(s.get("value"), (int, float))]
            if values:
                result[key] = {
                    "count": len(values),
                    "avg": round(sum(values) / len(values), 2),
                    "min": min(values),
                    "max": max(values),
                    "scores": group_scores,
                }
        return result

    def tool_usage_stats(self, from_date=None, to_date=None):
        """Aggregate tool call frequency and latency from span observations."""
        observations = self.get_observations(type="SPAN", limit=100)
        if from_date or to_date:
            observations = self._filter_by_date(observations, from_date, to_date)

        tool_stats = defaultdict(lambda: {"count": 0, "total_latency_ms": 0})
        for obs in observations:
            name = obs.get("name", "unknown")
            start = obs.get("startTime", "")
            end = obs.get("endTime", "")
            if start and end:
                latency = (self._parse_ts(end) - self._parse_ts(start)).total_seconds() * 1000
                tool_stats[name]["total_latency_ms"] += latency
            tool_stats[name]["count"] += 1

        # Compute averages
        result = {}
        for name, stats in sorted(tool_stats.items(), key=lambda x: x[1]["count"], reverse=True):
            result[name] = {
                "count": stats["count"],
                "avg_latency_ms": round(stats["total_latency_ms"] / max(stats["count"], 1), 1),
                "total_latency_ms": round(stats["total_latency_ms"], 1),
            }
        return result

    def model_comparison(self, from_date=None, to_date=None):
        """Compare token usage and latency across models from generation observations."""
        observations = self.get_observations(type="GENERATION", limit=100)
        if from_date or to_date:
            observations = self._filter_by_date(observations, from_date, to_date)

        model_stats = defaultdict(lambda: {"count": 0, "total_input": 0, "total_output": 0, "total_latency_ms": 0})
        for obs in observations:
            model = obs.get("model", "unknown")
            usage = obs.get("usage", {})
            model_stats[model]["count"] += 1
            model_stats[model]["total_input"] += usage.get("input", 0) or 0
            model_stats[model]["total_output"] += usage.get("output", 0) or 0
            start = obs.get("startTime", "")
            end = obs.get("endTime", "")
            if start and end:
                latency = (self._parse_ts(end) - self._parse_ts(start)).total_seconds() * 1000
                model_stats[model]["total_latency_ms"] += latency

        result = {}
        for model, stats in sorted(model_stats.items(), key=lambda x: x[1]["count"], reverse=True):
            n = max(stats["count"], 1)
            result[model] = {
                "count": stats["count"],
                "total_input_tokens": stats["total_input"],
                "total_output_tokens": stats["total_output"],
                "avg_latency_ms": round(stats["total_latency_ms"] / n, 1),
            }
        return result

    def sessions_summary(self, from_date=None, to_date=None, min_avg_score=None):
        """Group traces by session, compute per-session stats."""
        traces = self.get_traces(limit=100)
        if from_date or to_date:
            traces = self._filter_by_date(traces, from_date, to_date, ts_key="timestamp")

        sessions = defaultdict(list)
        for t in traces:
            sid = t.get("sessionId") or t.get("metadata", {}).get("session", "unknown")
            sessions[sid].append(t)

        result = {}
        for sid, session_traces in sorted(sessions.items()):
            trace_ids = [t["id"] for t in session_traces]
            all_scores = []
            for tid in trace_ids[:20]:  # Limit API calls per session
                scores = self.get_scores(trace_id=tid)
                if scores:
                    all_scores.extend(scores)

            numeric_scores = [s["value"] for s in all_scores if isinstance(s.get("value"), (int, float))]
            avg_score = round(sum(numeric_scores) / len(numeric_scores), 2) if numeric_scores else None

            if min_avg_score is not None and avg_score is not None and avg_score < min_avg_score:
                continue

            result[sid] = {
                "trace_count": len(session_traces),
                "avg_score": avg_score,
                "score_count": len(numeric_scores),
                "tags": list(set(tag for t in session_traces for tag in t.get("tags", []))),
                "first_trace": min(t.get("timestamp", "9999") for t in session_traces),
                "last_trace": max(t.get("timestamp", "") for t in session_traces),
            }
        return result

    def export_traces(self, output_format="json", output_file=None, **filters):
        """Export trace data for external analysis."""
        traces = self.get_traces(**filters, limit=100)
        if not traces:
            return []

        if output_format == "csv" and output_file:
            import csv
            with open(output_file, "w", newline="") as f:
                writer = csv.DictWriter(f, fieldnames=[
                    "id", "name", "userId", "sessionId", "timestamp",
                    "tags", "release", "input", "output"
                ])
                writer.writeheader()
                for t in traces:
                    writer.writerow({
                        "id": t.get("id", ""),
                        "name": t.get("name", ""),
                        "userId": t.get("userId", ""),
                        "sessionId": t.get("sessionId", ""),
                        "timestamp": t.get("timestamp", ""),
                        "tags": ";".join(t.get("tags", [])),
                        "release": t.get("release", ""),
                        "input": str(t.get("input", ""))[:200],
                        "output": str(t.get("output", ""))[:200],
                    })
            return traces

        if output_file:
            with open(output_file, "w") as f:
                json.dump(traces, f, indent=2, default=str)
        return traces

    # ── Helpers ────────────────────────────────────────────────────────

    @staticmethod
    def _parse_ts(ts_str):
        """Parse ISO timestamp string."""
        if not ts_str:
            return datetime.min.replace(tzinfo=timezone.utc)
        ts_str = ts_str.replace("Z", "+00:00")
        try:
            return datetime.fromisoformat(ts_str)
        except (ValueError, TypeError):
            return datetime.min.replace(tzinfo=timezone.utc)

    def _filter_by_date(self, items, from_date=None, to_date=None, ts_key="startTime"):
        """Filter a list of items by date range."""
        filtered = items
        if from_date:
            if isinstance(from_date, str):
                from_date = datetime.fromisoformat(from_date.replace("Z", "+00:00"))
            filtered = [i for i in filtered if self._parse_ts(i.get(ts_key, "")) >= from_date]
        if to_date:
            if isinstance(to_date, str):
                to_date = datetime.fromisoformat(to_date.replace("Z", "+00:00"))
            filtered = [i for i in filtered if self._parse_ts(i.get(ts_key, "")) <= to_date]
        return filtered
