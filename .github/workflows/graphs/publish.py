import os
import sys
import json
import http.client
from urllib.parse import urlparse


def get_env_variable(var_name, exit_on_missing=True):
    value = os.environ.get(var_name)
    if not value and exit_on_missing:
        print(f"No {var_name} provided")
        sys.exit(1)
    return value


def main():
    goos = get_env_variable('OS')
    goarch = get_env_variable('ARCH')
    license_type = get_env_variable('LICENSE')
    sha256 = get_env_variable('SHA256')
    typ = get_env_variable('TYPE')
    version = get_env_variable('VERSION')
    key = get_env_variable('S3_KEY')
    region = get_env_variable('PUBLISH_S3_REGION')
    bucket = get_env_variable('PUBLISH_S3_BUCKET')
    endpoint = get_env_variable('PUBLISH_S3_ENDPOINT')
    publish_url = get_env_variable('PUBLISH_URL')
    publish_secret = get_env_variable('PUBLISH_SECRET')

    if "digitaloceanspaces.com" in endpoint:
        region = "nyc3"

    payload = [{
        "version": version,
        "os": goos,
        "arch": goarch,
        "license": license_type,
        "location": {
            "region": region,
            "bucket": bucket,
            "key": key,
            "endpoint": endpoint
        },
        "sha256": sha256,
        "type": typ
    }]

    headers = {
        'Content-Type': 'application/json',
        'Authorization': f'Bearer {publish_secret}'
    }

    parsed_url = urlparse(publish_url)
    conn = http.client.HTTPSConnection(parsed_url.netloc)

    conn.request("POST", parsed_url.path, body=json.dumps(payload), headers=headers)
    resp = conn.getresponse()
    data = resp.read()

    if resp.status != 201:
        print(f"Failed to publish with status {resp.status}: {data.decode()}")
        sys.exit(1)

    print("Published successfully")

if __name__ == "__main__":
    main()
