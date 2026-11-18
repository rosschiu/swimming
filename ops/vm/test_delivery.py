"""Run delivery regressions without contacting GitHub, SSH or systemd."""

import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import textwrap
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

    def workflow_release(self, sha, status):
        workflow = FILES.parents[1] / ".github/workflows/ci.yml"
        step = workflow.read_text().split("- name: Install tested release\n", 1)[1]
        script = self.work / "workflow-release"
        script.write_text(textwrap.dedent(step.split("        run: |\n", 1)[1]))
        self.mock("gh", 'echo "$MAIN_SHA"; exit "$GH_STATUS"')
        self.mock("ssh", 'printf "%s\\n" "$@" > "$TRACE"')
        (self.work / "swimming-release.tar.gz").touch()
        return self.run_script(
            script, MAIN_SHA=sha, GH_STATUS=str(status),
            GITHUB_REPOSITORY="test/swimming", GITHUB_SHA=NEW,
            VM_HOST="vm.test", VM_USER="deploy", VM_KEY="dummy",
            VM_KNOWN_HOSTS="dummy host key",
        )

    def test_invalid_main_lookup_fails_without_connecting(self):
        for sha, status in [(NEW, 1), ("null", 0), ("", 0)]:
            with self.subTest(sha=sha, status=status):
                result = self.workflow_release(sha, status)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(self.trace.exists())

    def test_stale_revision_skips_and_current_revision_pins_host_identity(self):
        self.assertEqual(self.workflow_release(OLD, 0).returncode, 0)
        self.assertFalse(self.trace.exists())
        self.assertEqual(self.workflow_release(NEW, 0).returncode, 0)
        self.assertIn("StrictHostKeyChecking=yes", self.trace.read_text())

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
