## v0.13.0

Changes since `v0.12.0`.

  Bug Fixes

  - Stop declaring webhook server cert Secret `data` fields in the Helm chart so that Helm v4 upgrades no longer conflict with the JobSet controller over field ownership of the certificate data (#1205)
