"""Qualify named tokens against an isolated copy of the real Beanaries stack.

Generated Compose files and credentials are private local artifacts, never source.
Requires Docker Compose, the Beanaries checkout, and a locally built Spaniel image.
"""
import argparse
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import time
import urllib.error
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--beanaries", type=Path, default=Path.home() / "src/beanaries")
    parser.add_argument("--output", type=Path, default=Path(".local/beanaries-security"))
    parser.add_argument("--image", default="spaniel:issue-133")
    parser.add_argument("--project", default="beanaries-security-133")
    parser.add_argument("--verify-only", action="store_true")
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    compose_file = output / "compose.json"
    credentials_file = output / "credentials.json"
    base = "http://127.0.0.1:18288"
    ingestion = "http://127.0.0.1:18328"

    def run(command, **kwargs):
        return subprocess.run(command, check=True, text=True, **kwargs)

    def compose(*command, **kwargs):
        return run(["docker", "compose", "-p", args.project, "-f", str(compose_file), *command], **kwargs)

    def api(path, method="GET", data=None, token=None, origin=base):
        headers = {"Content-Type": "application/json"}
        if token:
            headers["Authorization"] = "Bearer " + token
        request = urllib.request.Request(origin + path, data=json.dumps(data).encode() if data is not None else None, headers=headers, method=method)
        try:
            with urllib.request.urlopen(request, timeout=20) as response:
                return response.status, json.load(response)
        except urllib.error.HTTPError as error:
            return error.code, json.load(error)

    if not args.verify_only:
        example = args.beanaries / "kafka/.env.example"
        target = args.beanaries / "kafka/.env"
        if not target.exists():
            shutil.copyfile(example, target)
        env = dict(os.environ)
        # The qualification stack uses its own local MinIO, never production S3.
        env.update(KAFKA_S3_ACCESS_KEY="minioadmin", KAFKA_S3_SECRET_KEY="minioadmin")
        raw = run(["docker", "compose", "-f", str(args.beanaries / "docker-compose.yml"), "-f", str(args.beanaries / "docker-compose.override.yml"), "config", "--format", "json"], capture_output=True, env=env).stdout
        config = json.loads(raw)
        config["name"] = args.project
        for name in config.get("networks", {}):
            config["networks"][name] = {"name": f"{args.project}-{name}"}
        for name in config.get("volumes", {}):
            config["volumes"][name] = {"name": f"{args.project}-{name}"}
        for service in config["services"].values():
            service.pop("container_name", None)
            for port in service.get("ports", []):
                port.update(published="0", host_ip="127.0.0.1")
        # Keep qualification reproducible even before the companion Beanaries
        # image fix lands: official MinIO Docker Hub images were withdrawn.
        source_context = str(Path(__file__).resolve().parent / "containers")
        for service, image, target in [("minio", "beanaries/minio:source-7aac2a2", "server"), ("minio-init", "beanaries/mc:source-77f82e1", "client")]:
            config["services"][service].update(image=image, build={"context": source_context, "dockerfile": "minio.Dockerfile", "target": target})
        certs = output / "certs"
        certs.mkdir(exist_ok=True)
        if not (certs / "dev.pem").exists():
            run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", str(certs / "dev-key.pem"), "-out", str(certs / "dev.pem"), "-days", "7", "-subj", "/CN=beanaries.localhost", "-addext", "subjectAltName=DNS:*.beanaries.localhost,DNS:beanaries.localhost,DNS:localhost"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            (certs / "dev-key.pem").chmod(0o600)
        for volume in config["services"]["caddy"].get("volumes", []):
            if volume.get("target") == "/etc/caddy/certs":
                volume["source"] = str(certs)
        config["services"]["gatus"].setdefault("environment", {}).setdefault("SENTRY_URL", "https://errors.beanaries.com")
        spaniel = config["services"]["spaniel"]
        spaniel.pop("build", None)
        spaniel.pop("depends_on", None)
        spaniel.update(image=args.image, command=["--no-browser", "--db-path=/data/spaniel.duckdb"])
        spaniel["ports"] = [{"target": port, "published": str(host), "host_ip": "127.0.0.1"} for port, host in [(8080, 18288), (4317, 18327), (4318, 18328)]]
        password_file = output / "encryption-password.txt"
        if not password_file.exists():
            password_file.write_text(secrets.token_urlsafe(32))
            password_file.chmod(0o600)
        spaniel.setdefault("environment", {}).update(SPANIEL_ENCRYPTION_PASSWORD=password_file.read_text(), SPANIEL_SELF_MONITOR="false")
        for name in ["kafka-connect", "kafka-connect-init"]:
            config["services"][name].setdefault("environment", {}).update(KAFKA_S3_ACCESS_KEY="minioadmin", KAFKA_S3_SECRET_KEY="minioadmin", KAFKA_S3_ENDPOINT="http://minio:9000")
        compose_file.write_text(json.dumps(config, indent=2))
        compose_file.chmod(0o600)
        compose("up", "-d", "--no-build", "spaniel")
        for attempt in range(60):
            try:
                if api("/api/health")[0] == 200:
                    break
            except OSError:
                pass
            time.sleep(1)
        else:
            raise RuntimeError("Spaniel never became ready")
        if credentials_file.exists():
            credentials = json.loads(credentials_file.read_text())
        else:
            credentials = {}
            for name in ["qualification-admin", *config["services"]]:
                status, result = api("/api/security/tokens", "POST", {"name": f"beanaries/{name}"})
                if status != 200:
                    raise RuntimeError(f"Cannot create token for {name}: HTTP {status}")
                credentials[name] = result["data"]["secret"]
            credentials_file.write_text(json.dumps(credentials))
            credentials_file.chmod(0o600)
        for name, service in config["services"].items():
            service.setdefault("environment", {}).update(OTEL_EXPORTER_OTLP_ENDPOINT="http://spaniel:4317", OTEL_EXPORTER_OTLP_PROTOCOL="grpc", OTEL_EXPORTER_OTLP_INSECURE="true", OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer%20" + credentials[name], OTEL_SERVICE_NAME=name)
        config["services"]["keycloak"]["environment"].update(KC_TRACING_HEADER_AUTHORIZATION="Bearer " + credentials["keycloak"], KC_TRACING_ENDPOINT="http://spaniel:4317", KC_TRACING_SAMPLER_TYPE="always_on")
        config["services"]["temporal"]["environment"].update(OTEL_EXPORTER_OTLP_TRACES_HEADERS="Authorization=Bearer%20" + credentials["temporal"], OTEL_EXPORTER_OTLP_TRACES_ENDPOINT="http://spaniel:4317", OTEL_TRACES_EXPORTER="otlp")
        config["services"]["gate"]["environment"]["SPANIEL_API_TOKEN"] = credentials["gate"]
        for volume in config["services"]["gate"].get("volumes", []):
            if volume.get("target") == "/etc/caddy/Caddyfile":
                text = Path(volume["source"]).read_text()
                anchor = "reverse_proxy {$OTLP_UPSTREAM:alloy:4318} {"
                if "header_up Authorization" not in text:
                    text = text.replace(anchor, anchor + '\n\t\t\theader_up Authorization "Bearer {$SPANIEL_API_TOKEN}"')
                gate_config = output / "gate.Caddyfile"
                gate_config.write_text(text)
                volume["source"] = str(gate_config)
        compose_file.write_text(json.dumps(config, indent=2))
        status, _ = api("/api/security", "PUT", {"enabled": True}, credentials["qualification-admin"])
        if status != 200:
            raise RuntimeError(f"Cannot enable security: HTTP {status}")
        # Preserve partial startup diagnostics if an unrelated stack service fails.
        with (output / "startup.log").open("w") as log:
            result = subprocess.run(["docker", "compose", "-p", args.project, "-f", str(compose_file), "up", "-d", "--build"], stdout=log, stderr=subprocess.STDOUT)
        print(f"Full-stack startup exit code: {result.returncode}; diagnostics: {output / 'startup.log'}")

    credentials = json.loads(credentials_file.read_text())
    raw = compose("ps", "--all", "--format", "json", capture_output=True).stdout
    containers = json.loads(raw) if raw.lstrip().startswith("[") else [json.loads(line) for line in raw.splitlines() if line.strip()]
    report = []
    for container in containers:
        name = container["Service"]
        token = credentials.get(name)
        if not token:
            continue
        # A log bridge covers infrastructure without native OTLP exporters.
        # This is real container output, not a fabricated success signal.
        logs = subprocess.run(["docker", "logs", "--tail", "5", container["ID"]], text=True, capture_output=True)
        message = (logs.stdout + logs.stderr)[-4096:]
        payload = {"resourceLogs": [{"resource": {"attributes": [{"key": "service.name", "value": {"stringValue": name}}]}, "scopeLogs": [{"scope": {"name": "beanaries-docker-log-bridge"}, "logRecords": [{"timeUnixNano": str(time.time_ns()), "body": {"stringValue": message or "No container log output"}}]}]}]}
        status, _ = api("/v1/logs", "POST", payload, token, ingestion)
        report.append({"service": name, "state": container["State"], "health": container.get("Health", ""), "exit_code": container.get("ExitCode", 0), "authenticated_log_export": status == 200})
    for signal in ["traces", "metrics", "logs"]:
        status, body = api(f"/v1/{signal}", "POST", {}, origin=ingestion)
        assert status == 401 and body.get("code") == "token_required", (signal, status)
        status, body = api(f"/v1/{signal}", "POST", {}, "invalid-example", ingestion)
        assert status == 401 and body.get("code") == "token_invalid", (signal, status)
    (output / "report.json").write_text(json.dumps(report, indent=2))
    print(json.dumps(report, indent=2))
    present = {row["service"] for row in report}
    missing = set(credentials) - {"qualification-admin"} - present
    if missing:
        print("Services not created: " + ", ".join(sorted(missing)))
        raise RuntimeError("Full-stack startup was incomplete; inspect startup.log")
    if not report or not all(row["authenticated_log_export"] for row in report):
        raise RuntimeError("Some service credentials could not export telemetry")
    if any(row["state"] not in ["running", "exited"] or row["health"] == "unhealthy" or row["state"] == "exited" and row["exit_code"] != 0 for row in report):
        raise RuntimeError("Some stack services are unhealthy; inspect the report and startup log")


if __name__ == "__main__":
    main()
