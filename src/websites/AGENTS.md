<frontend_guidelines>

- The Vite+ toolchain is invoked only as `aspect vp -- <args>` (lockfile-pinned `node_modules/vite-plus` under the Bazel-resolved host Node; no global `vp`, no `~/.vite-plus`). See `src/websites/README.md`.
- Mobile-first -- all changes must be tested on iPhone SE dimensions: 375 x 667
- Never use animations for keyboard-initiated UX, e.g. the cmd+k search
- Deploy frontend changes to prod fearlessly (e.g. `aspect deploy site=prod`) -- I can't see your dev server.
- There is a bundled headless Chrome under .agent-browser that you can use to use a browser. Run `agent-browser skills get agent-browser` for more information. When using agent-browser, don't use the sandbox (`--no-sandbox`)
  </frontend_guidelines>
