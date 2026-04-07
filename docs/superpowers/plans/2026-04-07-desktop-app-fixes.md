# Desktop App Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the artwork loader use-after-free segfault and bring desktop library/playlist parsing, batch resolution, and lib-ref handling to parity with the Android client.

**Architecture:** Four independent fixes in `integrations/desktop_app/`. Task 1 is a memory-safety fix in `ArtworkLoader` (mark callback `owned`, refactor wrapper construction). Task 2 ports Android's container detection and richer item-field reading into `library_view.vala`'s metadata helpers. Task 3 reshapes `LibraryRepository.resolve_batch` to return an `id → object` map and updates the two callers. Task 4 replaces the brittle 5-segment `split_lib_ref` with a longest-prefix matcher driven by the live `NodeRepository.get_libraries()` set.

**Tech Stack:** Vala 0.56 (GTK4, libsoup3, json-glib, GLib HashTable/GenericArray), Meson + Ninja build, the project's existing `LibraryRepository`, `NodeRepository`, `CommandCorrelator`.

---

## File Structure

**Modified files (no new files needed):**

- `integrations/desktop_app/src/ui/widgets/artwork_loader.vala` — Make `load_async` take `owned ArtworkCallback`. Construct `ArtworkCallbackWrapper` with `(owned) callback`. (Task 1)
- `integrations/desktop_app/src/ui/library_view.vala` — Five sub-edits:
  - Update item helpers (`is_container_item`, `get_item_artwork_url`, `get_item_artist`, plus a new `get_item_overview`). (Task 2)
  - Change `browse_root()` from `"0"` → `""`. (Task 2)
  - Rewrite `on_play_all` / `on_queue_all` to look up resolved entries by id. (Task 3)
  - Replace `split_lib_ref` with `find_library_for_item` + `strip_lib_prefix` and update `do_view_playlist`. (Task 4)
- `integrations/desktop_app/src/repositories/library_repository.vala` — Change `resolve_batch` return type to `HashTable<string, Json.Object>?`, key by `itemId`. (Task 3)

**Why we keep this in existing files:** `library_view.vala` already owns these helpers; splitting them would fight the established structure. The fixes are localized and code-sharing within the file is fine. Creating new utility classes for ~50 lines would be over-engineering.

---

## Task 1: Fix ArtworkLoader use-after-free

**Why this is the segfault:** A clean rebuild emits `warning: copying delegates is not supported` at `artwork_loader.vala:52` and `:60`. The generated C calls `mu_artwork_callback_wrapper_new (callback, callback_target, NULL)` — note the `NULL` `destroy_notify`. The wrapper holds a raw pointer to the lambda's closure block, but the *caller* (e.g. `library_view.c:6616`) immediately calls `block13_data_unref(_data13_)` after `build_track_row` returns, freeing the closure. When Soup later finishes the request, `finish_request` invokes the wrapper, which calls into freed memory.

**Fix:** Mark the delegate parameter `owned` so Vala increments the closure block's refcount on entry to `load_async`, hoist wrapper construction so the ownership transfer happens exactly once, and let the wrapper's existing `owned ArtworkCallback` constructor carry the `destroy_notify` through.

**Files:**
- Modify: `integrations/desktop_app/src/ui/widgets/artwork_loader.vala:35-97`

- [ ] **Step 1: Verify the warnings are present in a clean rebuild**

Run:
```bash
cd integrations/desktop_app
rm -f builddir/media-utopia.p/src/ui/widgets/artwork_loader.*
ninja -C builddir 2>&1 | grep -E "(delegate|artwork_loader)"
```

Expected: two `copying delegates is not supported` warnings at lines `52` and `60` of `artwork_loader.vala`. (If they are missing, stop and ask — the diagnosis is wrong.)

- [ ] **Step 2: Apply the load_async fix**

Replace the body of `load_async` (the whole method, lines ~35-97) with this implementation. Note three changes: `owned ArtworkCallback callback` in the signature, hoisted wrapper construction with `(owned) callback`, and a single `wrapper` variable used in both branches.

```vala
public void load_async (string url, owned ArtworkCallback callback) {
    if (url.length == 0) {
        callback (null);
        return;
    }

    /* Cache hit */
    var cached = cache.lookup (url);
    if (cached != null) {
        callback (cached);
        return;
    }

    /* All remaining paths queue the callback into a wrapper.
     * Hoist construction so the (owned) transfer happens exactly once. */
    var wrapper = new ArtworkCallbackWrapper ((owned) callback);

    /* Already in flight — queue callback */
    if (inflight.contains (url)) {
        var existing = pending.lookup (url);
        if (existing != null) {
            existing.add (wrapper);
        }
        return;
    }

    /* Mark in-flight */
    inflight.insert (url, true);
    var waiters = new GenericArray<ArtworkCallbackWrapper> ();
    waiters.add (wrapper);
    pending.insert (url, waiters);

    /* Fetch */
    var msg = new Soup.Message ("GET", url);
    if (msg == null) {
        finish_request (url, null);
        return;
    }

    session.send_and_read_async.begin (msg, GLib.Priority.DEFAULT, null,
        (obj, res) => {
            try {
                var bytes = session.send_and_read_async.end (res);
                if (bytes == null || bytes.get_size () == 0) {
                    finish_request (url, null);
                    return;
                }

                /* Check HTTP status */
                if (msg.status_code < 200 || msg.status_code >= 300) {
                    warning ("Artwork fetch failed: HTTP %u for %s",
                        msg.status_code, url);
                    finish_request (url, null);
                    return;
                }

                var texture = Gdk.Texture.from_bytes (bytes);
                cache.insert (url, texture);
                finish_request (url, texture);

            } catch (GLib.Error e) {
                warning ("Artwork load error for %s: %s", url, e.message);
                finish_request (url, null);
            }
        }
    );
}
```

- [ ] **Step 3: Rebuild and confirm the warnings are gone**

Run:
```bash
cd integrations/desktop_app
rm -f builddir/media-utopia.p/src/ui/widgets/artwork_loader.*
ninja -C builddir 2>&1 | grep -E "(delegate|artwork_loader)"
```

Expected: no `copying delegates is not supported` warnings. The build line for `artwork_loader.c.o` should appear without errors.

- [ ] **Step 4: Inspect the regenerated C to confirm destroy_notify is now wired**

Run:
```bash
grep -n "mu_artwork_callback_wrapper_new" integrations/desktop_app/builddir/media-utopia.p/src/ui/widgets/artwork_loader.c
```

Expected: every `mu_artwork_callback_wrapper_new` call now passes a non-NULL third argument (a `*_destroy_notify` function), instead of `NULL`. If any call still passes `NULL`, the fix is incomplete — re-read the file and confirm `(owned) callback` was used.

- [ ] **Step 5: Commit**

```bash
cd integrations/desktop_app
git add src/ui/widgets/artwork_loader.vala
git commit -m "$(cat <<'EOF'
fix(desktop): take owned callback in ArtworkLoader.load_async

Vala emitted "copying delegates is not supported" because load_async
took an unowned ArtworkCallback and then stored it in a wrapper. The
generated wrapper constructor was called with a NULL destroy_notify,
so the lambda's closure block was freed by the caller immediately
after build_*_row returned, leaving a dangling target for the async
HTTP completion to invoke.

Mark the parameter owned, hoist wrapper construction so the ownership
transfer happens once, and let the existing owned-delegate wrapper
constructor carry the destroy_notify through.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Port Android item parsing into library_view.vala

**Why:** On filesystem libraries the desktop currently misclassifies artist/album/folder containers as tracks (because it only checks `type == "container"` and `childCount`/`children`), and ignores `imageUrl`, `artists[]`, and `overview`. Android's `parseLibraryItem` (`LibraryRepository.kt:440-488`) is the reference. We don't need a structural rewrite — just update the helpers and the root container ID.

**Files:**
- Modify: `integrations/desktop_app/src/ui/library_view.vala:558-563` (root container id)
- Modify: `integrations/desktop_app/src/ui/library_view.vala:1716-1797` (item helpers)

- [ ] **Step 1: Update browse_root to use empty string**

Replace the body of `browse_root` (around lines 558-564) with:

```vala
private void browse_root () {
    nav_stack = new GenericArray<BrowseCrumb> ();
    nav_stack.add (new BrowseCrumb ("", "Root"));
    browse_offset = 0;
    is_search_mode = false;
    load_container ("", true);
}
```

Reason: Android's `LibraryViewModel.browseContainerOnLibrary(libraryNodeId, "")` (`LibraryViewModel.kt:156`) uses `""` as the universal root id; the protocol treats `""` as "default root" for both filesystem and Jellyfin libraries. The hardcoded `"0"` was Jellyfin-specific and breaks fs libraries.

- [ ] **Step 2: Rewrite is_container_item to match Android's container detection**

Replace `is_container_item` (around lines 1716-1730) with:

```vala
private bool is_container_item (Json.Object item) {
    /* Container patterns and explicit overrides — keep in sync with
     * Android LibraryRepository.kt:61 (containerPatterns,
     * explicitContainerTypes, explicitLeafTypes). */
    string item_type = "";
    if (item.has_member ("type") && !item.get_null_member ("type")) {
        item_type = item.get_string_member ("type") ?? "";
    }
    var type_lower = item_type.down ();

    /* Explicit leaves win over patterns. */
    if (type_lower == "podcastepisode") return false;

    /* Explicit containers. */
    if (type_lower == "podcast") return true;

    /* Pattern match: type contains the pattern, OR itemId starts with "{pattern}:". */
    string item_id = "";
    if (item.has_member ("itemId") && !item.get_null_member ("itemId")) {
        item_id = item.get_string_member ("itemId") ?? "";
    } else if (item.has_member ("id") && !item.get_null_member ("id")) {
        item_id = item.get_string_member ("id") ?? "";
    }
    var id_lower = item_id.down ();

    string[] patterns = { "container", "artist", "album", "folder" };
    foreach (var pattern in patterns) {
        if (type_lower.contains (pattern)) return true;
        if (id_lower.has_prefix (pattern + ":")) return true;
    }

    /* Legacy fallbacks the old desktop code relied on. */
    if (item.has_member ("childCount")) return true;
    if (item.has_member ("children")) return true;

    return false;
}
```

- [ ] **Step 3: Extend get_item_artwork_url to read imageUrl as well**

Replace `get_item_artwork_url` (around lines 1765-1780) with:

```vala
private string get_item_artwork_url (Json.Object item) {
    /* Direct fields: prefer artworkUrl, fall back to imageUrl. */
    if (item.has_member ("artworkUrl") && !item.get_null_member ("artworkUrl")) {
        var url = item.get_string_member ("artworkUrl");
        if (url != null && url.length > 0) return url;
    }
    if (item.has_member ("imageUrl") && !item.get_null_member ("imageUrl")) {
        var url = item.get_string_member ("imageUrl");
        if (url != null && url.length > 0) return url;
    }

    /* Nested metadata. */
    if (item.has_member ("metadata") && !item.get_null_member ("metadata")) {
        var meta = item.get_object_member ("metadata");
        if (meta.has_member ("artworkUrl") && !meta.get_null_member ("artworkUrl")) {
            var url = meta.get_string_member ("artworkUrl");
            if (url != null && url.length > 0) return url;
        }
        if (meta.has_member ("imageUrl") && !meta.get_null_member ("imageUrl")) {
            var url = meta.get_string_member ("imageUrl");
            if (url != null && url.length > 0) return url;
        }
    }

    return "";
}
```

- [ ] **Step 4: Extend get_item_artist to read artists[] joined**

Replace `get_item_artist` (around lines 1782-1797) with:

```vala
private string get_item_artist (Json.Object item) {
    /* Try artists[] first (Android-style). */
    var joined = read_artists_array (item);
    if (joined.length > 0) return joined;

    /* Direct artist scalar. */
    if (item.has_member ("artist") && !item.get_null_member ("artist")) {
        var artist = item.get_string_member ("artist");
        if (artist != null && artist.length > 0) return artist;
    }

    /* Nested metadata. */
    if (item.has_member ("metadata") && !item.get_null_member ("metadata")) {
        var meta = item.get_object_member ("metadata");
        var meta_joined = read_artists_array (meta);
        if (meta_joined.length > 0) return meta_joined;
        if (meta.has_member ("artist") && !meta.get_null_member ("artist")) {
            var artist = meta.get_string_member ("artist");
            if (artist != null && artist.length > 0) return artist;
        }
    }

    return "";
}

private string read_artists_array (Json.Object obj) {
    if (!obj.has_member ("artists") || obj.get_null_member ("artists")) return "";
    var arr = obj.get_array_member ("artists");
    var parts = new GenericArray<string> ();
    for (uint i = 0; i < arr.get_length (); i++) {
        var node = arr.get_element (i);
        if (node.get_node_type () == Json.NodeType.VALUE) {
            var s = node.get_string ();
            if (s != null && s.length > 0) parts.add (s);
        }
    }
    if (parts.length == 0) return "";

    var sb = new StringBuilder ();
    for (uint i = 0; i < parts.length; i++) {
        if (i > 0) sb.append (", ");
        sb.append (parts[i]);
    }
    return sb.str;
}
```

- [ ] **Step 5: Build and confirm no warnings/errors**

Run:
```bash
cd integrations/desktop_app
ninja -C builddir 2>&1 | grep -E "(warning|error)" | grep -v "deprecated since"
```

Expected: no new warnings; no errors. The pre-existing GTK 4.10 deprecation warning may still appear and is unrelated.

- [ ] **Step 6: Commit**

```bash
cd integrations/desktop_app
git add src/ui/library_view.vala
git commit -m "$(cat <<'EOF'
fix(desktop): port Android item parsing to library_view

is_container_item now matches Android's containerPatterns
(artist/album/folder/container) plus the explicit podcast/
podcastepisode overrides, instead of only treating
type=="container" or childCount/children as containers. This
fixes filesystem libraries that surface artist/album/folder
nodes without childCount.

get_item_artwork_url also reads imageUrl, get_item_artist reads
artists[] joined, and browse_root uses "" as the universal root
container id (matches Android LibraryViewModel.kt:156).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: ID-keyed batch resolve

**Why:** `LibraryRepository.resolve_batch` returns a positional `GenericArray<Json.Object>` and `on_play_all`/`on_queue_all` zip it back to the request `track_ids` by index. If the library skips, reorders, or fails any item, every subsequent row gets paired with the wrong source URL and metadata. Android's `resolveBatch` builds a `localToFull` map and assembles results keyed by `itemId` (`LibraryRepository.kt:217-254`). We mirror that.

**Files:**
- Modify: `integrations/desktop_app/src/repositories/library_repository.vala:86-130`
- Modify: `integrations/desktop_app/src/ui/library_view.vala:1083-1198`

- [ ] **Step 1: Change resolve_batch to return a HashTable keyed by itemId**

Replace the `resolve_batch` method (around lines 86-130 in `library_repository.vala`) with:

```vala
/**
 * Resolve multiple items in batches of 20.
 * Returns a map of itemId → result object so callers can pair
 * results with their request IDs even when the library skips,
 * reorders, or partially fails items. Returns null only if
 * nothing was resolved at all.
 */
public async HashTable<string, Json.Object>? resolve_batch (string library_id,
                                                              string[] item_ids,
                                                              bool metadata_only = false) {
    var results = new HashTable<string, Json.Object> (str_hash, str_equal);

    int offset = 0;
    while (offset < item_ids.length) {
        int chunk_end = int.min (offset + BATCH_SIZE, item_ids.length);
        var chunk = new string[chunk_end - offset];
        for (int i = 0; i < chunk.length; i++) {
            chunk[i] = item_ids[offset + i];
        }

        var body = LibraryBodies.resolve_batch (chunk, metadata_only);
        var reply = yield correlator.send (
            library_id, "library.resolveBatch", body
        );

        if (reply == null || !reply.ok || reply.body == null) {
            /* Partial failure — return what we have so far, or null if nothing */
            return results.size () > 0 ? results : null;
        }

        if (reply.body.has_member ("items") &&
            !reply.body.get_null_member ("items")) {
            var items = reply.body.get_array_member ("items");
            for (uint i = 0; i < items.get_length (); i++) {
                var item = items.get_object_element (i);
                if (!item.has_member ("itemId")) continue;
                var iid = item.get_string_member ("itemId");
                if (iid == null || iid.length == 0) continue;

                results.set (iid, item);

                /* Cache metadata for each resolved item */
                if (item.has_member ("metadata") &&
                    !item.get_null_member ("metadata")) {
                    metadata_cache.set (iid, item.get_object_member ("metadata"));
                }
            }
        }

        offset = chunk_end;
    }

    return results.size () > 0 ? results : null;
}
```

- [ ] **Step 2: Rewrite on_play_all to look up resolved entries by id**

Replace `on_play_all` (around lines 1083-1145 in `library_view.vala`) with:

```vala
private void on_play_all () {
    var renderer_id = active_repo.active_renderer_id;
    var library_id = get_selected_library_id ();
    if (library_id == null || renderer_id.length == 0) return;

    /* Collect all track item IDs from current_items */
    var track_ids = new GenericArray<string> ();
    var track_items = new GenericArray<Json.Object> ();
    for (uint i = 0; i < current_items.length; i++) {
        if (!is_container_item (current_items[i])) {
            track_ids.add (get_item_id (current_items[i]));
            track_items.add (current_items[i]);
        }
    }

    if (track_ids.length == 0) return;

    /* Resolve all tracks in batch */
    var id_array = new string[track_ids.length];
    for (uint i = 0; i < track_ids.length; i++) {
        id_array[i] = track_ids[i];
    }

    library_repo.resolve_batch.begin (
        library_id, id_array, false,
        (obj, res) => {
            var resolved_map = library_repo.resolve_batch.end (res);
            if (resolved_map == null || resolved_map.size () == 0) return;

            var entries = new Json.Array ();
            for (uint i = 0; i < track_ids.length; i++) {
                var iid = track_ids[i];
                var resolved = resolved_map.lookup (iid);
                if (resolved == null) continue;
                var browse_item = track_items[i];
                var entry = build_queue_entry_from_batch_resolved (
                    iid, browse_item, resolved);
                if (entry != null) {
                    entries.add_object_element (entry);
                }
            }

            if (entries.get_length () == 0) return;

            /* Replace queue and play from start */
            lease_mgr.ensure_lease.begin (renderer_id, (lobj, lres) => {
                var lease = lease_mgr.ensure_lease.end (lres);
                if (lease == null) return;

                var set_body = QueueBodies.set_entries (0, entries);
                correlator.send.begin (
                    renderer_id, "queue.set", set_body, lease, -1, 5000,
                    (sobj, sres) => {
                        var set_reply = correlator.send.end (sres);
                        if (set_reply == null || !set_reply.ok) return;

                        correlator.send_fire_and_forget (
                            renderer_id, "playback.play",
                            PlaybackBodies.play (0), lease);
                    }
                );
            });
        }
    );
}
```

- [ ] **Step 3: Rewrite on_queue_all the same way**

Replace `on_queue_all` (around lines 1147-1198) with:

```vala
private void on_queue_all () {
    var renderer_id = active_repo.active_renderer_id;
    var library_id = get_selected_library_id ();
    if (library_id == null || renderer_id.length == 0) return;

    /* Collect all track item IDs from current_items */
    var track_ids = new GenericArray<string> ();
    var track_items = new GenericArray<Json.Object> ();
    for (uint i = 0; i < current_items.length; i++) {
        if (!is_container_item (current_items[i])) {
            track_ids.add (get_item_id (current_items[i]));
            track_items.add (current_items[i]);
        }
    }

    if (track_ids.length == 0) return;

    var id_array = new string[track_ids.length];
    for (uint i = 0; i < track_ids.length; i++) {
        id_array[i] = track_ids[i];
    }

    library_repo.resolve_batch.begin (
        library_id, id_array, false,
        (obj, res) => {
            var resolved_map = library_repo.resolve_batch.end (res);
            if (resolved_map == null || resolved_map.size () == 0) return;

            var entries = new Json.Array ();
            for (uint i = 0; i < track_ids.length; i++) {
                var iid = track_ids[i];
                var resolved = resolved_map.lookup (iid);
                if (resolved == null) continue;
                var browse_item = track_items[i];
                var entry = build_queue_entry_from_batch_resolved (
                    iid, browse_item, resolved);
                if (entry != null) {
                    entries.add_object_element (entry);
                }
            }

            if (entries.get_length () == 0) return;

            lease_mgr.ensure_lease.begin (renderer_id, (lobj, lres) => {
                var lease = lease_mgr.ensure_lease.end (lres);
                if (lease == null) return;

                var add_body = QueueBodies.add ("end", entries);
                correlator.send_fire_and_forget (
                    renderer_id, "queue.add", add_body, lease);
            });
        }
    );
}
```

- [ ] **Step 4: Build and confirm no warnings/errors**

Run:
```bash
cd integrations/desktop_app
ninja -C builddir 2>&1 | grep -E "(warning|error)" | grep -v "deprecated since"
```

Expected: no new warnings; no errors. If a warning surfaces about an existing caller of `resolve_batch` that wasn't updated, search for it:

```bash
grep -rn "resolve_batch" src/ui/ src/repositories/
```

The only callers should be `on_play_all`, `on_queue_all` (now id-keyed), and `do_view_playlist` (which doesn't use the return value at all — it calls for cache side-effects, so it's already compatible).

- [ ] **Step 5: Commit**

```bash
cd integrations/desktop_app
git add src/repositories/library_repository.vala src/ui/library_view.vala
git commit -m "$(cat <<'EOF'
fix(desktop): key resolve_batch results by itemId

resolve_batch now returns HashTable<string, Json.Object> keyed by
itemId so on_play_all/on_queue_all can pair each request id with
its actual resolved entry. The previous positional GenericArray
silently mis-paired metadata and source URLs whenever the library
skipped, reordered, or partially failed items in a batch.

Mirrors Android LibraryRepository.resolveBatch's localToFull
mapping (LibraryRepository.kt:217).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Replace split_lib_ref with NodeRepository-driven matcher

**Why:** `split_lib_ref` (`library_view.vala:1443-1456`) hardcodes a 5-segment library node id. Real ids come from MQTT presence and may not have exactly 5 segments — e.g. an extra namespace or a shorter provider id breaks the split, and any cross-library playlist entry winds up with the wrong library id and a malformed raw item id. Android avoids this entirely: it walks `nodeRepository.libraries`, finds the one whose `nodeId + ":"` is a prefix of the stripped reference, and only then peels off the prefix (`LibraryRepository.kt:398-422`). Port that.

**Files:**
- Modify: `integrations/desktop_app/src/ui/library_view.vala:1438-1456` (replace split_lib_ref with two helpers)
- Modify: `integrations/desktop_app/src/ui/library_view.vala:1335-1385` (use the new helpers in do_view_playlist)

- [ ] **Step 1: Replace split_lib_ref with find_library_for_item + strip_lib_prefix**

Delete `split_lib_ref` (lines 1438-1456) and replace it with:

```vala
/**
 * Find the library node id for a `lib:` reference by walking the
 * known libraries and matching the longest node-id prefix. Returns
 * null if no library matches and there are no libraries to fall
 * back to. Mirrors Android LibraryRepository.findLibraryForItem
 * (LibraryRepository.kt:398).
 */
private string? find_library_for_item (string item_id) {
    var libraries = node_repo.get_libraries ();
    if (!item_id.has_prefix ("lib:")) {
        return libraries.length > 0 ? libraries[0].node_id : null;
    }
    var stripped = item_id.substring (4); // strip "lib:"
    for (uint i = 0; i < libraries.length; i++) {
        var node_id = libraries[i].node_id;
        if (stripped.has_prefix (node_id + ":")) {
            return node_id;
        }
    }
    return libraries.length > 0 ? libraries[0].node_id : null;
}

/**
 * Strip the `lib:{libraryNodeId}:` prefix from a reference, leaving
 * the local item id the library expects. Returns the input unchanged
 * if no prefix matches. Mirrors Android stripLibPrefix
 * (LibraryRepository.kt:415).
 */
private string strip_lib_prefix (string item_id, string library_node_id) {
    var prefix = "lib:" + library_node_id + ":";
    if (item_id.has_prefix (prefix)) {
        return item_id.substring (prefix.length);
    }
    return item_id;
}
```

- [ ] **Step 2: Update do_view_playlist to use the new helpers**

Replace the body of `do_view_playlist` (around lines 1335-1385) with:

```vala
private async void do_view_playlist (string server_id, string playlist_id) {
    var entries = yield playlist_repo.get_playlist (server_id, playlist_id);

    playlist_spinner.visible = false;
    playlist_spinner.spinning = false;

    if (entries == null || entries.get_length () == 0) return;

    /* Group entries by library node and remember the local-id ↔ full-id mapping
     * so we can merge cached metadata back onto the original entries. */
    var full_to_local = new HashTable<string, string> (str_hash, str_equal);
    var lib_to_local_ids = new HashTable<string, GenericArray<string>> (str_hash, str_equal);

    for (uint i = 0; i < entries.get_length (); i++) {
        var entry = entries.get_object_element (i);
        if (entry.has_member ("metadata") && !entry.get_null_member ("metadata")) continue;

        var full_id = get_playlist_entry_item_id (entry);
        if (full_id.length == 0) continue;

        var lib_id = find_library_for_item (full_id);
        if (lib_id == null) continue;

        var local_id = strip_lib_prefix (full_id, lib_id);
        full_to_local.insert (full_id, local_id);

        var items = lib_to_local_ids.lookup (lib_id);
        if (items == null) {
            items = new GenericArray<string> ();
            lib_to_local_ids.insert (lib_id, items);
        }
        items.add (local_id);
    }

    /* Resolve metadata per library (for the cache side-effect). */
    var lib_keys = new GenericArray<string> ();
    lib_to_local_ids.foreach ((k, v) => { lib_keys.add (k); });

    for (uint li = 0; li < lib_keys.length; li++) {
        var lib_id = lib_keys[li];
        var local_ids = lib_to_local_ids.lookup (lib_id);
        if (local_ids == null || local_ids.length == 0) continue;

        var id_array = new string[local_ids.length];
        for (uint k = 0; k < local_ids.length; k++) {
            id_array[k] = local_ids[k];
        }
        yield library_repo.resolve_batch (lib_id, id_array, true);
    }

    /* Build track rows with resolved metadata */
    populate_playlist_track_rows (entries, full_to_local);
}
```

Note: the `populate_playlist_track_rows` signature stays as `HashTable<string, string>?` — only the parameter name's *meaning* changes (full → local-id instead of full → raw). The lookup logic in `populate_playlist_track_rows` (lines ~1399-1416) already tries the mapped id first then falls back to the full id, so it remains correct without further edits.

- [ ] **Step 3: Build and confirm no warnings/errors**

Run:
```bash
cd integrations/desktop_app
ninja -C builddir 2>&1 | grep -E "(warning|error)" | grep -v "deprecated since"
```

Expected: no new warnings; no errors. The previously-flagged `Method 'Mu.LibraryView.get_first_library_id' never used` warning is unrelated to this task and may still be present — leave it alone.

- [ ] **Step 4: Verify split_lib_ref is gone**

Run:
```bash
grep -n "split_lib_ref" integrations/desktop_app/src/ui/library_view.vala
```

Expected: no matches. If any remain, the old method or a stale caller wasn't removed.

- [ ] **Step 5: Commit**

```bash
cd integrations/desktop_app
git add src/ui/library_view.vala
git commit -m "$(cat <<'EOF'
fix(desktop): match lib: refs against known library nodes

split_lib_ref hardcoded a 5-segment library node id, so any
playlist reference whose library node had a different number of
segments was split incorrectly and resolved against the wrong
library with a malformed local id. Replace it with
find_library_for_item / strip_lib_prefix, which walk the live
NodeRepository.get_libraries() set and peel off the longest
matching nodeId prefix.

Mirrors Android LibraryRepository.findLibraryForItem and
stripLibPrefix (LibraryRepository.kt:398-422).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Final verification

- [ ] **Step 1: Clean rebuild and audit warnings**

```bash
cd integrations/desktop_app
ninja -C builddir clean
ninja -C builddir 2>&1 | tee /tmp/desktop-build.log | tail -20
grep -E "(warning|error)" /tmp/desktop-build.log | grep -v "deprecated since" | grep -v "atomic_load" | grep -v "defined but not used"
```

Expected: no `copying delegates is not supported` warnings, no errors. Pre-existing GTK 4.10 deprecation, the GLib `atomic_load` qualifier warning, and the `*_properties defined but not used` notes are unrelated and acceptable.

- [ ] **Step 2: Smoke-test artwork loading manually (optional but recommended)**

Run the desktop app, browse a library that returns artwork URLs, and scroll the list rapidly so rows are built and torn down quickly. Pre-fix this would frequently segfault under valgrind or AddressSanitizer; post-fix it should not. If valgrind is available:

```bash
cd integrations/desktop_app
G_SLICE=always-malloc G_DEBUG=gc-friendly valgrind --tool=memcheck \
    --leak-check=no ./builddir/media-utopia 2>&1 | grep -A3 "Invalid read\|after free"
```

Expected: no `Invalid read of size N` or `Invalid read of size N (after free)` reports under `mu_artwork_callback_wrapper_invoke`.

- [ ] **Step 3: Confirm git state**

```bash
cd integrations/desktop_app
git log --oneline -6
git status
```

Expected: four new commits on top of the `master` HEAD that was current at plan-write time, and a clean `git status` (other than the pre-existing untracked files in the repo).
