# VM delivery

The optional GitHub Actions deployment packages a tested push to `main` for a Linux x86-64 VM. Pull requests run the application checks without packaging or deployment. A VM timer makes outbound HTTPS requests to fetch the artifact only when current `main` has a successful CI run. No GitHub runner needs private network access. The workflow ships the API, migration binary and built web files. It uses the existing PostgreSQL database and HTTPS proxy.

## Prepare the VM

- Provision a VM with systemd, Bash, GNU tar, curl, flock, jq, unzip, sudo, SSH and PostgreSQL. CI uses PostgreSQL 17. The inspected VM uses PostgreSQL 18.6, where the deployed revision passed migrations and readiness. Permit outbound HTTPS to GitHub’s API and artifact storage. The public demo uses application sign-in over HTTPS. Operator SSH remains private.
- Create a system account and group named `swimming` for the application service. Install the service files in `/etc/systemd/system` and keep installers, service definitions and releases owned by root.
- Create `/etc/swimming/app.env` as a root-owned file with mode `0600`. Set `DATABASE_URL` with the app’s database credentials, `ADDR=127.0.0.1:18083`, `APP_ORIGIN` to the exact HTTPS origin and `COOKIE_SECURE=true`. PostgreSQL permissions must allow the app to run its migrations. Set SMTP and the teaching email allowlist only as intended for this environment.
- Configure the existing HTTPS proxy to serve `/opt/swimming/current/web`. Forward `/api/v1/*` without rewriting the path to `http://127.0.0.1:18083`. Serve `index.html` for application routes and allow hashed assets to be cached. Apply access and rate limits at the edge. The API’s direct peer address can be the proxy address. If the edge accepts PROXY protocol, accept it only on listeners and from sources explicitly provisioned for it. Direct IPv6 connections should use the ordinary HTTP and TLS listeners as provisioned.
From an approved copy of this repository, install the VM files and enable the API service for boot.

```bash
sudo install -d -m 0755 /opt/swimming/releases
sudo install -d -m 0700 /etc/swimming
sudo install -m 0755 ops/vm/install-release.sh /usr/local/sbin/swimming-install-release
sudo install -m 0755 ops/vm/check-for-release.sh /usr/local/sbin/swimming-check-for-release
sudo install -m 0644 ops/vm/swimming.service ops/vm/swimming-migrate@.service /etc/systemd/system/
sudo install -m 0644 ops/vm/swimming-release-poll.{service,timer} /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable swimming.service
```

The first successful delivery starts the service. The deployment does not seed data. Prepare the database and intended first administrator through the approved environment setup before giving users access. Use demo seeding only for a disposable synthetic database.

## Activate the target

The selected virtual machine uses private NetBird SSH. Complete the operator’s SSO flow before inspecting its current service and reverse-proxy state. Confirm the VM can reach GitHub over outbound HTTPS and retrieve the hosted CI artifact with its dedicated credential before enabling delivery.

The IPv4 gateway connects to both ports 80 and 443 using PROXY protocol. Limit trusted PROXY connections on both listeners to the gateway. Direct IPv6 connections on ports 80 and 443 use ordinary HTTP and TLS. Confirm that the proxy supports these two paths on the same deployment before enabling the repository delivery switch.

Run the following checks from an authorized dual-stack client after applying the target proxy configuration. Both HTTP requests should redirect to the intended HTTPS origin. Both HTTPS paths should present the correct certificate, serve the web app, and return a successful API readiness check. Confirm the edge access logs report the expected client addresses on both paths.

```bash
host=swimmer.bsoltest.com
for family in -4 -6; do
  curl "$family" -sS -o /dev/null -D - "http://$host/"
  curl "$family" -fsS -o /dev/null "https://$host/"
  curl "$family" -fsS "https://$host/api/v1/ready"
done
```

## Enable automatic delivery

1. Provision a dedicated fine-grained GitHub token restricted to the `rosschiu/swimming` repository with Actions and Contents read access. Store it as root in `/etc/swimming/github-token` with mode `0600`. The timer service reads it using systemd credentials. [GitHub workflow and artifact API permissions](https://docs.github.com/en/rest/actions/workflow-runs) · [Artifact downloads](https://docs.github.com/en/rest/actions/artifacts)
2. Take and verify a database backup. Set the repository Actions variable `SWIMMING_VM_DELIVERY` to `enabled` when hosted CI may publish a release artifact from its next successful push to `main`.
3. Start the poll once with `sudo systemctl start swimming-release-poll.service`. Inspect `sudo journalctl -u swimming-release-poll.service` and compare the active release against the tested revision.
4. Run `sudo systemctl enable --now swimming-release-poll.timer` after the manual check. Disable and stop the timer to pause automatic installs. Removing the repository setting prevents new artifacts from being created.

The timer installs only the exact current revision of `main` after a successful push workflow. It verifies GitHub’s artifact digest and rechecks `main` after downloading. If the current revision lacks a successful run or unexpired artifact, the active release keeps running. Inspect systemd logs for fetch and install failures. A manual rollback requires pausing the timer first.

## Verify a release

Run `python3 ops/vm/test_delivery.py` for simulated polling, artifact and rollback regressions. To exercise packaging locally, install the web dependencies and run the same packaging command used by Actions.

```bash
npm ci --prefix apps/web
ops/vm/package.sh "$(git rev-parse HEAD)" /tmp/swimming-release.tar.gz
tar -tzf /tmp/swimming-release.tar.gz
```

The installer checks the revision, applies migrations, switches the active release and polls the local database-backed API readiness endpoint. If the new API fails to start or become ready, the installer restores the previous release, restarts it and checks that it is ready. An unhealthy retry of the active revision preserves its release link and reports the failure. Applied database migrations remain in place. Verify schema compatibility before deploying and keep a restorable backup. After deployment, check the web page and `/api/v1/ready` through the intended HTTPS origin. Release files stay on the VM for troubleshooting and rollback.
