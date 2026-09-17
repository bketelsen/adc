# Single-service installation

Run ADC as one dedicated unprivileged user inside the installation's Incus instance. Build `bin/adc`, install it as `/usr/local/bin/adc`, install the tested Copilot CLI runtime, and supply the CLIs and MCP server processes required by that installation's work. Provider authentication belongs to the service user's environment; an ADC personal token connection is stored separately in ADC's private data directory.

The application binary embeds its web assets. Go is needed to build, not to run it. Git and provider/repository runtimes remain external dependencies.

Example systemd service, to adapt and install deliberately:

```ini
[Unit]
Description=Aide de Camp
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=adc
Group=adc
StateDirectory=adc
StateDirectoryMode=0700
WorkingDirectory=/var/lib/adc
Environment=COPILOT_CLI_PATH=/usr/local/bin/copilot
ExecStart=/usr/local/bin/adc serve -addr 127.0.0.1:8789 -data /var/lib/adc -secure-cookies
Restart=on-failure
RestartSec=5
UMask=0077
TimeoutStopSec=60

[Install]
WantedBy=multi-user.target
```

Place an HTTPS reverse proxy in front of the service, preserve the original Host header, allow long-lived server-sent event connections, and disable response buffering for `/live`. `-secure-cookies` requires HTTPS at the browser; omit it for an HTTP-only localhost development session. If the reverse proxy is outside the instance, bind to the instance's intended internal address and restrict access at that network boundary.

Keep the complete data directory, including `credential.key`, workspaces, and provider state. For an initial cold backup, stop ADC cleanly and copy the entire directory with its permissions, then restart it. Do not copy only a live SQLite database file and omit its WAL. Backup/export UI and automated restore qualification remain later work.

For this development checkout, `deploy/adc.service` is a persistent systemd user unit. `make start` installs it under `~/.config/systemd/user`, enables it for the user manager's `default.target`, and starts it on `0.0.0.0:8789`. User lingering must be enabled for startup without an interactive login. The unit deliberately starts ADC with a minimal explicit environment rather than inheriting unrelated desktop-session credentials.

This service template alone does not enforce per-agent access boundaries. Advisory assignments run native commands as the service user. Protected assignments instead use Bubblewrap and the ADC connection gateway. Install the distribution's Bubblewrap, util-linux, Bash, Python, Git, curl and CA packages. Keep ADC state in `/var/lib/adc`, outside runtime/CA mount paths.

The protected profile passed under an unprivileged Debian trixie Incus container with nesting enabled and commands launched as UID/GID 65534. Configure `security.nesting=true` explicitly on the ADC instance; a privileged instance is unnecessary. Without nesting, the tested container refused the private `/proc` mount and ADC failed closed. Do not add `RestrictNamespaces` or other unit restrictions that prevent this profile without requalifying it. See [permission setup and limitations](../design/permissions.md) before choosing protected defaults.
