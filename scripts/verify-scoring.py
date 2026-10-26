#!/usr/bin/env python3
"""Reproducible scoring acceptance; operates only on its own fictional Compose project."""
import argparse
import copy
import datetime
import json
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument("--project", default="scoring-it-" + datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%d%H%M%S"))
parser.add_argument("--skip-build", action="store_true")
parser.add_argument("--keep", action="store_true")
args = parser.parse_args()
if not args.project.startswith(("scoring-it-", "credit-scoring-check-")):
    parser.error("project must have an isolated scoring test prefix")
ARTIFACTS = ROOT / ".work" / "scoring" / args.project
ARTIFACTS.mkdir(parents=True, exist_ok=True)
BASE = ["docker", "compose", "-p", args.project, "-f", str(ROOT / "compose.scoring-test.yaml")]


def compose(*argv, input=None):
    return subprocess.run(BASE + list(argv), cwd=ROOT, input=input, text=True, check=True,
                          stdout=subprocess.PIPE, stderr=subprocess.STDOUT).stdout.strip()


def sql(command):
    return compose("exec", "-T", "postgres", "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1",
                   "-U", "scoring_test", "-d", "scoring_test", input=command)


def literal(value):
    return "'" + value.replace("'", "''") + "'"


def request(base, path, payload=None, key=None):
    headers = {"Content-Type": "application/json"}
    if key:
        headers["Idempotency-Key"] = key
    req = urllib.request.Request(base + path, data=None if payload is None else json.dumps(payload).encode(), headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=30) as response:
            return response.status, json.loads(response.read() or b"{}"), response.headers
    except urllib.error.HTTPError as response:
        return response.code, json.loads(response.read() or b"{}"), response.headers


def wait_until(check, description, timeout=120):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        try:
            if check():
                return
        except (OSError, subprocess.CalledProcessError, ValueError):
            pass
        time.sleep(0.5)
    raise AssertionError("timeout: " + description)


def seed():
    if sql("SELECT count(*) FROM scoring_jobs") != "0":
        raise RuntimeError("test database is not empty; use a new project")
    fixtures = {kind: json.loads((ROOT / "credit-engine/testdata/decisions" / filename).read_text())
                for kind, filename in [("ip", "uc04_ip_approved.json"), ("legal", "uc04_le_approved.json")]}
    commands = ["BEGIN; INSERT INTO users(id,full_name,role) VALUES('scale-user','Fictional','CLIENT');"]
    records = []
    for i in range(100):
        kind = "ip" if i % 2 == 0 else "legal"
        fixture = fixtures[kind]
        req = copy.deepcopy(fixture["request"])
        app, company, job = f"scale-app-{i}", f"scale-co-{i}", f"scale-job-{i}"
        inn = ("3" + f"{i:011d}") if kind == "ip" else ("2" + f"{i:09d}")
        req["application_id"] = app
        req["borrower"].update(id=company, inn=inn)
        req["model_version"] = kind + "-v2"
        statements = copy.deepcopy(fixture["service_b_response"])
        if statements:
            statements["borrower_id"] = company
        req["input_snapshot"] = {"id": job, "evaluation_date": "2026-10-23", "collection_id": "",
                                 "statements": statements, "documents": [], "source_states": [],
                                 "indicators": [], "diagnostics": []}
        if i % 10 == 0:
            req["need"]["confirmed_cost_rub"] = None
        if i % 10 in (1, 2):
            req["input_snapshot"]["diagnostics"] = [{"code": "FINANCIAL_DATA_CONFLICT", "message": "Clarify",
                                                       "fields": ["evidence"], "conflict": True}]
        if i % 10 == 2:
            req["scoring"]["ip" if kind == "ip" else "legal_entity"]["active"] = False
        snapshot = json.dumps({"request": req, "facts": {}, "source": {}, "answered": {}})
        commands += [
            f"INSERT INTO companies(id,inn,type,name) VALUES({literal(company)},{literal(inn)},{literal(req['borrower']['type'])},'Fictional');",
            f"INSERT INTO applications(id,number,company_id,created_by,status) VALUES({literal(app)},{literal(app)},{literal(company)},'scale-user','SCORING');",
            f"INSERT INTO scoring_jobs(id,application_id,round,snapshot,next_attempt_at) VALUES({literal(job)},{literal(app)},1,{literal(snapshot)}::jsonb,now()+interval '5 seconds');",
        ]
        records.append((job, req))
    # An abandoned lease must be reclaimed without starting a new round.
    commands.append("UPDATE scoring_jobs SET status='RUNNING',owner_token='dead-worker',lease_until=now()+interval '2 seconds' WHERE id='scale-job-99'; COMMIT;")
    sql("\n".join(commands))
    return records


try:
    if not args.skip_build:
        print("Building isolated images", flush=True)
        (ARTIFACTS / "build.log").write_text(compose("build"))
    print("Starting 2 scoring and 2 bank replicas", flush=True)
    compose("up", "-d", "postgres", "migrate", "data-collector", "credit-engine-scoring", "credit-engine-bank", "credit-engine-gateway",
            "--scale", "credit-engine-scoring=2", "--scale", "credit-engine-bank=2")
    base = "http://" + compose("port", "credit-engine-gateway", "8082")
    wait_until(lambda: request(base, "/readyz")[0] == 200, "scoring readiness")
    records = seed()
    key, frozen = records[3]
    code, original, _ = request(base, "/api/v1/decisions", frozen, key)
    assert code == 200 and original["status"] == "APPROVED", (code, original)
    # The remote operation committed, while the dispatch job has no saved reply.
    # Drop the scorer's final response too: it must resume its prepared bank request.
    sql(f"UPDATE credit_engine_runtime.scoring_runs SET response=NULL WHERE idempotency_key={literal(key)};")
    compose("kill", "credit-engine-scoring", "credit-engine-bank")
    compose("up", "-d", "credit-engine-scoring", "credit-engine-bank", "--scale", "credit-engine-scoring=2", "--scale", "credit-engine-bank=2")
    compose("stop", "data-collector")  # durable bank notifications must survive an unavailable sink
    print("Processing 100 rounds with 3 workers; replaying a committed bank operation", flush=True)
    compose("up", "-d", "data-collector-decision-worker", "--scale", "data-collector-decision-worker=3")
    def rounds_complete():
        failures = sql("SELECT id || ':' || last_error FROM scoring_jobs WHERE status='FAILED'")
        if failures:
            raise AssertionError(failures)
        return sql("SELECT count(*) FROM scoring_jobs WHERE status='COMPLETED'") == "100"
    wait_until(rounds_complete, "100 completed rounds")
    assert sql("SELECT count(*) FROM scoring_jobs WHERE status='FAILED'") == "0"
    assert sql("SELECT count(*) FROM decisions") == "100"
    assert sql("SELECT count(DISTINCT scoring_job_id) FROM decisions") == "100"
    assert sql("SELECT count(*) FROM (SELECT application_id FROM credit_engine_runtime.bank_loans GROUP BY application_id HAVING count(*)>1) s") == "0"
    assert sql("SELECT count(*) FROM application_events WHERE type='scoring.completed'") == "100"
    assert sql("SELECT count(*) FROM decisions WHERE auto_outcome='REJECTED'") == "10", "confirmed stops lost"
    assert int(sql("SELECT count(*) FROM credit_engine_runtime.bank_loans")) >= 1
    code, replay, _ = request(base, "/api/v1/decisions", frozen, key)
    assert code == 200 and replay == original, (code, replay, original)
    changed = copy.deepcopy(frozen)
    changed["borrower"]["name"] = "Changed"
    code, conflict, _ = request(base, "/api/v1/decisions", changed, key)
    assert code == 409 and conflict["code"] == "IDEMPOTENCY_CONFLICT", conflict
    def sample(path):
        return {request(base, path)[2].get("X-Runtime-Upstream") for _ in range(40)}
    scoring_addresses, bank_addresses = sample("/healthz"), sample("/api/v1/bank/state")
    assert len(scoring_addresses) == len(bank_addresses) == 2, (scoring_addresses, bank_addresses)
    before = request(base, "/api/v1/bank/state")[1]
    compose("up", "-d", "data-collector")
    wait_until(lambda: sql("SELECT count(*) FROM credit_engine_runtime.bank_outbox WHERE delivered_at IS NULL") == "0", "outbox delivery")
    assert sql("SELECT count(*) FROM credit_event_inbox") == sql("SELECT count(*) FROM credit_engine_runtime.bank_outbox")
    compose("kill", "data-collector-decision-worker", "credit-engine-scoring", "credit-engine-bank")
    compose("up", "-d", "data-collector-decision-worker", "credit-engine-scoring", "credit-engine-bank",
            "--scale", "data-collector-decision-worker=3", "--scale", "credit-engine-scoring=2", "--scale", "credit-engine-bank=2")
    wait_until(lambda: request(base, "/api/v1/bank/state")[0] == 200, "bank restart")
    after = request(base, "/api/v1/bank/state")[1]
    assert before == after, "bank state reset on restart"
    assert sql("SELECT count(*) FROM decisions") == "100"
    summary = {"jobs": 100, "worker_replicas": 3, "scoring_replicas": 2, "bank_replicas": 2,
               "scoring_upstreams": sorted(scoring_addresses), "bank_upstreams": sorted(bank_addresses),
               "loans": int(sql("SELECT count(*) FROM credit_engine_runtime.bank_loans")),
               "bank_state_preserved": True, "prepared_recovery": True, "outbox_recovered": True}
    (ARTIFACTS / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    print(json.dumps(summary, indent=2), flush=True)
finally:
    try:
        (ARTIFACTS / "containers.log").write_text(compose("logs", "--no-color"))
    finally:
        if not args.keep:
            compose("down", "--remove-orphans", "--volumes")
    print("Artifacts: " + str(ARTIFACTS), flush=True)
