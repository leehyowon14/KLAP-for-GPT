#!/usr/bin/env python3
"""Package only the reviewed plugin files, optionally for another instance."""
import argparse
import json
from pathlib import Path
from urllib.parse import urlsplit
import zipfile


def main():
    root = Path(__file__).resolve().parent.parent
    source = root / "plugin" / "klap"
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--origin", help="Verified public HTTPS origin of your KLAP instance")
    parser.add_argument("--output", type=Path, default=root / "dist" / "klap-plugin-0.1.0.zip")
    args = parser.parse_args()
    output = args.output.resolve()
    if output.is_relative_to(source):
        parser.error("Archive must be outside plugin/klap")
    manifest = json.loads((source / "plugin.json").read_text())
    mcp = json.loads((source / "mcp.json").read_text())
    if args.origin:
        value = urlsplit(args.origin)
        if (value.scheme != "https" or not value.hostname or value.username or value.password
                or value.path not in ("", "/") or value.query or value.fragment
                or any(c.isspace() for c in args.origin)):
            parser.error("--origin must be an HTTPS origin without credentials, path, query, or fragment")
        mcp["mcpServers"]["klap"]["url"] = args.origin.rstrip("/") + "/mcp"
    assert manifest["$schema"] == "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
    assert mcp["$schema"] == "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"
    interface = manifest["extensions"]["com.openai"]["interface"]
    assert len(interface["shortDescription"]) <= 30
    paths = {"plugin.json", "mcp.json", "skills/klap/SKILL.md", interface["logo"], interface["composerIcon"]}
    for relative in paths:
        path = source / relative
        assert path.is_file() and not path.is_symlink() and path.resolve().is_relative_to(source)
    output.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(output, "w", zipfile.ZIP_DEFLATED) as archive:
        for relative in sorted(paths):
            body = (json.dumps(mcp, ensure_ascii=False, indent=2) + "\n").encode() if relative == "mcp.json" else (source / relative).read_bytes()
            archive.writestr("klap/" + relative, body)
    with zipfile.ZipFile(output) as archive:
        assert archive.testzip() is None
        assert set(archive.namelist()) == {"klap/" + name for name in paths}
    print(output)


if __name__ == "__main__":
    main()
