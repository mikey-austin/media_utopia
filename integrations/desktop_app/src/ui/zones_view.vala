/* zones_view.vala — Mu.ZonesView : Adw.Bin
 * Shows discovered zone nodes with per-zone volume, mute, and source controls.
 */

namespace Mu {

    public class ZonesView : Adw.Bin {

        /* Service dependencies */
        private NodeRepository node_repo;
        private RendererStateRepository state_repo;
        private CommandCorrelator correlator;

        /* Layout widgets */
        private Gtk.Label count_label;
        private Gtk.Box zone_list_box;
        private Gtk.Label empty_label;

        /* Signal handler IDs for cleanup */
        private ulong node_added_id = 0;
        private ulong node_removed_id = 0;
        private ulong node_updated_id = 0;
        private ulong state_changed_id = 0;

        /* Track zone cards by node_id */
        private HashTable<string, Gtk.Box> card_map;

        /* Volume debounce timers per zone */
        private HashTable<string, uint> volume_timers;

        /* Flag to suppress slider signal during programmatic updates */
        private bool updating_slider = false;

        public ZonesView (NodeRepository node_repo,
                          RendererStateRepository state_repo,
                          CommandCorrelator correlator) {
            this.node_repo = node_repo;
            this.state_repo = state_repo;
            this.correlator = correlator;

            card_map = new HashTable<string, Gtk.Box> (str_hash, str_equal);
            volume_timers = new HashTable<string, uint> (str_hash, str_equal);

            build_ui ();
            connect_signals ();
            populate_initial ();
        }

        private void build_ui () {
            var scroll = new Gtk.ScrolledWindow ();
            scroll.hscrollbar_policy = Gtk.PolicyType.NEVER;
            scroll.vexpand = true;
            scroll.hexpand = true;

            var outer = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);
            outer.margin_start = 24;
            outer.margin_end = 24;
            outer.margin_top = 24;
            outer.margin_bottom = 24;

            /* --- Header --- */
            var header_box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 12);
            header_box.halign = Gtk.Align.START;
            header_box.margin_bottom = 20;

            var title = new Gtk.Label ("Zones");
            title.add_css_class ("section-title");
            title.halign = Gtk.Align.START;
            header_box.append (title);

            count_label = new Gtk.Label ("0");
            count_label.add_css_class ("badge");
            count_label.valign = Gtk.Align.CENTER;
            header_box.append (count_label);

            outer.append (header_box);

            /* --- Empty state --- */
            empty_label = new Gtk.Label ("No zones discovered yet");
            empty_label.add_css_class ("meta-label");
            empty_label.halign = Gtk.Align.START;
            empty_label.margin_bottom = 16;
            outer.append (empty_label);

            /* --- Zone card list --- */
            zone_list_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 8);
            outer.append (zone_list_box);

            scroll.child = outer;
            this.child = scroll;
        }

        private void connect_signals () {
            node_added_id = node_repo.node_added.connect (on_node_added);
            node_removed_id = node_repo.node_removed.connect (on_node_removed);
            node_updated_id = node_repo.node_updated.connect (on_node_updated);
            state_changed_id = state_repo.state_changed.connect (on_state_changed);
        }

        private void populate_initial () {
            var zones = node_repo.get_zones ();
            for (uint i = 0; i < zones.length; i++) {
                add_zone_card (zones[i]);
            }
            update_count ();
        }

        /* ---- Signal handlers ---- */

        private void on_node_added (Presence presence) {
            if (presence.kind != "zone") return;
            if (card_map.contains (presence.node_id)) return;
            add_zone_card (presence);
            update_count ();
        }

        private void on_node_removed (string node_id) {
            var card = card_map.lookup (node_id);
            if (card != null) {
                zone_list_box.remove (card);
                card_map.remove (node_id);

                /* Cancel any pending volume timer */
                var timer = volume_timers.lookup (node_id);
                if (timer != 0) {
                    Source.remove (timer);
                    volume_timers.remove (node_id);
                }

                update_count ();
            }
        }

        private void on_node_updated (Presence presence) {
            if (presence.kind != "zone") return;
            if (!card_map.contains (presence.node_id)) {
                add_zone_card (presence);
                update_count ();
                return;
            }
            update_zone_card (presence);
        }

        private void on_state_changed (string node_id, RendererState state) {
            if (!card_map.contains (node_id)) return;
            update_zone_state (node_id, state);
        }

        /* ---- Card management ---- */

        private void add_zone_card (Presence presence) {
            var card = build_zone_card (presence);
            zone_list_box.append (card);
            card_map.set (presence.node_id, card);
            empty_label.visible = false;
        }

        private Gtk.Box build_zone_card (Presence presence) {
            var card = new Gtk.Box (Gtk.Orientation.VERTICAL, 10);
            card.add_css_class ("mu-card");
            /* Store node_id via widget name for lookup */
            card.name = presence.node_id;

            /* --- Top row: zone name + source label --- */
            var top_row = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);

            var name_label = new Gtk.Label (presence.name);
            name_label.add_css_class ("renderer-name");
            name_label.halign = Gtk.Align.START;
            name_label.hexpand = true;
            name_label.ellipsize = Pango.EllipsizeMode.END;
            top_row.append (name_label);

            var source_label = new Gtk.Label (get_current_source_name (presence));
            source_label.add_css_class ("badge-renderer");
            source_label.halign = Gtk.Align.END;
            source_label.valign = Gtk.Align.CENTER;
            source_label.ellipsize = Pango.EllipsizeMode.END;
            source_label.max_width_chars = 20;
            top_row.append (source_label);

            card.append (top_row);

            /* --- Volume row: mute button + volume slider + percentage --- */
            var vol_row = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);
            vol_row.valign = Gtk.Align.CENTER;

            var mute_btn = new Gtk.Button ();
            mute_btn.add_css_class ("flat");
            mute_btn.valign = Gtk.Align.CENTER;
            var initial_muted = get_zone_muted (presence.node_id);
            mute_btn.icon_name = initial_muted
                ? "audio-volume-muted-symbolic" : "audio-volume-high-symbolic";
            mute_btn.tooltip_text = initial_muted ? "Unmute" : "Mute";
            mute_btn.clicked.connect (() => {
                var current_muted = get_zone_muted (presence.node_id);
                send_mute_command (presence.node_id, !current_muted);
            });
            vol_row.append (mute_btn);

            var vol_scale = new Gtk.Scale.with_range (
                Gtk.Orientation.HORIZONTAL, 0.0, 100.0, 1.0);
            vol_scale.add_css_class ("volume-scale");
            vol_scale.hexpand = true;
            vol_scale.draw_value = false;
            vol_scale.set_value (get_zone_volume (presence.node_id) * 100.0);
            vol_scale.value_changed.connect (() => {
                if (updating_slider) return;
                debounce_volume (presence.node_id, vol_scale.get_value () / 100.0);
            });
            vol_row.append (vol_scale);

            var pct_label = new Gtk.Label (
                "%d%%".printf ((int) (get_zone_volume (presence.node_id) * 100.0)));
            pct_label.add_css_class ("text-secondary");
            pct_label.width_chars = 4;
            pct_label.xalign = 1.0f;
            vol_row.append (pct_label);

            card.append (vol_row);

            /* --- Source selector row (only if sources available) --- */
            if (presence.sources != null && presence.sources.length > 0) {
                var source_row = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);
                source_row.valign = Gtk.Align.CENTER;

                var source_icon = new Gtk.Image.from_icon_name ("audio-input-line-symbolic");
                source_icon.add_css_class ("text-secondary");
                source_icon.pixel_size = 16;
                source_row.append (source_icon);

                var src_label = new Gtk.Label ("Source");
                src_label.add_css_class ("text-secondary");
                src_label.halign = Gtk.Align.START;
                source_row.append (src_label);

                var source_dropdown = build_source_dropdown (presence);
                source_dropdown.hexpand = true;
                source_dropdown.halign = Gtk.Align.END;
                source_row.append (source_dropdown);

                card.append (source_row);
            }

            return card;
        }

        private Gtk.DropDown build_source_dropdown (Presence presence) {
            var string_list = new Gtk.StringList (null);
            int active_idx = 0;
            var current_source = presence.source ?? "";

            for (uint i = 0; i < presence.sources.length; i++) {
                var src = presence.sources[i];
                string_list.append (src.name);
                if (src.id == current_source) {
                    active_idx = (int) i;
                }
            }

            var dropdown = new Gtk.DropDown (string_list, null);
            dropdown.selected = active_idx;

            /* Store sources reference for lookup in handler */
            var sources_ref = presence.sources;
            var node_id = presence.node_id;

            dropdown.notify["selected"].connect (() => {
                var idx = dropdown.selected;
                if (idx < sources_ref.length) {
                    var source_id = sources_ref[idx].id;
                    send_source_command (node_id, source_id);
                }
            });

            return dropdown;
        }

        private void update_zone_card (Presence presence) {
            var card = card_map.lookup (presence.node_id);
            if (card == null) return;

            /* Find name label (first child -> top_row -> first child) */
            var top_row = card.get_first_child () as Gtk.Box;
            if (top_row == null) return;

            var name_label = top_row.get_first_child () as Gtk.Label;
            if (name_label != null) {
                name_label.label = presence.name;
            }

            /* Update source label */
            var source_label = name_label != null
                ? name_label.get_next_sibling () as Gtk.Label : null;
            if (source_label != null) {
                source_label.label = get_current_source_name (presence);
            }

            /* Update source dropdown selection if sources changed */
            update_source_dropdown (card, presence);
        }

        private void update_zone_state (string node_id, RendererState state) {
            var card = card_map.lookup (node_id);
            if (card == null) return;

            /* Navigate to volume row (second child of card) */
            var top_row = card.get_first_child ();
            if (top_row == null) return;

            var vol_row = top_row.get_next_sibling () as Gtk.Box;
            if (vol_row == null) return;

            double volume = 1.0;
            bool muted = false;
            if (state.playback != null) {
                volume = state.playback.volume;
                muted = state.playback.mute;
            }

            /* Mute button (first child of vol_row) */
            var mute_btn = vol_row.get_first_child () as Gtk.Button;
            if (mute_btn != null) {
                mute_btn.icon_name = muted
                    ? "audio-volume-muted-symbolic" : "audio-volume-high-symbolic";
                mute_btn.tooltip_text = muted ? "Unmute" : "Mute";
            }

            /* Volume scale (second child) */
            var vol_scale = mute_btn != null
                ? mute_btn.get_next_sibling () as Gtk.Scale : null;
            if (vol_scale != null) {
                updating_slider = true;
                vol_scale.set_value (volume * 100.0);
                updating_slider = false;
            }

            /* Percentage label (third child) */
            var pct_label = vol_scale != null
                ? vol_scale.get_next_sibling () as Gtk.Label : null;
            if (pct_label != null) {
                pct_label.label = "%d%%".printf ((int) (volume * 100.0));
            }
        }

        private void update_source_dropdown (Gtk.Box card, Presence presence) {
            if (presence.sources == null || presence.sources.length == 0) return;

            /* Source row is the third child of card (after top_row and vol_row) */
            var top_row = card.get_first_child ();
            if (top_row == null) return;
            var vol_row = top_row.get_next_sibling ();
            if (vol_row == null) return;
            var source_row = vol_row.get_next_sibling () as Gtk.Box;
            if (source_row == null) return;

            /* The dropdown is the last child of source_row */
            Gtk.Widget? child = source_row.get_first_child ();
            Gtk.DropDown? dropdown = null;
            while (child != null) {
                if (child is Gtk.DropDown) {
                    dropdown = child as Gtk.DropDown;
                }
                child = child.get_next_sibling ();
            }

            if (dropdown == null) return;

            var current_source = presence.source ?? "";
            for (uint i = 0; i < presence.sources.length; i++) {
                if (presence.sources[i].id == current_source) {
                    if (dropdown.selected != i) {
                        dropdown.selected = i;
                    }
                    break;
                }
            }
        }

        /* ---- Commands ---- */

        private void send_volume_command (string node_id, double volume) {
            var body = new Json.Object ();
            body.set_double_member ("volume", volume);
            correlator.send_fire_and_forget (node_id, "zone.setVolume", body);
        }

        private void send_mute_command (string node_id, bool mute) {
            var body = new Json.Object ();
            body.set_boolean_member ("mute", mute);
            correlator.send_fire_and_forget (node_id, "zone.setMute", body);
        }

        private void send_source_command (string node_id, string source_id) {
            var body = new Json.Object ();
            body.set_string_member ("sourceId", source_id);
            correlator.send_fire_and_forget (node_id, "zone.setSource", body);
        }

        /* ---- Debounced volume ---- */

        private void debounce_volume (string node_id, double volume) {
            /* Cancel any pending timer for this zone */
            var existing = volume_timers.lookup (node_id);
            if (existing != 0) {
                Source.remove (existing);
            }

            /* Update percentage label immediately for responsiveness */
            var card = card_map.lookup (node_id);
            if (card != null) {
                var top_row = card.get_first_child ();
                if (top_row != null) {
                    var vol_row = top_row.get_next_sibling () as Gtk.Box;
                    if (vol_row != null) {
                        var mute_btn = vol_row.get_first_child ();
                        var vol_scale = mute_btn != null ? mute_btn.get_next_sibling () : null;
                        var pct_label = vol_scale != null
                            ? vol_scale.get_next_sibling () as Gtk.Label : null;
                        if (pct_label != null) {
                            pct_label.label = "%d%%".printf ((int) (volume * 100.0));
                        }
                    }
                }
            }

            var timer_id = Timeout.add (100, () => {
                volume_timers.remove (node_id);
                send_volume_command (node_id, volume);
                return Source.REMOVE;
            });
            volume_timers.set (node_id, timer_id);
        }

        /* ---- Helpers ---- */

        private void update_count () {
            var zones = node_repo.get_zones ();
            count_label.label = "%u".printf (zones.length);
            empty_label.visible = (zones.length == 0);
        }

        private string get_current_source_name (Presence presence) {
            if (presence.source == null || presence.sources == null) return "";

            for (uint i = 0; i < presence.sources.length; i++) {
                if (presence.sources[i].id == presence.source) {
                    return presence.sources[i].name;
                }
            }
            return presence.source;
        }

        private double get_zone_volume (string node_id) {
            var state = state_repo.get_state (node_id);
            if (state != null && state.playback != null) {
                return state.playback.volume;
            }
            return 1.0;
        }

        private bool get_zone_muted (string node_id) {
            var state = state_repo.get_state (node_id);
            if (state != null && state.playback != null) {
                return state.playback.mute;
            }
            return false;
        }
    }
}
