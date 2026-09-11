# Aide de Camp

Read `docs/plans/build.md` for the approved product scope and `docs/design/interface.md` for the UI direction.

Use Go with embedded web assets, SQLite, server-rendered HTML and Datastar. Preserve the original README's seed vision. ADC is general-purpose organizational software; keep GitHub-specific concepts out of the core assignment model.

An assignment must survive provider/session failure and continue through independent review and correction without repeated human prompts. Implementation and review require different model families. Provider identity alone is insufficient. Never claim advisory tool permissions are technically enforced.

Keep secrets out of source, logs and workspaces. Use real provider-backed behavior; label fixtures as fixtures and do not populate the working UI with invented activity. Do not publish or merge repository changes as part of application development unless explicitly authorized for that action.

Run `make verify` before handing off changes. Test meaningful lifecycle, permissions/routing, persistence and authentication behavior. Keep documentation current and record material limitations honestly.
