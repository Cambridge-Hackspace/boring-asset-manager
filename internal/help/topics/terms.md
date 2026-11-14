# Search terms

People look for things by what they are, not only by what they're called: someone after "flexible filament" wants the TPU, and someone after a "flashlight" wants the torch. Search terms tell search what things are called, so it finds them.

- A **term** has a preferred label, such as *TPU*, and other names it goes by, such as *thermoplastic polyurethane*.
- A term can be **narrower** than another: *TPU* is narrower than *flexible filament*, which is narrower than *filament*.
- Terms can be **related**: *TPU* and *TPE*.

A search for a term, by any of its names, also finds its other names and everything narrower than it, however far down: "flexible filament" finds the TPU and the TPE. It doesn't go the other way, so searching "TPU" doesn't find every filament. Related terms are offered under the search to try instead, and aren't searched. Each thing found through a term says why, so a surprising result can be traced to the term that brought it.

## Keeping the terms

Those who manage search terms keep them on the **Search terms** page, under Catalog: add a term with **New term**, then link it to broader, narrower and related terms. **Try a search** shows what some words would find, before anyone searches for them.

Each term says where it came from: who added it, the assistant it came through, or the import it came in.

## Letting an assistant fill them in

BAM! doesn't run an assistant of its own for this, but yours can, through BAM!'s MCP server, as you and with your permissions. Ask it something like "look through our filament categories and add the terms people might search for", then check what it added with **Try a search**. Whatever it adds is marked as coming through it.

## Imports

Many terms at once come in as an **import**: a file of terms on the **Search terms** page, or a batch from an assistant. An import is kept together, so that if it isn't what you wanted, **Remove import** takes away its terms and links in one go. A file is JSON, a list of terms such as:

```
[
  {"label": "TPU", "alt_labels": ["thermoplastic polyurethane"], "broader": ["Flexible filament"], "related": ["TPE"]},
  {"label": "Flexible filament", "broader": ["Filament"]}
]
```

A term named only as broader or related, such as *Filament*, is added too. A term whose label is already there isn't added again; its links are added to the one there.
