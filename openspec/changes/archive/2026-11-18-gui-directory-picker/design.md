## Context

The shared SourceForm already validates typed Location changes and clears Location errors on edit. The desktop host binds App through Wails; the browser preview and loopback app do not have that binding.

## Goals / Non-Goals

- Fill Location through a native picker without changing source submission.
- Do not add a picker to Git, Marketplace, or browser/loopback forms, or change the local HTTP API, configuration schema, or source admission rules.

## Decisions

- Mockup: Settings > Sources > Add a source > Directory registry.

  ```text
  Location
  [ /example/team-registry               ] [ Browse... ]
                                              [ Add source ]
  ```

  Activate Browse with the keyboard or pointer. Choose a folder to replace Location and clear its previous field error through the ordinary edit path. Cancel to keep the old Location; neither action submits the form. The chooser starts at an existing current Location or at home. In other source kinds or without the native binding, the Location input remains and Browse is omitted.
- Add `App.ChooseDirectory(defaultPath string) (string, error)`, backed by Wails `OpenDirectoryDialog` from the startup context. Keep directory resolution and the injected dialog testable without a real window. Return an empty path on cancel.
- Gate the control on directory kind and the binding's presence. Reuse SourceForm's existing validation and location edit path; keep manual input as the fallback if the dialog cannot supply a path. The accessible name is `Browse for folder`.

## Risks / Trade-offs

The loopback runner cannot drive an operating-system dialog. Use a fake Go dialog to prove the native adapter contract and a stub Wails binding in Playwright to prove the built frontend interacts with it and only writes through Add source. Treat preview screenshots and stubbed browser runs as evidence of the form, not proof of the operating-system dialog.
