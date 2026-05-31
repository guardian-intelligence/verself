package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/verself/deployment-tools/internal/runtime"
)

func applyGarageBootstrap(ctx context.Context, rt *runtime.Runtime, plan *deployPlan) error {
	runtimePath, err := artifactRemotePath(plan, "garage-runtime")
	if err != nil {
		return err
	}
	getter := plan.SiteCfg.ArtifactDelivery.GetterCredentials
	publisher := plan.SiteCfg.ArtifactDelivery.PublisherCredentials
	if getter.EnvironmentFile == "" || getter.AccessKeyIDEnv == "" || getter.SecretAccessKeyEnv == "" {
		return fmt.Errorf("artifact_delivery.getter_credentials is required for Garage bootstrap")
	}
	if publisher.EnvironmentFile == "" || publisher.AccessKeyIDEnv == "" || publisher.SecretAccessKeyEnv == "" {
		return fmt.Errorf("artifact_delivery.publisher_credentials is required for Garage bootstrap")
	}
	values := []string{
		strings.TrimRight(runtimePath, "/") + "/bin/garage",
		plan.SiteCfg.ArtifactDelivery.Bucket,
		getter.EnvironmentFile,
		getter.AccessKeyIDEnv,
		getter.SecretAccessKeyEnv,
		publisher.EnvironmentFile,
		publisher.AccessKeyIDEnv,
		publisher.SecretAccessKeyEnv,
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		item, err := shellQuote(value)
		if err != nil {
			return err
		}
		quoted = append(quoted, item)
	}
	cmd := strings.Join([]string{
		"sudo",
		"GARAGE_BIN=" + quoted[0],
		"GARAGE_BUCKET=" + quoted[1],
		"GETTER_ENV_FILE=" + quoted[2],
		"GETTER_ACCESS_KEY_ENV=" + quoted[3],
		"GETTER_SECRET_KEY_ENV=" + quoted[4],
		"PUBLISHER_ENV_FILE=" + quoted[5],
		"PUBLISHER_ACCESS_KEY_ENV=" + quoted[6],
		"PUBLISHER_SECRET_KEY_ENV=" + quoted[7],
		"/usr/bin/python3 -",
	}, " ")
	if err := rt.SSH.Run(ctx, cmd, strings.NewReader(garageBootstrapPython)); err != nil {
		return err
	}
	if _, err := rt.SSH.Exec(ctx, "sudo /bin/systemctl restart nomad"); err != nil {
		return fmt.Errorf("restart Nomad after artifact credentials: %w", err)
	}
	fmt.Println("verself-deploy: garage reconciled cluster layout and artifact credentials")
	return nil
}

const garageBootstrapPython = `
import json
import os
import pathlib
import re
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request

garage_bin = os.environ["GARAGE_BIN"]
bucket_name = os.environ["GARAGE_BUCKET"]
getter_env_file = pathlib.Path(os.environ["GETTER_ENV_FILE"])
getter_access_key_env = os.environ["GETTER_ACCESS_KEY_ENV"]
getter_secret_key_env = os.environ["GETTER_SECRET_KEY_ENV"]
publisher_env_file = pathlib.Path(os.environ["PUBLISHER_ENV_FILE"])
publisher_access_key_env = os.environ["PUBLISHER_ACCESS_KEY_ENV"]
publisher_secret_key_env = os.environ["PUBLISHER_SECRET_KEY_ENV"]
nodes = [
    {"instance": 0, "s3_port": 3900, "rpc_port": 3901, "admin_port": 3903},
    {"instance": 1, "s3_port": 3910, "rpc_port": 3911, "admin_port": 3913},
    {"instance": 2, "s3_port": 3920, "rpc_port": 3921, "admin_port": 3923},
]
admin_token = pathlib.Path("/etc/garage/admin-token").read_text(encoding="utf-8").strip()

def request(method, endpoint, payload=None, query=None, missing_ok=False):
    url = f"http://127.0.0.1:3903/v2/{endpoint}"
    if query:
        url += "?" + urllib.parse.urlencode(query)
    body = None
    headers = {"Authorization": "Bearer " + admin_token}
    if payload is not None:
        body = json.dumps(payload).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=5) as response:
            data = response.read()
    except urllib.error.HTTPError as exc:
        if missing_ok and exc.code == 404:
            return None
        detail = exc.read().decode(errors="replace")
        raise SystemExit(f"{method} {endpoint} failed with HTTP {exc.code}: {detail}")
    if not data:
        return {}
    return json.loads(data.decode())

def cli(*args):
    completed = subprocess.run(["sudo", "-u", "garage", garage_bin, *args], check=True, text=True, capture_output=True)
    return completed.stdout.strip()

def node_id(node):
    completed = subprocess.run(
        [garage_bin, "-c", f"/etc/garage/garage-{node['instance']}.toml", "node", "id", "-q"],
        check=True,
        text=True,
        capture_output=True,
    )
    full = completed.stdout.strip()
    short, addr = full.split("@", 1)
    return {"id": short, "addr": addr, "full": full}

node_info = [node_id(node) for node in nodes]
rpc_host = node_info[0]["full"]
deadline = time.time() + 60
while True:
    try:
        status = request("GET", "GetClusterStatus")
        break
    except Exception:
        if time.time() >= deadline:
            raise
        time.sleep(1)

by_addr = {entry.get("addr"): entry for entry in status.get("nodes", [])}
for info in node_info[1:]:
    if info["addr"] not in by_addr:
        cli("-h", rpc_host, "--rpc-secret-file", "/etc/garage/rpc-secret", "node", "connect", info["full"])

deadline = time.time() + 60
while True:
    status = request("GET", "GetClusterStatus")
    by_addr = {entry.get("addr"): entry for entry in status.get("nodes", [])}
    if all(by_addr.get(info["addr"], {}).get("isUp") for info in node_info):
        break
    if time.time() >= deadline:
        raise SystemExit(f"garage cluster did not converge: {status}")
    time.sleep(1)

desired_roles = sorted(
    [
        {
            "capacity": 50000000000,
            "id": info["id"],
            "tags": sorted([f"garage-{node['instance']}", "single-node"]),
            "zone": f"garage-{node['instance']}",
        }
        for info, node in zip(node_info, nodes)
    ],
    key=lambda item: item["id"],
)
desired_parameters = {"zoneRedundancy": {"atLeast": 3}}

layout = request("GET", "GetClusterLayout")
current_roles = sorted(
    [
        {
            "id": role["id"],
            "zone": role["zone"],
            "capacity": role.get("capacity"),
            "tags": sorted(role.get("tags", [])),
        }
        for role in layout.get("roles", [])
    ],
    key=lambda item: item["id"],
)
if (
    current_roles != desired_roles
    or layout.get("parameters") != desired_parameters
    or layout.get("stagedRoleChanges")
    or layout.get("stagedParameters")
):
    cli("-h", rpc_host, "--rpc-secret-file", "/etc/garage/rpc-secret", "layout", "revert", "--yes")
    desired_ids = {role["id"] for role in desired_roles}
    for role in current_roles:
        if role["id"] not in desired_ids:
            cli("-h", rpc_host, "--rpc-secret-file", "/etc/garage/rpc-secret", "layout", "remove", role["id"])
    cli("-h", rpc_host, "--rpc-secret-file", "/etc/garage/rpc-secret", "layout", "config", "--redundancy", "3")
    for role in desired_roles:
        args = ["-h", rpc_host, "--rpc-secret-file", "/etc/garage/rpc-secret", "layout", "assign", role["id"], "--zone", role["zone"], "--capacity", str(role["capacity"])]
        for tag in role["tags"]:
            args.extend(["--tag", tag])
        cli(*args)
    layout = request("GET", "GetClusterLayout")
    cli("-h", rpc_host, "--rpc-secret-file", "/etc/garage/rpc-secret", "layout", "apply", "--version", str(layout["version"] + 1))

def api(method, endpoint, payload=None, query=None, missing_ok=False):
    return request(method, endpoint, payload=payload, query=query, missing_ok=missing_ok)

def create_bucket_if_missing(name):
    found = api("GET", "GetBucketInfo", query={"globalAlias": name}, missing_ok=True)
    if found is not None:
        return found
    return api("POST", "CreateBucket", payload={"globalAlias": name})

def key_info(name):
    return api("GET", "GetKeyInfo", query={"search": name, "showSecretKey": "true"}, missing_ok=True)

def create_key_if_missing(name):
    found = key_info(name)
    if found is not None:
        return found
    return api("POST", "CreateKey", payload={"name": name, "neverExpires": True})

def ensure_key_permission(bucket, key, read, write, owner):
    desired = {"read": read, "write": write, "owner": owner}
    for entry in bucket.get("keys", []):
        if entry.get("accessKeyId") == key["accessKeyId"]:
            current = entry.get("permissions", {})
            if all(bool(current.get(name)) == value for name, value in desired.items()):
                return bucket
            deny = {name: True for name, value in desired.items() if not value and bool(current.get(name))}
            if deny:
                bucket = api("POST", "DenyBucketKey", payload={"bucketId": bucket["id"], "accessKeyId": key["accessKeyId"], "permissions": deny})
            break
    return api("POST", "AllowBucketKey", payload={"bucketId": bucket["id"], "accessKeyId": key["accessKeyId"], "permissions": desired})

def assert_env_safe(key):
    for field in ("accessKeyId", "secretAccessKey"):
        value = key.get(field, "")
        if not re.fullmatch(r"[A-Za-z0-9/+=._:-]+", value):
            raise SystemExit(f"Garage generated unsafe {field} for environment-file serialization")

bucket = create_bucket_if_missing(bucket_name)
reader = create_key_if_missing("verself-nomad-artifacts-reader")
publisher = create_key_if_missing("verself-nomad-artifacts-publisher")
assert_env_safe(reader)
assert_env_safe(publisher)
bucket = ensure_key_permission(bucket, reader, True, False, False)
bucket = ensure_key_permission(bucket, publisher, True, True, False)

getter_env_file.parent.mkdir(parents=True, exist_ok=True)
publisher_env_file.parent.mkdir(parents=True, exist_ok=True)
getter_env_file.write_text(
    f"{getter_access_key_env}={reader['accessKeyId']}\n{getter_secret_key_env}={reader['secretAccessKey']}\nAWS_REGION=garage\nAWS_EC2_METADATA_DISABLED=true\n",
    encoding="utf-8",
)
publisher_env_file.write_text(
    f"{publisher_access_key_env}={publisher['accessKeyId']}\n{publisher_secret_key_env}={publisher['secretAccessKey']}\n",
    encoding="utf-8",
)
os.chmod(getter_env_file, 0o600)
os.chmod(publisher_env_file, 0o600)
`
