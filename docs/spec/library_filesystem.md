# Filesystem Library Extensions (Spec)

This spec defines module-level extensions for the filesystem library module.
It builds on the core mu library commands and adds query semantics for
semantic search and duplicate management. These extensions are optional and
module-specific; other libraries may ignore them.

## Semantic Search Query Grammar
The filesystem library uses `library.search` with a query string that supports
structured filters and similarity operators.

### Grammar (informal)
```
query        := term | filter | similar | semantic | (query " " query)
term         := any text token
filter       := key ":" value
similar      := "similar:" item_id
semantic     := "semantic:" text
```

### Reserved filter keys
- `artist`, `album`, `title`
- `genre`, `year`, `media`
- `duration` (in seconds, supports ranges: `duration:120..300`)
- `quality` (e.g. `lossless`, `aac`, `h264`)
- `container` (module-specific container IDs)

### Examples
```
late night jazz trumpet
genre:jazz year:1959
media:video sci fi ambient
similar:track:abc123
semantic:focus study music
duration:120..300 genre:ambient
```

### Behavior
- `similar:<item_id>` returns items most similar to that item.
- `semantic:<text>` runs semantic search on the text even if it does not match
  lexical tokens.
- If multiple terms are provided, the module combines lexical and semantic
  scores (weighted; module-specific).

## Duplicate Management (Module Extension)
The filesystem library provides optional commands for duplicate inspection and
policy actions.

### Commands
- `library.dupes.list`: list duplicate groups and preferred item
- `library.dupes.apply`: apply a policy action to duplicates

### Payloads
```json
// library.dupes.list
{
  "start": 0,
  "count": 50,
  "policy": "prefer|hide|merge",
  "minConfidence": 0.7
}
```

```json
// library.dupes.apply
{
  "policy": "prefer|hide|merge",
  "dryRun": false
}
```

### Replies
```json
// library.dupes.list reply body
{
  "groups": [
    {
      "groupId": "dup:123",
      "itemIds": ["track:a", "track:b"],
      "preferredId": "track:b",
      "confidence": 0.92
    }
  ],
  "total": 1
}
```

### Notes
- These commands are optional; clients should handle `unsupported command`.
- Policies are module-specific and may map to different behaviors.

