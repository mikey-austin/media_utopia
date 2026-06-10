/* queue_view.vala — Mu.QueueView : Adw.Bin
 * Queue management view with track list, reordering, removal, jump-to-play,
 * and shuffle/repeat controls.
 */

namespace Mu {

    public class QueueView : Adw.Bin {

        /* Service dependencies */
        private RendererStateRepository state_repo;
        private ActiveRendererRepository active_repo;
        private CommandCorrelator correlator;
        private LeaseManager lease_mgr;
        private ArtworkLoader artwork_loader;

        /* Header widgets */
        private Gtk.Label track_count_label;
        private Gtk.Button shuffle_button;
        private Gtk.Button repeat_button;
        private Gtk.Button clear_button;
        private Gtk.Box lease_banner;
        private Gtk.Label lease_banner_label;
        private bool lease_blocked = false;

        /* Queue list */
        private Gtk.ListBox queue_list;
        private Gtk.Box empty_box;
        private Gtk.ScrolledWindow scroll;

        /* Cached queue data */
        private GenericArray<QueueEntry> items;
        private int64 current_index = -1;
        private int64 last_revision = -1;
        private bool shuffle_on = false;
        private string repeat_mode = "off";

        /* Signal handler IDs */
        private ulong state_changed_id = 0;
        private ulong active_changed_id = 0;

        /* Drag state */
        private int drag_source_index = -1;

        public QueueView (RendererStateRepository state_repo,
                          ActiveRendererRepository active_repo,
                          CommandCorrelator correlator,
                          LeaseManager lease_mgr,
                          ArtworkLoader artwork_loader) {
            this.state_repo = state_repo;
            this.active_repo = active_repo;
            this.correlator = correlator;
            this.lease_mgr = lease_mgr;
            this.artwork_loader = artwork_loader;
            this.items = new GenericArray<QueueEntry> ();

            build_ui ();
            connect_signals ();

            /* Load initial queue if renderer is already active */
            fetch_queue ();
        }

        private void build_ui () {
            scroll = new Gtk.ScrolledWindow ();
            scroll.hscrollbar_policy = Gtk.PolicyType.NEVER;
            scroll.vexpand = true;
            scroll.hexpand = true;

            var outer = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);
            outer.margin_start = 24;
            outer.margin_end = 24;
            outer.margin_top = 24;
            outer.margin_bottom = 24;

            /* --- Header --- */
            var title = new Gtk.Label ("Queue");
            title.add_css_class ("section-title");
            title.halign = Gtk.Align.START;
            outer.append (title);

            track_count_label = new Gtk.Label ("");
            track_count_label.add_css_class ("meta-label");
            track_count_label.halign = Gtk.Align.START;
            track_count_label.margin_top = 2;
            outer.append (track_count_label);

            /* Action buttons row */
            var actions = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);
            actions.margin_top = 16;
            actions.margin_bottom = 16;
            actions.halign = Gtk.Align.START;

            shuffle_button = new Gtk.Button ();
            shuffle_button.add_css_class ("queue-toggle-button");
            update_shuffle_button ();
            shuffle_button.clicked.connect (on_shuffle_clicked);
            actions.append (shuffle_button);

            repeat_button = new Gtk.Button ();
            repeat_button.add_css_class ("queue-toggle-button");
            update_repeat_button ();
            repeat_button.clicked.connect (on_repeat_clicked);
            actions.append (repeat_button);

            clear_button = new Gtk.Button.with_label ("Clear");
            clear_button.add_css_class ("release-button");
            clear_button.clicked.connect (on_clear_clicked);
            actions.append (clear_button);

            outer.append (actions);

            /* Foreign-lease banner (queue becomes read-only) */
            lease_banner = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);
            lease_banner.add_css_class ("lease-banner");
            lease_banner.margin_bottom = 12;
            lease_banner.visible = false;

            lease_banner_label = new Gtk.Label ("");
            lease_banner_label.halign = Gtk.Align.START;
            lease_banner_label.hexpand = true;
            lease_banner_label.wrap = true;
            lease_banner.append (lease_banner_label);

            outer.append (lease_banner);

            /* --- Empty state --- */
            empty_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 12);
            empty_box.halign = Gtk.Align.CENTER;
            empty_box.valign = Gtk.Align.CENTER;
            empty_box.vexpand = true;
            empty_box.margin_top = 80;

            var empty_icon = new Gtk.Image.from_icon_name ("view-list-symbolic");
            empty_icon.pixel_size = 48;
            empty_icon.add_css_class ("text-secondary");
            empty_box.append (empty_icon);

            var empty_title = new Gtk.Label ("Queue Empty");
            empty_title.add_css_class ("heading-medium");
            empty_box.append (empty_title);

            var empty_sub = new Gtk.Label ("Add tracks from the library to get started");
            empty_sub.add_css_class ("text-secondary");
            empty_box.append (empty_sub);

            outer.append (empty_box);

            /* --- Queue list --- */
            queue_list = new Gtk.ListBox ();
            queue_list.selection_mode = Gtk.SelectionMode.NONE;
            queue_list.add_css_class ("queue-list");
            queue_list.row_activated.connect (on_row_activated);

            /* Delete on the keyboard-focused row */
            var key_controller = new Gtk.EventControllerKey ();
            key_controller.key_pressed.connect ((keyval, keycode, modifiers) => {
                if (keyval != Gdk.Key.Delete && keyval != Gdk.Key.KP_Delete) {
                    return false;
                }
                if (lease_blocked) return false;

                var focused = queue_list.get_focus_child () as Gtk.ListBoxRow;
                if (focused == null) return false;

                var index = int.parse (focused.name);
                if (index < 0 || index >= (int) items.length) return false;

                on_remove_track (index, items[index]);
                return true;
            });
            queue_list.add_controller (key_controller);

            outer.append (queue_list);

            scroll.child = outer;
            this.child = scroll;
        }

        private void connect_signals () {
            state_changed_id = state_repo.state_changed.connect ((node_id, state) => {
                if (node_id == active_repo.active_renderer_id) {
                    on_state_updated (state);
                }
            });

            active_changed_id = active_repo.active_renderer_changed.connect ((node_id) => {
                last_revision = -1;
                fetch_queue ();
            });
        }

        public override void dispose () {
            if (state_changed_id != 0) {
                state_repo.disconnect (state_changed_id);
                state_changed_id = 0;
            }
            if (active_changed_id != 0) {
                active_repo.disconnect (active_changed_id);
                active_changed_id = 0;
            }
            base.dispose ();
        }

        /* ---- State observation ---- */

        private void on_state_updated (RendererState state) {
            update_lease_blocked (state);

            if (state.queue == null) return;

            var qs = state.queue;

            /* Update shuffle/repeat from state */
            shuffle_on = qs.shuffle;
            repeat_mode = qs.repeat_mode;
            update_shuffle_button ();
            update_repeat_button ();

            /* If current index changed, update highlight */
            if (qs.index != current_index) {
                var old_index = current_index;
                current_index = qs.index;
                update_row_highlight (old_index);
                update_row_highlight (current_index);
            }

            /* If revision changed, re-fetch queue */
            if (qs.revision != last_revision) {
                fetch_queue ();
            }
        }

        /* ---- Data fetching ---- */

        private void fetch_queue () {
            var renderer_id = active_repo.active_renderer_id;
            if (renderer_id.length == 0) {
                clear_list ();
                return;
            }

            correlator.send.begin (
                renderer_id,
                "queue.get",
                QueueBodies.get (0, 200),
                null, -1, 3000,
                (obj, res) => {
                    var reply = correlator.send.end (res);
                    if (reply == null || !reply.ok || reply.body == null) return;

                    var queue_reply = QueueGetReply.from_json (reply.body);
                    last_revision = queue_reply.revision;
                    current_index = queue_reply.index;
                    items = queue_reply.entries;

                    rebuild_list ();
                }
            );
        }

        /* ---- List management ---- */

        private void rebuild_list () {
            /* Remove all existing rows */
            Gtk.Widget? child = queue_list.get_first_child ();
            while (child != null) {
                var next = child.get_next_sibling ();
                queue_list.remove (child);
                child = next;
            }

            if (items.length == 0) {
                empty_box.visible = true;
                queue_list.visible = false;
                track_count_label.label = "";
                return;
            }

            empty_box.visible = false;
            queue_list.visible = true;

            /* Update header count */
            update_track_count_label ();

            /* Add rows */
            for (uint i = 0; i < items.length; i++) {
                var row = build_queue_row ((int) i, items[i]);
                queue_list.append (row);
            }
        }

        private void clear_list () {
            items = new GenericArray<QueueEntry> ();
            current_index = -1;
            last_revision = -1;
            rebuild_list ();
        }

        private void update_track_count_label () {
            if (items.length == 0) {
                track_count_label.label = "";
                return;
            }

            /* Calculate total duration from display if available */
            int64 total_ms = 0;
            bool has_duration = false;
            for (uint i = 0; i < items.length; i++) {
                var display = items[i].display;
                if (display != null && display.duration_ms > 0) {
                    total_ms += display.duration_ms;
                    has_duration = true;
                }
            }

            var count_text = "%u TRACK%s".printf (items.length,
                items.length == 1 ? "" : "S");

            if (has_duration && total_ms > 0) {
                track_count_label.label = "%s \u00B7 %s".printf (count_text,
                    format_duration (total_ms));
            } else {
                track_count_label.label = count_text;
            }
        }

        private Gtk.ListBoxRow build_queue_row (int index, QueueEntry item) {
            var row = new Gtk.ListBoxRow ();
            row.name = index.to_string ();
            row.focusable = true;
            row.activatable = true;

            var card = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 12);
            card.add_css_class ("mu-card");
            card.add_css_class ("queue-row");
            card.margin_top = 2;
            card.margin_bottom = 2;

            /* Active highlight */
            if (index == (int) current_index) {
                card.add_css_class ("active");
            }

            /* Drag handle */
            var drag_handle = new Gtk.Image.from_icon_name ("list-drag-handle-symbolic");
            drag_handle.pixel_size = 16;
            drag_handle.add_css_class ("queue-drag-handle");
            drag_handle.valign = Gtk.Align.CENTER;
            drag_handle.visible = !lease_blocked;
            card.append (drag_handle);

            /* Index number */
            var index_label = new Gtk.Label ("%02d".printf (index + 1));
            index_label.add_css_class ("queue-index");
            index_label.width_request = 28;
            index_label.halign = Gtk.Align.CENTER;
            if (index == (int) current_index) {
                index_label.add_css_class ("track-active");
            } else {
                index_label.add_css_class ("text-secondary");
            }
            card.append (index_label);

            /* Artwork */
            var art = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);
            art.add_css_class ("queue-art");
            art.width_request = 40;
            art.height_request = 40;

            var art_icon = new Gtk.Image.from_icon_name ("media-optical-symbolic");
            art_icon.pixel_size = 20;
            art_icon.add_css_class ("text-secondary");
            art_icon.halign = Gtk.Align.CENTER;
            art_icon.valign = Gtk.Align.CENTER;
            art_icon.hexpand = true;
            art_icon.vexpand = true;
            art.append (art_icon);

            var art_picture = new Gtk.Picture ();
            art_picture.content_fit = Gtk.ContentFit.COVER;
            art_picture.width_request = 40;
            art_picture.height_request = 40;
            art_picture.visible = false;
            art.append (art_picture);

            /* Load artwork from display */
            if (item.display != null && item.display.artwork_url.length > 0) {
                var art_url = item.display.artwork_url;
                artwork_loader.load_async (art_url, (texture) => {
                    if (texture != null && art_picture.get_parent () != null) {
                        art_picture.paintable = texture;
                        art_picture.visible = true;
                        art_icon.visible = false;
                    }
                });
            }

            card.append (art);

            /* Text info */
            var text_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 2);
            text_box.hexpand = true;
            text_box.valign = Gtk.Align.CENTER;

            var track_title = get_display_title (item, "Unknown Track");
            var title_label = new Gtk.Label (track_title);
            title_label.add_css_class ("queue-track-title");
            title_label.halign = Gtk.Align.START;
            title_label.ellipsize = Pango.EllipsizeMode.END;
            title_label.max_width_chars = 50;
            if (index == (int) current_index) {
                title_label.add_css_class ("track-active");
            }
            text_box.append (title_label);

            var artist = get_display_artist (item, "Unknown Artist");
            var artist_label = new Gtk.Label (artist);
            artist_label.add_css_class ("queue-track-artist");
            artist_label.halign = Gtk.Align.START;
            artist_label.ellipsize = Pango.EllipsizeMode.END;
            artist_label.max_width_chars = 50;
            text_box.append (artist_label);

            card.append (text_box);

            /* Duration */
            if (item.display != null && item.display.duration_ms > 0) {
                var dur_label = new Gtk.Label (format_duration (item.display.duration_ms));
                dur_label.add_css_class ("text-timestamp");
                dur_label.valign = Gtk.Align.CENTER;
                card.append (dur_label);
            }

            /* Delete button */
            var delete_btn = new Gtk.Button.from_icon_name ("edit-delete-symbolic");
            delete_btn.add_css_class ("queue-delete-button");
            delete_btn.valign = Gtk.Align.CENTER;
            delete_btn.tooltip_text = "Remove from queue";
            delete_btn.visible = !lease_blocked;
            delete_btn.clicked.connect (() => {
                on_remove_track (index, item);
            });
            card.append (delete_btn);

            row.child = card;

            /* Drag-and-drop: source */
            var drag_source = new Gtk.DragSource ();
            drag_source.actions = Gdk.DragAction.MOVE;
            drag_source.prepare.connect ((src, x, y) => {
                if (lease_blocked) return null;
                drag_source_index = index;
                var val = Value (typeof (string));
                val.set_string (index.to_string ());
                return new Gdk.ContentProvider.for_value (val);
            });
            drag_source.drag_begin.connect ((src, drag) => {
                /* Create a visual drag icon from the row */
                var paintable = new Gtk.WidgetPaintable (row);
                src.set_icon (paintable, 0, 0);
            });
            row.add_controller (drag_source);

            /* Drag-and-drop: target */
            var drop_target = new Gtk.DropTarget (typeof (string), Gdk.DragAction.MOVE);
            drop_target.drop.connect ((target, val, x, y) => {
                if (lease_blocked) return false;
                var from_str = val.get_string ();
                if (from_str == null) return false;

                var from_idx = int.parse (from_str);
                var to_idx = index;

                if (from_idx == to_idx) return false;

                on_move_track (from_idx, to_idx);
                return true;
            });
            row.add_controller (drop_target);

            return row;
        }

        private void update_row_highlight (int64 index) {
            if (index < 0) return;

            var row = queue_list.get_row_at_index ((int) index);
            if (row == null) return;

            var card = row.child as Gtk.Box;
            if (card == null) return;

            var is_active = (index == current_index);

            if (is_active) {
                card.add_css_class ("active");
            } else {
                card.remove_css_class ("active");
            }

            /* Update index label and title styling */
            var child = card.get_first_child ();

            /* Skip drag handle */
            if (child != null) {
                child = child.get_next_sibling ();
            }

            /* Index label */
            if (child != null) {
                var index_label = child as Gtk.Label;
                if (index_label != null) {
                    if (is_active) {
                        index_label.remove_css_class ("text-secondary");
                        index_label.add_css_class ("track-active");
                    } else {
                        index_label.remove_css_class ("track-active");
                        index_label.add_css_class ("text-secondary");
                    }
                }
                child = child.get_next_sibling ();
            }

            /* Skip artwork placeholder */
            if (child != null) {
                child = child.get_next_sibling ();
            }

            /* Text box */
            if (child != null) {
                var text_box = child as Gtk.Box;
                if (text_box != null) {
                    var title_label = text_box.get_first_child () as Gtk.Label;
                    if (title_label != null) {
                        if (is_active) {
                            title_label.add_css_class ("track-active");
                        } else {
                            title_label.remove_css_class ("track-active");
                        }
                    }
                }
            }
        }

        /* ---- Foreign-lease handling ---- */

        private void update_lease_blocked (RendererState? state) {
            var own_identity = correlator.get_identity ();
            var owner = (state != null && state.session != null)
                ? state.session.owner : "";

            var blocked = owner.length > 0 && owner != own_identity;

            if (blocked) {
                lease_banner_label.label =
                    "Controlled by %s — queue is read-only".printf (owner);
            }

            if (blocked == lease_blocked) return;
            lease_blocked = blocked;

            lease_banner.visible = blocked;
            shuffle_button.sensitive = !blocked;
            repeat_button.sensitive = !blocked;
            clear_button.sensitive = !blocked;

            /* Rebuild rows so handles/delete buttons reflect the new mode */
            rebuild_list ();
        }

        /* ---- Command dispatch ---- */

        private void send_command (string cmd_type, Json.Object body) {
            var renderer_id = active_repo.active_renderer_id;
            if (renderer_id.length == 0) return;

            lease_mgr.ensure_lease.begin (renderer_id, (obj, res) => {
                var lease = lease_mgr.ensure_lease.end (res);
                if (lease != null) {
                    correlator.send_fire_and_forget (renderer_id, cmd_type, body, lease);
                }
            });
        }

        private void on_row_activated (Gtk.ListBoxRow row) {
            var index_str = row.name;
            if (index_str == null || index_str.length == 0) return;

            var index = int64.parse (index_str);
            var renderer_id = active_repo.active_renderer_id;
            if (renderer_id.length == 0) return;

            /* Jump to the track, then play */
            lease_mgr.ensure_lease.begin (renderer_id, (obj, res) => {
                var lease = lease_mgr.ensure_lease.end (res);
                if (lease == null) return;

                correlator.send_fire_and_forget (renderer_id, "queue.jump",
                    QueueBodies.jump (index), lease);
                correlator.send_fire_and_forget (renderer_id, "playback.play",
                    PlaybackBodies.play (), lease);
            });
        }

        private void on_remove_track (int index, QueueEntry item) {
            /* Optimistic: remove from local list immediately */
            if (index >= 0 && index < (int) items.length) {
                items.remove_index ((uint) index);
                rebuild_list ();
            }

            /* Send command */
            if (item.queue_entry_id.length > 0) {
                send_command ("queue.remove", QueueBodies.remove (item.queue_entry_id));
            } else {
                send_command ("queue.remove", QueueBodies.remove ("", (int64) index));
            }
        }

        private void on_move_track (int from_index, int to_index) {
            /* Optimistic local reorder so the row lands immediately */
            if (from_index >= 0 && from_index < (int) items.length &&
                to_index >= 0 && to_index < (int) items.length) {
                var moved = items[from_index];
                items.remove_index ((uint) from_index);
                items.insert (to_index, moved);
                if (current_index == from_index) {
                    current_index = to_index;
                } else if (from_index < current_index && to_index >= current_index) {
                    current_index--;
                } else if (from_index > current_index && to_index <= current_index) {
                    current_index++;
                }
                rebuild_list ();
            }

            send_command ("queue.move", QueueBodies.move ((int64) from_index, (int64) to_index));

            /* Re-fetch to sync with the renderer's authoritative order */
            Timeout.add (300, () => {
                fetch_queue ();
                return Source.REMOVE;
            });
        }

        private void on_shuffle_clicked () {
            shuffle_on = !shuffle_on;
            update_shuffle_button ();
            send_command ("queue.setShuffle", QueueBodies.set_shuffle (shuffle_on));
        }

        private void on_repeat_clicked () {
            /* Cycle: off -> all -> one -> off */
            switch (repeat_mode) {
                case "off":
                    repeat_mode = "all";
                    break;
                case "all":
                    repeat_mode = "one";
                    break;
                default:
                    repeat_mode = "off";
                    break;
            }

            update_repeat_button ();
            var repeat_on = (repeat_mode != "off");
            send_command ("queue.setRepeat", QueueBodies.set_repeat (repeat_on, repeat_mode));
        }

        private void on_clear_clicked () {
            /* Optimistic: clear local list immediately */
            clear_list ();

            send_command ("queue.clear", QueueBodies.clear ());
        }

        /* ---- UI helpers ---- */

        private void update_shuffle_button () {
            var child = shuffle_button.child;
            if (child != null) {
                shuffle_button.child = null;
            }

            var box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 6);
            var icon = new Gtk.Image.from_icon_name ("media-playlist-shuffle-symbolic");
            icon.pixel_size = 16;
            box.append (icon);
            var label = new Gtk.Label ("Shuffle");
            label.add_css_class ("heading-small");
            box.append (label);
            shuffle_button.child = box;

            if (shuffle_on) {
                shuffle_button.add_css_class ("queue-toggle-active");
            } else {
                shuffle_button.remove_css_class ("queue-toggle-active");
            }
        }

        private void update_repeat_button () {
            var child = repeat_button.child;
            if (child != null) {
                repeat_button.child = null;
            }

            string icon_name;
            string label_text;
            switch (repeat_mode) {
                case "all":
                    icon_name = "media-playlist-repeat-symbolic";
                    label_text = "Repeat All";
                    break;
                case "one":
                    icon_name = "media-playlist-repeat-song-symbolic";
                    label_text = "Repeat One";
                    break;
                default:
                    icon_name = "media-playlist-repeat-symbolic";
                    label_text = "Repeat";
                    break;
            }

            var box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 6);
            var icon = new Gtk.Image.from_icon_name (icon_name);
            icon.pixel_size = 16;
            box.append (icon);
            var label = new Gtk.Label (label_text);
            label.add_css_class ("heading-small");
            box.append (label);
            repeat_button.child = box;

            if (repeat_mode != "off") {
                repeat_button.add_css_class ("queue-toggle-active");
            } else {
                repeat_button.remove_css_class ("queue-toggle-active");
            }
        }

        private string get_display_title (QueueEntry item, string fallback) {
            if (item.display != null && item.display.title.length > 0) {
                return item.display.title;
            }
            return fallback;
        }

        private string get_display_artist (QueueEntry item, string fallback) {
            if (item.display != null) {
                var artist = item.display.artist_display ();
                if (artist.length > 0) return artist;
            }
            return fallback;
        }

        private string format_duration (int64 total_ms) {
            var total_seconds = (int) (total_ms / 1000);
            var hours = total_seconds / 3600;
            var minutes = (total_seconds % 3600) / 60;
            var seconds = total_seconds % 60;

            if (hours > 0) {
                return "%d:%02d:%02d".printf (hours, minutes, seconds);
            }
            return "%d:%02d".printf (minutes, seconds);
        }
    }
}
