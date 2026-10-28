#!/usr/bin/env python3
"""Package the registered cloud App or an explicit portable MCP export."""
import argparse
import json
from pathlib import Path
from urllib.parse import urlsplit
import zipfile


def main():
    root = Path(__file__).resolve().parent.parent
    source = root / "plugin" / "klap"
    manifest = json.loads((source / "plugin.json").read_text())
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--portable", action="store_true", help="Export direct MCP configuration instead of the registered cloud App")
    parser.add_argument("--origin", help="Verified public HTTPS origin of your KLAP instance")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    portable = args.portable or bool(args.origin)
    suffix = "-portable" if portable else ""
    output = (args.output or root / "dist" / f"klap-plugin-{manifest['version']}{suffix}.zip").resolve()
    if output.is_relative_to(source):
        parser.error("Archive must be outside plugin/klap")
    compat = json.loads((source / ".codex-plugin/plugin.json").read_text())
    bodies = {}
    if portable:
        mcp = json.loads((root / "plugin/mcp.template.json").read_text())
        manifest["extensions"]["com.openai"].pop("apps", None)
        compat.pop("apps", None)
        compat["mcpServers"] = "./.mcp.json"
        bodies["mcp.json"] = mcp
        bodies[".mcp.json"] = {"mcpServers": mcp["mcpServers"]}
    else:
        binding = json.loads((source / ".app.json").read_text())
        assert manifest["extensions"]["com.openai"]["apps"] == compat["apps"] == "./.app.json"
        assert binding["apps"] and all(app["id"] for app in binding["apps"].values())
        assert "mcpServers" not in compat
        bodies[".app.json"] = binding
    if args.origin:
        try:
            value = urlsplit(args.origin)
            value.port
        except ValueError:
            parser.error("--origin must have a valid host and port")
        if (value.scheme != "https" or not value.hostname or value.username or value.password
                or value.path not in ("", "/") or value.query or value.fragment
                or any(c.isspace() for c in args.origin)):
            parser.error("--origin must be an HTTPS origin without credentials, path, query, or fragment")
        mcp["mcpServers"]["klap"]["url"] = args.origin.rstrip("/") + "/mcp"
    assert manifest["$schema"] == "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
    if portable:
        assert mcp["$schema"] == "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"
    assert compat["version"] == manifest["version"]
    interface = manifest["extensions"]["com.openai"]["interface"]
    assert len(interface["shortDescription"]) <= 30
    bodies.update({"plugin.json": manifest, ".codex-plugin/plugin.json": compat})
    paths = {"skills/klap/SKILL.md", interface["logo"], interface["composerIcon"]}
    for relative in paths:
        path = source / relative
        assert path.is_file() and not path.is_symlink() and path.resolve().is_relative_to(source)
    output.parent.mkdir(parents=True, exist_ok=True)
    files = {relative: (source / relative).read_bytes() for relative in paths}
    files.update({relative: (json.dumps(body, ensure_ascii=False, indent=2) + "\n").encode() for relative, body in bodies.items()})
    with zipfile.ZipFile(output, "w", zipfile.ZIP_DEFLATED) as archive:
        for relative, body in sorted(files.items()):
            archive.writestr("klap/" + relative, body)
    with zipfile.ZipFile(output) as archive:
        assert archive.testzip() is None
        assert set(archive.namelist()) == {"klap/" + name for name in files}
    print(output)


if __name__ == "__main__":
    main()
