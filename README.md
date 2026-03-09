# cvrender: CV generation from a single data file

`cvrender` is a CV generation tool for academics who want a single, structured source of CV data, independent of Word/Pages/LaTeX. Content is kept in data files (`YAML`/`JSON`) and presentation is encoded in templates (`XML` DSL).  Output can then be rendered for different targets: CV.pdf, web CV, application packages, internal profile variants, dossiers, and print-ready pipelines.

## Why use it

- Platform-independent CV workflow.
- Consistent styling across many CV variants.
- Scalable updates: edit data once, regenerate all outputs.
- Reproducible rendering from CLI (good for version control and automation).
- Easy to maintain tailored views (full CV, short CV, teaching-focused, industry-focused).

## How it works

- `cv-data.yaml` (or JSON): structured academic data.
- `cv-template.xml`: transformation/rules and HTML structure with supported inline scripting for transforming variable output in-place.
- `cvrender` (default mode): produces HTML output.
- `cvrender --query`: inspects data paths for debugging/authoring.

### Synthetic placeholder examples

This repository includes examples for:

- `cv-template.xml`
- `cv-data.yaml`

## Quick start

Render HTML:

```bash
./cvrender --template cv-template.xml --data cv-data.yaml --output cv.html
```

For debug purposes, you can inspect data:

```bash
./cvrender --query --data cv-data.yaml --path publications/journals/0 --format text
```

## Common usage patterns

Merge multiple data files:

```bash
./dist/cvrender --template cv-template.xml --data base.yaml --data overrides.yaml --output cv.html
```

Set ad-hoc flags:

```bash
./dist/cvrender --template cv-template.xml --data cv-data.yaml --set show_references=1 --output cv.html
```

Preserve whitespace-sensitive formatting:

```bash
./dist/cvrender --template cv-template.xml --data cv-data.yaml --preserve-format --output cv.html
```

## CLI summary

- `cvrender --template <file> --data <file> [--data <file> ...] [--set k=v ...] [--preserve-format] [--output <file>]`
- `cvrender --query --data <file> [--data <file> ...] --path <expr> [--prefix <path>] [--format json|text] [--output <file>]`

`query` defaults to deterministic JSON output (`--format=json`).

## Supported `cv:` Tags

### `<cv:val>`
Renders a value (`of`) or conditionally renders when present (`if`), with optional prefix/suffix (`pf`/`sf`) and optional script processing (`proc`).
```xml
<cv:val of="contact/name"/>
<cv:val if="location" pf=", "/>
```

### `<cv:if>`
Conditional rendering block based on a truthy (`t`) or falsy (`n`) expression/path; also supports inline value/else attributes (`v`/`e`).
```xml
<cv:if t="award"><strong>{{award}}</strong></cv:if>
<cv:if t="selected" v="Selected" e="Not selected"/>
```

### `<cv:ite>`
If-then-else branching container that selects either `<cv:true>` or `<cv:false>` branch.
```xml
<cv:ite t="begin">
  <cv:true><cv:val of="begin"/>-<cv:val if="end"/></cv:true>
  <cv:false><cv:val if="end"/></cv:false>
</cv:ite>
```

### `<cv:true>`
True branch body for `<cv:ite>`.
```xml
<cv:true>Shown when condition is true</cv:true>
```

### `<cv:false>`
False branch body for `<cv:ite>`.
```xml
<cv:false>Shown when condition is false</cv:false>
```

### `<cv:for>`
Iterates over a collection (`in`) and renders children for each item; supports sorting and grouping options.
```xml
<cv:for in="publications/papers" sort="year,desc">
  <li>{{title}} ({{year}})</li>
</cv:for>
```

### `<cv:text>`
Injects literal text from attribute `v` (useful for explicit markup fragments).
```xml
<cv:text v="<hr/>"/>
```

### `<cv:func>`
Defines a named reusable template fragment (`id`) registered at parse time.
```xml
<cv:func id="dates">
  <span><cv:val of="begin"/>-<cv:val if="end"/></span>
</cv:func>
```

### `<cv:ex>`
Executes (expands) a named function/template fragment by `fn`.
```xml
<cv:ex fn="dates"/>
```

### `<cv:set>`
Sets/overrides a key (`k`) in the current execution scope from expression/value `v`.
```xml
<cv:set k="venue_short" v="{{/venues/{{venue_ref}}/venue_short}}"/>
```

### `<cv:json>`
Loads a JSON/YAML file into the current scope at render time (path from `fn`).
```xml
<cv:json fn="extra-data.yaml"/>
```

### `<cv:incl>`
Includes another XML transformation file and evaluates it in-place (also imports its `<cv:func>` definitions).
```xml
<cv:incl fn="partials/publications.xml"/>
```

### Inline scripting expressions
Inline scripting is supported for custom data transformations and computed conditions at render time.
```xml
<cv:val of="venue_short" proc="{{}}.replace(' ', '&nbsp;')"/>
<cv:if t="%int(d['year']) >= 2024">Recent item</cv:if>
```

## License

MIT License. See [LICENSE](LICENSE) for details.
