# Syslog and HTTP access examples

Streamline recognizes these formats in **Auto** mode for stdin and command
sources. **Text** keeps the original sanitized lines.

## Try the sample logs

From the repository root, after `make build`:

```sh
cat examples/logs/syslog-rfc5424.log | ./bin/streamline
cat examples/logs/syslog-rfc3164.log | ./bin/streamline
cat examples/logs/syslog-text.log | ./bin/streamline
cat examples/logs/http-access.log | ./bin/streamline
```

Run one command at a time, then open the viewer. Alternatively, enter one of
the `cat examples/logs/…` commands in a command tab.

## Saved configurations

Each file in [configurations](configurations) is a complete version-1 saved
configuration. Copy the desired files into the `configs/` subdirectory of
Streamline's configuration directory and select **Refresh** in Saved
configurations. Defaults are `~/.config/streamline/configs/` in production and
`.local/streamline/configs/` in development.

| Configuration | Columns and filter | Example command source |
| --- | --- | --- |
| [RFC 5424](configurations/syslog-rfc5424.json) | Host, app, process, facility, severity; `app = sshd` | `tail -n 200 -F /var/log/remote.log` |
| [RFC 3164](configurations/syslog-rfc3164.json) | Same fields, with inferred year/timezone; `app = sshd` | `tail -n 200 -F /var/log/remote.log` |
| [Local syslog](configurations/syslog-text.json) | Host, app, process, message; `app = sshd` | `tail -n 200 -F /var/log/auth.log` |
| [HTTP access](configurations/http-access.json) | Client, method, target, status, bytes, referrer, agent; `500 <= status < 600` | `tail -n 200 -F /var/log/nginx/access.log` |

Edit the command for your actual log location before clicking **Run**.
The two remote-log examples assume a collector has already written that format;
Streamline does not listen for syslog network traffic. A local syslog file can
also be `/var/log/syslog` or `/var/log/messages`, depending on the host.
For remote access, use an already configured SSH connection, for example:

```sh
ssh -o BatchMode=yes server 'tail -n 200 -F /var/log/auth.log'
ssh -o BatchMode=yes server 'tail -n 200 -F /var/log/nginx/access.log'
```

For the sample logs, replace the saved command with the matching `cat` command.
Every saved filter matches exactly one record in its sample.

## Extracted fields

### Syslog

| Field | Type and meaning |
| --- | --- |
| `time` | Original timestamp string; null for RFC 5424 NILVALUE |
| `hostname`, `app`, `procid` | Host, application, and process identifier; strings or null |
| `priority`, `facility`, `severity_code` | Numbers, only when a PRI prefix exists |
| `version`, `msgid` | RFC 5424 version number and message ID |
| `structured_data` | RFC 5424 object keyed by SD-ID; null for NILVALUE |

`procid` stays a string because RFC 5424 permits nonnumeric identifiers.
Repeated structured-data parameters become arrays, preserving every value.
For example, `structured_data.origin.ip` is filterable when it is a scalar.
The existing field-path rules do not support indexing arrays or looking up
literal dotted keys; such values remain visible in previews and general search.

The canonical `message` contains the payload, excluding the syslog header.
The canonical `severity` uses PRI modulo 8 and remains empty without PRI.
A syslog message mentioning “error” does not manufacture severity.

Traditional timestamps omit a year and timezone. Streamline defaults to UTC
and chooses the closest valid year to source startup from that year and its
neighbors, adding `timestamp_context_assumed`. This can misdate old archives or
logs written in a different local timezone. Prefer timestamped RFC 5424 or
`journalctl -o json --no-pager` when that context matters. Parser timezone and
reference-time options are internal; there is no new UI setting.

### HTTP access

| Field | Type and meaning |
| --- | --- |
| `client`, `ident`, `user` | Remote address/hostname, ident, authenticated user; strings or null |
| `time`, `request` | Original timestamp and decoded request text |
| `method`, `target`, `protocol` | Present only for a valid three-part HTTP request |
| `status`, `bytes` | Numeric response status (100–599) and nonnegative byte count; null for `-` |
| `referer`, `user_agent` | Combined format only; strings or null |

The canonical `message` is the decoded request. HTTP severity is empty.
A missing request stays `"-"`; a malformed client request such as `"bad request"`
is preserved and does not invalidate an otherwise valid access record.

The parser handles Apache quote/backslash/C-style escapes and Nginx/Apache
`\xHH` byte escapes, then sanitizes decoded values. It does not URL-decode
request targets. Missing byte counts are null, not zero.

Only the standard Common and Combined layouts are supported. Added virtual-host
prefixes, extra duration columns, custom field orders, and nonstandard status
codes require another format. Common/Combined do not supply request duration.

## Failure behavior and compatibility

Malformed recognized candidates retain their entire sanitized line as text,
with `malformed_syslog_fallback` or `malformed_http_access_fallback`.
They do not establish a parsed stream. In a mixed parsed stream their diagnostics
appear on the row; if every line is unrecognized, EOF displays raw text.

Structurally valid records with invalid dates retain their fields and get
`invalid_timestamp`, leaving the canonical timestamp empty. Exact source bytes
and physical line boundaries are preserved by the parser. JSON/logfmt message
values and syslog payloads are not recursively decoded.

These additions require no new source modes, API version, or saved-configuration
migration. Container wrappers, DNS/firewall payloads, and multiline reassembly
remain later work.

## Format references

- [RFC 5424](https://www.rfc-editor.org/rfc/rfc5424.html)
- [RFC 3164](https://www.rfc-editor.org/rfc/rfc3164.html)
- [Apache access formats](https://httpd.apache.org/docs/2.4/logs.html)
- [Apache escaping](https://httpd.apache.org/docs/2.4/mod/mod_log_config.html)
- [Nginx Combined format and escaping](https://nginx.org/en/docs/http/ngx_http_log_module.html)
