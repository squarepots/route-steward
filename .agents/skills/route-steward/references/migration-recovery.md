# Migration and recovery

## Replace a server

`migrate-route` records a resumable transaction in private `migrations.json`:

1. inspect the source Route and current drift;
2. register or select the replacement Server;
3. create and deploy replacement Route and Link state;
4. audit the replacement and run real traffic health;
5. switch affected Profiles and ClientTargets;
6. republish existing subscription targets;
7. validate the new client path.

The current path stays selected until the replacement passes health. A failed or interrupted client switch restores the old selection or records a rollback step. Retry `workflow-blocked` with the same source Route and replacement Server.

Completion leaves the old remote service and external capacity in place. Retirement is a separate destructive operation requested by the user.

## Back up and recover

`route-steward backup --private-dir <directory>` creates an encrypted archive of inventory, secrets, SSH material, and active migration checkpoints. Enter the password only in the local 7-Zip prompt.

Restore into a clean destination:

```text
route-steward recover --archive <path> --private-dir <directory>
```

Recovery verifies the manifest and archive paths, relocates SSH and delivery paths, applies private permissions, validates current inventory, and recreates empty observed evidence. Restored migrations return to a revalidation stage.

After recovery, read capabilities, context, drift, and migrations. Audit existing Routes before a remote write, then render clients from the restored state.

Schema-1 recovery archives remain supported. Restore validates the old archive, translates its inventory into canonical schema 2, relocates private paths, and recreates empty observed evidence before any remote write. New recovery archives contain schema-2 inventory while auxiliary recovery and migration formats keep their own schema versions.
