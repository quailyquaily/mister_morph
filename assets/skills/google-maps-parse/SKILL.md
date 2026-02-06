---
name: "google-maps-parse"
description: "Parse Google Maps URLs to extract coordinates(lat/lng) and POI"
metadata:
requirements:
  - bash
  - curl
  - python3
---

# Google Maps Parse

## What this skill provides

- A script that converts a Google Maps URL (short or long) into structured JSON: lat/lng/zoom + best-effort place/query fields.

This is a **skill** (prompt context), not an automatic built-in tool. To execute the script, the agent must use the `bash` tool.

## How to use

- IMPORTANT: the `bash` tool runs in the agent's current working directory. Do NOT use a relative path like `scripts/...` unless you `cd` first.

- Preferred (absolute path):
  - `~/.morph/skills/google-maps-parse/scripts/google_maps_parse.sh "<url>"`
- Alternative (cd first):
  - `cd ~/.morph/skills/google-maps-parse && ./scripts/google_maps_parse.sh "<url>"`
- If network access is unavailable (cannot follow redirects), run:
  - `~/.morph/skills/google-maps-parse/scripts/google_maps_parse.sh --no-resolve "<expanded_url>"`

## Output

The script prints a single JSON object to stdout.

## Follow-up

If the user ask how to go from A to here, the agent can extract the coordinates, and then ask the user for another location, and then use them to generate **a google maps link** for the directions.
