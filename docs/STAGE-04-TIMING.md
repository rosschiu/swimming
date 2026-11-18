# Administrator lesson timing settings

This stage persists three agreed timing settings under School setup. The defaults are a 24-hour parent change cutoff, attendance opening 15 minutes after lesson start, and a three-day correction window. Booking, attendance and change handlers apply the relevant setting when their respective workflows are implemented.

Migration 12 creates one versioned settings row. The API validates integer inputs, checks the current administrator, and writes the values and audit event in one transaction. Each edit includes the version the administrator reviewed. A stale save returns a conflict and leaves the stored values intact.

Acceptance requires a real administrator to update the settings, refresh and see the saved values. Two editors loading the same version must not overwrite each other. A non-administrator must be unable to read or write the endpoint. Validate the original defaults on migration, lower and upper boundaries, rollback, and audit history. The exact attendance deadline anchor and handling of corrections after a credit has been reused remain separate policy decisions.
