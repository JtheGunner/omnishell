# OMNIS-1 — Manifest-Feld `conflicts` für unverträgliche Module

_Plan generated 2026-09-07 · branch `feat/OMNIS-1-manifest-conflicts-field`_

**Klassifizierung:** bounded — neues optionales Manifest-Feld, analog zu
`requires`/`after` validiert und durchgesetzt. Durchsetzung **nur zur Plan-Zeit**
(kein `enable`-Vorabcheck), konsistent mit dem heutigen `requires`-Verhalten.

## 1. Schema — `internal/module/manifest.go`

- Feld `Conflicts []string \`toml:"conflicts"\`` im `Manifest`-Struct, root-level
  neben `Requires`/`After`.
- `ValidateManifest`: jeder Eintrag muss `idRe` erfüllen und ungleich der eigenen
  id sein; `requires ∩ conflicts = ∅` innerhalb eines Manifests. Fehler als
  `ManifestError{Field: "conflicts", …}`.
- **Kein** `ManifestSchemaVersion`-Bump — additives, rückwärtskompatibles Feld
  (wie `description`/`homepage` in 0.2.0). Doku-Hinweis: Manifeste mit `conflicts`
  parsen auf älteren omnishell-Versionen nicht (strikter unknown-key-Check).

## 2. Durchsetzung — `internal/graph/graph.go`

- Neuer Fehlertyp `ConflictError{Module, Conflicts string}` →
  `module "atuin" conflicts with "fzf", which is also enabled`.
- In `Order(active)` vor dem Topo-Sort: für jedes aktive Modul jeden
  `Conflicts`-Eintrag prüfen; ist die id ebenfalls aktiv → `ConflictError`.
- Einseitige Deklaration genügt. Aktive ids sortiert iterieren → deterministische
  Paarwahl bei mehreren Konflikten.

## 3. Kein Eingriff in apply/doctor/exit

`plan.go` verpackt `graph.Order`-Fehler bereits als `ConfigError`; `doctor.go`
reicht `ConfigError` unverändert durch; `exit.go` mappt `ConfigError` → Exit 2.
`apply`, `diff`, `doctor` erben das Verhalten. Keine eigene `doctor`-Finding —
konsistent damit, wie fehlendes `requires` heute als Exit 2 (ohne Finding)
erscheint.

## 4. Doku

- `docs/writing-a-module.md`: Abschnitt „`requires` vs `after`" um `conflicts`
  erweitern + im annotierten Manifest-Beispiel zeigen.
- `CHANGELOG.md`: Unreleased → Added.

## 5. Tests (TDD, Red zuerst)

- `internal/graph/graph_test.go`: zwei gleichzeitig aktive, kollidierende Module
  → `ConflictError` (ein- und beidseitige Deklaration); Konfliktziel nicht aktiv
  → kein Fehler; deterministische Paarwahl.
- `internal/module/manifest_test.go`: `conflicts` mit ungültiger id / self /
  `requires`-Overlap abgelehnt; gültiges `conflicts` akzeptiert.
- `internal/engine/plan_test.go`: `ComputePlan` liefert `ConfigError` (umschließt
  `ConflictError`), wenn die Config beide aktiviert.
- `internal/cli`: Exit-Code-2-Assertion (Präzedenz in `apply_test.go` /
  `exit_test.go`).

## Dateien

`internal/module/manifest.go` · `internal/graph/graph.go` · je zugehörige
`_test.go` · `internal/engine/plan_test.go` · `internal/cli/*_test.go` ·
`docs/writing-a-module.md` · `CHANGELOG.md`.
Unverändert: `apply.go`, `doctor.go`, `exit.go`.
