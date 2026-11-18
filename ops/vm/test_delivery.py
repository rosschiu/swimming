"""Run delivery regressions without contacting GitHub or systemd."""

import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import zipfile
import unittest

FILES = Path(__file__).resolve().parent
NEW = "a" * 40
OLD = "b" * 40


class DeliveryTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.work = Path(temporary.name)
        self.bin = self.work / "bin"
        self.bin.mkdir()
        (self.work / "releases").mkdir()
        self.trace = self.work / "trace"
        self.current = self.work / "current"
        self.env = {
            **os.environ,
            "PATH": f"{self.bin}:{os.environ['PATH']}",
            "TRACE": str(self.trace),
            "CURRENT": str(self.current),
        }

    def mock(self, name, code):
        command = self.bin / name
        command.write_text("#!/bin/bash\n" + code + "\n")
        command.chmod(0o755)

    def run_script(self, script, *arguments, archive=None, **environment):
        return subprocess.run(
            ["bash", script, *arguments], input=archive, cwd=self.work,
            env={**self.env, **environment}, capture_output=True, timeout=10,
        )

    def poll(self, *, main=NEW, final=NEW, conclusion="success", valid_digest=True):
        source = (FILES / "check-for-release.sh").read_text()
        script = self.work / "poll"
        script.write_text(source.replace("root=/opt/swimming\n", f"root={self.work}\n", 1)
                          .replace("/usr/local/sbin/swimming-install-release", "mock-install"))
        credential = self.work / "github-token"
        credential.write_text("github_pat_Test")
        archive = self.work / "artifact.zip"
        with zipfile.ZipFile(archive, "w") as bundle:
            bundle.writestr("swimming-release.tar.gz", b"tested release")
        digest = hashlib.sha256(archive.read_bytes()).hexdigest() if valid_digest else "0" * 64
        files = {
            "MAIN": {"object": {"sha": main}},
            "FINAL": {"object": {"sha": final}},
            "RUN": {"workflow_runs": [{"id": 123, "head_sha": NEW, "head_branch": "main",
                    "event": "push", "head_repository": {"full_name": "rosschiu/swimming"},
                    "status": "completed", "conclusion": conclusion}]},
            "ARTIFACTS": {"artifacts": [{"name": "swimming-vm-release", "id": 456,
                          "expired": False, "digest": "sha256:" + digest}]},
        }
        for key, content in files.items():
            path = self.work / key
            path.write_text(json.dumps(content))
            self.env[key] = str(path)
        self.mock("curl", r"""
url=${!#}
case "$url" in
  */git/ref/heads/main)
    if [[ -e "$MAIN.read" ]]; then cat "$FINAL"; else touch "$MAIN.read"; cat "$MAIN"; fi ;;
  */actions/workflows/ci.yml/runs\?branch=main\&event=push\&head_sha=*\&per_page=1) cat "$RUN" ;;
  */actions/runs/123/artifacts\?name=swimming-vm-release\&per_page=100) cat "$ARTIFACTS" ;;
  */actions/artifacts/456/zip) cat "$ARCHIVE" ;;
  *) exit 1 ;;
esac
""")
        self.mock("mock-install", 'echo "$1" > "$TRACE"; cat >> "$TRACE"')
        return self.run_script(script, CREDENTIALS_DIRECTORY=str(self.work), ARCHIVE=str(archive))

    def test_invalid_main_lookup_fails(self):
        self.assertNotEqual(self.poll(main="invalid").returncode, 0)
        self.assertFalse(self.trace.exists())

    def test_untested_revision_waits(self):
        self.assertEqual(self.poll(conclusion="failure").returncode, 0)
        self.assertFalse(self.trace.exists())

    def test_main_change_during_download_skips_release(self):
        result = self.poll(final=OLD)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.trace.exists())

    def test_digest_mismatch_rejects_artifact(self):
        self.assertNotEqual(self.poll(valid_digest=False).returncode, 0)
        self.assertFalse(self.trace.exists())

    def test_current_tested_release_installs(self):
        result = self.poll()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.trace.read_text(), f"{NEW}\ntested release")

    def install(self, active, healthy):
        if active:
            release = self.work / "releases" / active
            release.mkdir()
            self.current.symlink_to(release)
        script = self.work / "install-release"
        source = (FILES / "install-release.sh").read_text()
        script.write_text(source.replace("root=/opt/swimming\n", f"root={self.work}\n", 1))
        self.mock("chown", "true")
        self.mock("runuser", 'shift 3; exec "$@"')
        self.mock("sleep", "true")
        self.mock("systemctl", 'echo "systemctl $*" >> "$TRACE"')
        self.mock("curl", '''
release=$(basename "$(readlink "$CURRENT")")
echo "ready $release" >> "$TRACE"
[[ "$release" == "$HEALTHY" ]]
''')
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode="w:gz") as bundle:
            for name, data, mode in [
                ("REVISION", NEW.encode() + b"\n", 0o644),
                ("swimming-api", b"test", 0o755),
                ("swimming-migrate", b"test", 0o755),
                ("web/index.html", b"test", 0o644),
            ]:
                member = tarfile.TarInfo(name)
                member.size, member.mode = len(data), mode
                bundle.addfile(member, io.BytesIO(data))
        return self.run_script(script, NEW, archive=archive.getvalue(), HEALTHY=healthy)

    def test_unhealthy_retry_preserves_active_link(self):
        result = self.install(NEW, OLD)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.current.readlink().name, NEW)
        self.assertNotIn("systemctl stop", self.trace.read_text())

    def test_rollback_verifies_previous_release_readiness(self):
        result = self.install(OLD, OLD)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.current.readlink().name, OLD)
        self.assertIn(f"ready {OLD}", self.trace.read_text())
        self.assertIn(b"Previous release restored and ready", result.stderr)

    def test_failed_rollback_reports_unhealthy_previous_release(self):
        result = self.install(OLD, "")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.current.readlink().name, OLD)
        self.assertIn(b"Previous release restored but failed readiness", result.stderr)

    def test_failed_first_release_clears_active_link(self):
        result = self.install(None, "")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.current.is_symlink())
        self.assertIn("systemctl stop", self.trace.read_text())


if __name__ == "__main__":
    unittest.main()
