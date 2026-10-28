import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import zipfile


ROOT = Path(__file__).resolve().parent.parent
SCRIPT = ROOT / "scripts/package-plugin.py"


class PackagePluginTest(unittest.TestCase):
    def package(self, *args):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "plugin.zip"
            subprocess.run([sys.executable, str(SCRIPT), "--output", str(output), *args], check=True, capture_output=True)
            with zipfile.ZipFile(output) as archive:
                return {name.removeprefix("klap/"): archive.read(name) for name in archive.namelist()}

    def test_cloud_package_preserves_brand_skill_and_registered_app(self):
        files = self.package()
        self.assertEqual(set(files), {"plugin.json", ".codex-plugin/plugin.json", ".app.json", "assets/icon.svg", "skills/klap/SKILL.md"})
        manifest = json.loads(files["plugin.json"])
        compat = json.loads(files[".codex-plugin/plugin.json"])
        self.assertEqual(manifest["extensions"]["com.openai"]["apps"], "./.app.json")
        self.assertEqual(compat["apps"], "./.app.json")
        self.assertNotIn("mcpServers", compat)
        self.assertEqual(manifest["version"], compat["version"])
        self.assertEqual(json.loads(files[".app.json"])["apps"]["dev-6ae1c28743408191b4f0fe2d651fd0b0"]["id"], "asdk_app_6ae1c28743408191b4f0fe2d651fd0b0")
        for path in ["assets/icon.svg", "skills/klap/SKILL.md"]:
            self.assertEqual(files[path], (ROOT / "plugin/klap" / path).read_bytes())

    def test_portable_package_has_no_account_app_binding(self):
        files = self.package("--portable")
        self.assertNotIn(".app.json", files)
        self.assertNotIn("apps", json.loads(files["plugin.json"])["extensions"]["com.openai"])
        self.assertNotIn("apps", json.loads(files[".codex-plugin/plugin.json"]))
        self.assertEqual(json.loads(files[".codex-plugin/plugin.json"])["mcpServers"], "./.mcp.json")
        self.assertEqual(json.loads(files["mcp.json"])["mcpServers"], json.loads(files[".mcp.json"])["mcpServers"])

    def test_origin_selects_portable_and_changes_both_mcp_configs(self):
        files = self.package("--origin", "https://other.example:8443/")
        self.assertNotIn(".app.json", files)
        for path in ["mcp.json", ".mcp.json"]:
            config = json.loads(files[path])["mcpServers"]["klap"]
            self.assertEqual(config["url"], "https://other.example:8443/mcp")
            self.assertEqual(config["extensions"]["com.openai"]["auth"]["baseScopes"], ["klap"])

    def test_invalid_origins_do_not_create_archives(self):
        for origin in ["http://other.example", "https://u:p@other.example", "https://other.example/path", "https://other.example?query", "https://other.example#fragment", "https://other.example\n", "https://other.example:invalid", "https://[bad"]:
            with self.subTest(origin=origin), tempfile.TemporaryDirectory() as directory:
                output = Path(directory) / "plugin.zip"
                result = subprocess.run([sys.executable, str(SCRIPT), "--output", str(output), "--origin", origin], capture_output=True)
                self.assertEqual(result.returncode, 2)
                self.assertFalse(output.exists())

    def test_archive_cannot_be_written_inside_plugin_source(self):
        output = ROOT / "plugin/klap/forbidden.zip"
        result = subprocess.run([sys.executable, str(SCRIPT), "--output", str(output)], capture_output=True)
        self.assertEqual(result.returncode, 2)
        self.assertFalse(output.exists())


if __name__ == "__main__":
    unittest.main()
