# omnishell logo concepts

Four draft directions for the omnishell logo, each as a standalone SVG mark
(`omnishell-logo-*.svg`, 256×256 rounded-square tile, usable as an avatar) and
side by side in [`overview.svg`](overview.svg) (also rendered as
[`overview.png`](overview.png)). The sheet shows every mark large on light and
dark backgrounds, at 64/32/16 px, and combined with a wordmark.

![Overview of the four concepts](overview.png)

The wordmark in the overview is plain monospace text for comparison only; it is
not a designed wordmark.

## References

What the logos of comparable tools do well:

| Tool | Mark | Takeaway |
|:--|:--|:--|
| [Starship](https://github.com/starship/starship/tree/master/media) | Rocket in a gradient circle | A picture of the name, one strong silhouette, readable as a tiny avatar |
| [zoxide](https://github.com/ajeetdsouza/zoxide/tree/main/contrib) | Keycap with a prompt and a "Z" | Literal and instantly recognisable; light and dark variants ship together |
| [mise](https://github.com/jdx/mise/tree/main/docs/public) | Chef character plus lowercase wordmark | A pun on the name (*mise en place*) gives the tool personality |

Common thread: each mark comes from the tool's own name or workflow, has a
single silhouette, and survives as a small square avatar.

## Concepts

| # | Direction | Idea | Strengths | Weaknesses |
|:-:|:--|:--|:--|:--|
| 1 | **Shell** | Spiral shell ending in a block cursor | Direct link to the name and the 🐚 already used in the README; friendly | Fine spiral turns blur at 16 px |
| 2 | **Modules** | Six segments form a hexagon, one segment lit amber | Says "modular" and "all in one" at once; uses the full palette | Busiest of the four; weakest at 16 px |
| 3 | **Undo** | A prompt inside a loop that returns | Encodes *reversible*, the tool's distinguishing trait | Close to generic "refresh / history" icons |
| 4 | **Monogram** | An "o" with a block cursor in its centre | Simplest and boldest; holds up at every size | Least specific; could be mistaken for a record or power icon |

## Decision

**Concept 2 (Modules)** is the chosen direction, with the shell prompt `>_` as
its centre (see the refinement below).

- It says *modular* and *terminal* in one mark: six module segments around a
  shell prompt, which is the tool's job in a single picture.
- It uses the README palette and stays distinct from the generic terminal
  cliché.
- The first-round recommendation was Concept 1 (Shell), for its link to the 🐚
  used in the README. Concept 2 was preferred after comparing both; concepts 1,
  3 and 4 stay in this folder as unused alternatives.

## Refined direction: Modules

Concept 2 was refined in [`overview-modules.svg`](overview-modules.svg)
([PNG](overview-modules.png)):

| File | Use |
|:--|:--|
| `omnishell-logo-2-modules.svg` | Main mark: six module segments around an amber shell prompt, on a dark tile |
| `omnishell-logo-2-modules-glyph.svg` | Same mark without the tile, for READMEs on light or dark pages |
| `omnishell-logo-2-modules-small.svg` | Same mark with coarser details (thicker segments, narrower gaps, bolder prompt) for 32 and 16 px |

Changes from the first draft: two tones for the modules, clearly separated
segments, a larger hexagon inside the tile, and the shell prompt `>_` as the
amber centre so the terminal is the focal point. The small-size variant keeps
the same structure and colours and only simplifies the details.

Size guidance:

- **64 px and up:** main mark (or the glyph where no tile is wanted).
- **32 px:** small-size variant; the prompt chevron stays readable.
- **16 px:** use the small-size variant as well, but expect only the hexagon
  silhouette and the amber chevron to read; the underscore is lost. If a
  favicon needs more, a dedicated 16 px drawing is a follow-up.

## Palette

Tiles and strokes use the README accent palette: green `#22c55e`, blue
`#3776ab`, sky `#0ea5e9`, purple `#8b5cf6`, amber `#f59e0b`, plus ink
`#0f172a` for dark tiles.
