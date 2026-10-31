"""Refresh the bundled, reviewed OpenAI text-token prices from official docs.

Run from the repository root with Python 3 and Windows curl. This updates the
catalog artifact only; it never connects to a user's Vane database.
"""

import datetime
import json
import re
import subprocess
from html.parser import HTMLParser
from pathlib import Path

SOURCE = "https://developers.openai.com/api/docs/pricing"


def fetch(url):
    result = subprocess.run(
        ["curl.exe", "-4", "--http1.1", "-L", "--compressed", "--max-time", "30", "-sS", url],
        capture_output=True, check=True,
    )
    return result.stdout.decode("utf-8")


def decode(value):
    kind = value[0]
    body = value[1] if len(value) > 1 else None
    if kind == 1:
        return [decode(item) for item in body]
    if isinstance(body, dict):
        return {key: decode(item) for key, item in body.items()}
    return body


class PricingProps(HTMLParser):
    def __init__(self):
        super().__init__()
        self.groups = []
        self.component = None

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag != "astro-island" or "props" not in attrs:
            return
        props = json.loads(attrs["props"])
        if "tier" in props and "rows" in props and "/pricing." in attrs.get("component-url", ""):
            self.groups.append({key: decode(value) for key, value in props.items()})
            self.component = attrs["component-url"]


def long_prices(script):
    # Read the static pricing data object, never execute downloaded JavaScript.
    match = re.search(r"var \w+=(\{ultrafast:)", script)
    if not match:
        raise ValueError("Official long-context pricing format changed")
    start = match.start(1)
    depth, quote, escaped = 0, None, False
    for end in range(start, len(script)):
        char = script[end]
        if quote:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == quote:
                quote = None
        elif char in '\"\'`':
            quote = char
        elif char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                raw = script[start:end + 1].replace("`-`", '"-"')
                raw = re.sub(r"(?<=[{,])([A-Za-z]\w*):", r'"\1":', raw)
                raw = re.sub(r"(?<=:)\.(\d)", r"0.\1", raw)
                return json.loads(raw)
    raise ValueError("Unterminated official pricing data")


def number(value):
    return float(value) if isinstance(value, (int, float)) else 0


def main():
    parser = PricingProps()
    parser.feed(fetch(SOURCE))
    if not parser.component or len(parser.groups) < 4:
        raise ValueError("Official text pricing tables were not found")
    long_rows = long_prices(fetch("https://developers.openai.com" + parser.component))
    records = {}
    for group in parser.groups:
        variants = [(group["tier"], group["rows"])]
        if group["tier"] == "batch" and "alternateRows" in group:
            variants.append(("flex", group["alternateRows"]))
        for tier, rows in variants:
            for row in rows:
                name = re.sub(r"\s*\(.*?\)\s*$", "", row[0]).strip()
                if len(row) not in (4, 5) or not isinstance(row[1], (int, float)) or not isinstance(row[-1], (int, float)):
                    continue
                write = number(row[3]) if len(row) == 5 else 0
                context = group.get("longContextRows", long_rows.get("batch" if tier == "flex" else tier, {})).get(name, {})
                record = dict(model_name=name, provider="codex", service_tier="" if tier == "standard" else tier,
                              input_price=number(row[1]), output_price=number(row[-1]), cache_read_price=number(row[2]),
                              cache_write_price_30m=write, cache_write_price_12h=write * 2,
                              long_context_threshold=272000 if number(context.get("input")) and number(context.get("output")) else 0,
                              long_context_input_price=number(context.get("input")), long_context_output_price=number(context.get("output")),
                              long_context_cache_read_price=number(context.get("cachedInput")),
                              long_context_cache_write_price=number(context.get("cacheWrite")),
                              long_context_cache_write_price_12h=number(context.get("cacheWrite")) * 2)
                records[(name, record["service_tier"])] = record
    if records.get(("gpt-6.1-sol", "ultrafast"), {}).get("input_price") != 12:
        raise ValueError("Review changed flagship pricing before updating this catalog")
    result = dict(source=SOURCE, cache_source="https://developers.openai.com/api/docs/guides/prompt-caching",
                  as_of=datetime.date.today().isoformat(), records=list(records.values()))
    output = Path(__file__).resolve().parents[2] / "internal/pricing/service/openai-pricing.json"
    output.write_text(json.dumps(result, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"Saved {len(records)} official pricing rows to {output}")


if __name__ == "__main__":
    main()
