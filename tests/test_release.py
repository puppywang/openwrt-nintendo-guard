import hashlib
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
from release import expected_assets, parse_manifest, validate_artifacts, version


class ReleaseArtifacts(unittest.TestCase):
    def test_complete_and_corrupted_artifacts(self):
        tag = "v1.2.3"
        with tempfile.TemporaryDirectory(prefix="nintendo-release-test-") as tmp:
            directory = Path(tmp)
            lines = []
            for name in sorted(expected_assets(tag)):
                data = (name + "\n").encode()
                (directory / name).write_bytes(data)
                lines.append(hashlib.sha256(data).hexdigest() + "  " + name)
            (directory / "SHA256SUMS").write_text("\n".join(lines) + "\n", encoding="ascii")
            self.assertEqual(len(validate_artifacts(directory, tag)), 20)
            package = directory / "luci-app-nintendo-guard-1.2.3-r1.noarch.apk"
            package.write_bytes(b"corrupted")
            with self.assertRaisesRegex(ValueError, "integrity"):
                validate_artifacts(directory, tag)
            package.unlink()
            with self.assertRaisesRegex(ValueError, "missing"):
                validate_artifacts(directory, tag)

    def test_incomplete_or_unsafe_manifest(self):
        for text in ["0" * 64 + "  ../secret\n", "0" * 64 + "  only-one.apk\n"]:
            with self.assertRaises(ValueError):
                parse_manifest(text.encode(), "v1.2.3")

    def test_invalid_version(self):
        for value in ["dev", "v1.2", "v1.2.3;echo secret", "v1.2.3/../x"]:
            with self.assertRaises(ValueError):
                version(value)


if __name__ == "__main__":
    unittest.main()
