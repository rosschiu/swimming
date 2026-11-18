# Private VM delivery

The optional GitHub Actions deployment packages a tested push to `main` for a Linux x86-64 VM. Pull requests run the application checks without packaging or deployment. Delivery compares the tested revision to the current `main` before connecting, then uses SSH with a pinned host key. The workflow ships the API, migration binary and built web files. It uses the existing PostgreSQL database and HTTPS proxy.

## Prepare the VM

- Provision a VM with systemd, Bash, GNU tar, curl, flock, sudo, SSH and a PostgreSQL 17 database. Provision exactly one dedicated Linux x86-64 runner in the `swimming-private-vm` group and give it the `swimming-private-vm` label. The single runner serializes revision lookup and SSH delivery. Restrict the group to this repository and allow only `rosschiu/swimming/.github/workflows/ci.yml@refs/heads/main` under Selected workflows. Keep delivery disabled until the GitHub organization or enterprise supports and enforces those restrictions. The runner needs an approved private route to the VM SSH port, plus Bash, curl, OpenSSH and the GitHub CLI. Pull-request CI stays on GitHub-hosted runners. [Runner-group access controls](https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/manage-access) Keep application access limited to the intended users through the existing private network or authenticated HTTPS edge.
- Create a system account and group named `swimming` for the service and a separate SSH-only deployment account. Install the service files in `/etc/systemd/system` and install the release script as a root-owned executable. Keep the deploy account from writing to the installer, service definitions or release paths.
- Create `/etc/swimming/app.env` as a root-owned file with mode `0600`. Set `DATABASE_URL` with the app’s database credentials, `ADDR=127.0.0.1:18083`, `APP_ORIGIN` to the exact private HTTPS origin and `COOKIE_SECURE=true`. PostgreSQL permissions must allow the app to run its migrations. Set SMTP and the teaching email allowlist only as intended for this environment.
- Configure the existing HTTPS proxy to serve `/opt/swimming/current/web`. Forward `/api/v1/*` without rewriting the path to `http://127.0.0.1:18083`. Serve `index.html` for application routes and allow hashed assets to be cached. Apply access and rate limits at the edge. The API’s direct peer address can be the proxy address. If the edge accepts PROXY protocol, accept it only on listeners and from sources explicitly provisioned for it. Direct IPv6 connections should use the ordinary HTTP and TLS listeners as provisioned.
- Install the deployment public key for the SSH-only account with forwarding and PTY access disabled. For an account called `swimming-deploy`, use this narrowly scoped entry in the sudoers configuration and validate it with `visudo`.

```sudoers
swimming-deploy ALL=(root) NOPASSWD: /usr/local/sbin/swimming-install-release *
```

From an approved copy of this repository, install the VM files and enable the API service for boot.

```bash
sudo install -d -m 0755 /opt/swimming/releases
sudo install -d -m 0700 /etc/swimming
sudo install -m 0755 ops/vm/install-release.sh /usr/local/sbin/swimming-install-release
sudo install -m 0644 ops/vm/swimming.service ops/vm/swimming-migrate@.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable swimming.service
```

The first successful delivery starts the service. The deployment does not seed data. Prepare the database and intended first administrator through the approved environment setup before giving users access. Use demo seeding only for a disposable synthetic database.

## Activate the target

The selected virtual machine uses private NetBird SSH. Complete the operator’s SSO flow before inspecting its current service and reverse-proxy state. Confirm the dedicated Actions runner can reach SSH and retrieve its workflow artifacts before enabling delivery.

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

## Enable GitHub Actions delivery

1. Create a GitHub environment named `private-vm`. Restrict it to the `main` branch and configure the protection rules available for the repository. [GitHub environment setup](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/manage-environments)
2. Set environment variables `VM_HOST` to the permitted SSH hostname on port 22 and `VM_USER` to the deployment account.
3. Set environment secrets `VM_SSH_KEY` to the deployment private key and `VM_KNOWN_HOSTS` to the VM's OpenSSH host-key line. Verify the host key through the VM console or another trusted channel before copying it. [OpenSSH host-key settings](https://man.openbsd.org/ssh_config)
4. Take and verify a database backup as appropriate for the target. Set the repository Actions variable `SWIMMING_VM_DELIVERY` to `enabled` when delivery may begin. Remove it to disable future deliveries.

## Verify a release

To exercise packaging locally, install the web dependencies and run the same packaging command used by Actions.

```bash
npm ci --prefix apps/web
ops/vm/package.sh "$(git rev-parse HEAD)" /tmp/swimming-release.tar.gz
tar -tzf /tmp/swimming-release.tar.gz
```

The installer checks the revision, applies migrations, switches the active release and polls the local database-backed API readiness endpoint. If the new API fails to start or become ready, the installer restores the previous release, restarts it and checks that it is ready. An unhealthy retry of the active revision preserves its release link and reports the failure. Applied database migrations remain in place. Verify schema compatibility before deploying and keep a restorable backup. After deployment, check the web page and `/api/v1/ready` through the intended HTTPS origin. Release files stay on the VM for troubleshooting and rollback.
