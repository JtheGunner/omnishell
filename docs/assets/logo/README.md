# omnishell logo

The logo is a hexagon of six module segments around an amber shell prompt
`>_`: the modules of one configuration, with the terminal at the centre.

![Overview of the logo variants](overview.png)

| File | Use |
|:--|:--|
| [`omnishell-logo.svg`](omnishell-logo.svg) | Main mark on a dark tile; also usable as an avatar |
| [`omnishell-logo-glyph.svg`](omnishell-logo-glyph.svg) | Same mark without the tile, for READMEs on light or dark pages |
| [`omnishell-logo-small.svg`](omnishell-logo-small.svg) | Same mark with coarser details (thicker segments, narrower gaps, bolder prompt) for 32 and 16 px |

[`overview.svg`](overview.svg) shows all three on light and dark backgrounds,
at 64/32/16 px. There is no designed wordmark yet.

## Size guidance

- **64 px and up:** main mark, or the glyph where no tile is wanted.
- **32 px:** small-size variant; the prompt chevron stays readable.
- **16 px:** use the small-size variant as well, but expect only the hexagon
  silhouette and the amber chevron to read; the underscore is lost. If a
  favicon needs more, a dedicated 16 px drawing is a follow-up.

## Why this direction

- It says *modular* and *terminal* in one picture: six module segments around
  a shell prompt, which is the tool's job.
- It uses the README accent palette and avoids the generic terminal cliché of
  `>_` on a neon gradient.
- Other directions were explored and dropped: a spiral shell ending in a block
  cursor, a prompt inside a loop that returns (reversible), and an "o"
  monogram with a cursor. The shell was the closest alternative, for its link
  to the 🐚 used in the README.

## References

What the logos of comparable tools do well:

| Tool | Mark | Takeaway |
|:--|:--|:--|
| [Starship](https://github.com/starship/starship/tree/master/media) | Rocket in a gradient circle | A picture of the name, one strong silhouette, readable as a tiny avatar |
| [zoxide](https://github.com/ajeetdsouza/zoxide/tree/main/contrib) | Keycap with a prompt and a "Z" | Literal and instantly recognisable; light and dark variants ship together |
| [mise](https://github.com/jdx/mise/tree/main/docs/public) | Chef character plus lowercase wordmark | A pun on the name (*mise en place*) gives the tool personality |

Common thread: each mark comes from the tool's own name or workflow, has a
single silhouette, and survives as a small square avatar.

## Palette

Sky `#0ea5e9` and blue `#3776ab` for the segments, amber `#f59e0b` for the
prompt, ink `#0f172a` for the tile (outlined in `#334155` so it stays visible
on dark backgrounds).
