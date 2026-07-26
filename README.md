# rkn-penetration

Monorepo for building custom `geoip.dat` and `geosite.dat` from upstream ready-made `.dat` files.

Each output file can contain one or more independently configured tags/categories, for example:

- `geoip.dat`: `geoip:proxy`, `geoip:direct`
- `geosite.dat`: `geosite:proxy`, `geosite:direct`

Internally the generated `.dat` stores codes in uppercase, because Xray normalizes lookups such as `geosite:proxy` and `geoip:proxy` to uppercase.

Every output tag merges only its own selected upstream categories and custom rules. Geosite sanitization is also applied independently to each output tag.

## Configuration

Upstream URLs live in [config/sources.yml](config/sources.yml):

```yaml
geoip_dat_url: "https://raw.githubusercontent.com/runetfreedom/russia-blocked-geoip/release/geoip.dat"
geosite_dat_url: "https://raw.githubusercontent.com/runetfreedom/russia-blocked-geosite/release/geosite.dat"
```

Change these URLs without editing scripts.

## Add GeoIP Categories

Edit [config/geoip.yml](config/geoip.yml):

```yaml
outputs:
  - output_tag: proxy
    include:
      - ru-blocked
      - ru-blocked-community
      - re-filter
      - telegram
      - twitter
      - facebook
      - cloudflare
      - cloudfront
      - netflix
    custom:
      - custom/geoip/proxy.txt

  - output_tag: direct
    include:
      - private
    custom:
      - custom/geoip/direct.txt
```

Each `include` list is extracted from upstream `geoip.dat` and merged only into its surrounding `output_tag`. Each `custom` list is likewise local to that tag. The build fails if an output tag is duplicated, is empty, has no resulting CIDRs, or references an upstream category that does not exist.

## Add Geosite Categories

Edit [config/geosite.yml](config/geosite.yml):

```yaml
outputs:
  - output_tag: proxy
    include:
      - ru-blocked
      - category-dev
      - ubiquiti
    custom:
      - custom/geosite/proxy
    sanitize:
      - porn
      - kazino
      - ".ru"

  - output_tag: direct
    include:
      - category-ads-all
    custom:
      - custom/geosite/direct
    sanitize:
      - tracker
```

Every output object is isolated: its `include`, `custom`, and `sanitize` values do not affect neighboring output tags.

`sanitize` removes geosite rules after upstream and custom rules are merged. Plain values are case-insensitive keywords matched against the rule value. Values that start with a dot, such as `.ru`, are treated as domain suffix filters. Keep keywords conservative to avoid false positives.

The old single-output top-level format (`output_tag`, `include`, `custom`, `sanitize`) remains supported for backward compatibility. Do not mix it with `outputs` in the same file.

## Add Custom CIDR

Edit [custom/geoip/proxy.txt](custom/geoip/proxy.txt). Use one IPv4 or IPv6 CIDR per line:

```text
203.0.113.0/24
198.51.100.10/32
2001:db8::/32
```

Empty lines and `#` comments are allowed. Invalid CIDR stops the build.

## Add Custom Domains

Edit [custom/geosite/proxy](custom/geosite/proxy). Supported rule formats:

```text
domain:example.com
full:api.example.com
keyword:example-cdn
regexp:^.*\.example\.net$
example.org
```

Empty lines and `#` comments are allowed. Rules are deduplicated conservatively without aggressive normalization.

## Local Build

Requirements:

- Bash
- Go
- curl
- `sha256sum` or `shasum`

Commands:

```sh
make geoip
make geosite
make validate
make clean
```

`make all` creates:

- `dist/geoip.dat`
- `dist/geosite.dat`
- `dist/geoip.dat.sha256`
- `dist/geosite.dat.sha256`

Generated `.dat` files are ignored in the main branch and published by GitHub Actions to GitHub Releases and to the `release` branch.

## Xray Usage

Download `geoip.dat` and `geosite.dat` from the latest GitHub Release or from the stable raw links below, then place them where Xray reads routing data files.

Example routing config:

```json
{
  "routing": {
    "domainStrategy": "IPIfNonMatch",
    "rules": [
      {
        "type": "field",
        "domain": [
          "geosite:proxy"
        ],
        "outboundTag": "proxy"
      },
      {
        "type": "field",
        "ip": [
          "geoip:proxy"
        ],
        "outboundTag": "proxy"
      }
    ]
  }
}
```

## Release URLs

After the workflow runs, the latest files are always available from the `release` branch:

```text
https://raw.githubusercontent.com/vespersio/rkn-penetration/release/geosite.dat
https://raw.githubusercontent.com/vespersio/rkn-penetration/release/geosite.dat.sha256

https://raw.githubusercontent.com/vespersio/rkn-penetration/release/geoip.dat
https://raw.githubusercontent.com/vespersio/rkn-penetration/release/geoip.dat.sha256
```

The same files are also attached to the latest GitHub Release:

```text
https://github.com/vespersio/rkn-penetration/releases/latest/download/geoip.dat
https://github.com/vespersio/rkn-penetration/releases/latest/download/geoip.dat.sha256
https://github.com/vespersio/rkn-penetration/releases/latest/download/geosite.dat
https://github.com/vespersio/rkn-penetration/releases/latest/download/geosite.dat.sha256
```

## Pipeline

The build follows `extract -> merge text/protobuf entries -> build new dat`:

1. Download upstream `.dat` from `config/sources.yml`.
2. Extract configured categories from the upstream protobuf data.
3. Add custom CIDR or domain rules.
4. Deduplicate entries.
5. Build a new `.dat` containing exactly the configured output tags.
6. Generate SHA256 checksums.
7. Validate that the output contains every configured tag, no extra tags, and non-empty data in each tag.
