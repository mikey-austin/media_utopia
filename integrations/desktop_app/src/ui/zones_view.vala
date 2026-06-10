/* zones_view.vala — Mu.ZonesView : Adw.Bin
 * Shows discovered zone nodes with per-zone volume, mute, and source controls.
 */

namespace Mu {

    public class ZonesView : Adw.Bin {

        /* Service dependencies */
        private NodeRepository node_repo;
        private ZoneStateRepository zone_state_repo;
        private CommandCorrelator correlator;
        private RendererStateRepository state_repo;
        private ActiveRendererRepository active_repo;
        private LeaseManager lease_mgr;

        /* Layout widgets */
        private Gtk.Label count_label;
        private Gtk.Box zone_list_box;
        private Gtk.Label empty_label;
        private Gtk.Stack zones_tab_stack;
        private Gtk.Box sources_list_box;
        private Gtk.Label sources_empty_label;

        /* Master volume widgets (active renderer) */
        private Gtk.Button master_mute_btn;
        private Gtk.Scale master_scale;
        private Gtk.Label master_pct_label;
        private uint master_volume_timer = 0;
        private bool updating_master = false;

        /* Signal handler IDs for cleanup */
        private ulong node_added_id = 0;
        private ulong node_removed_id = 0;
        private ulong node_updated_id = 0;
        private ulong state_changed_id = 0;
        private ulong renderer_state_id = 0;
        private ulong active_changed_id = 0;

        /* Track zone cards by node_id */
        private HashTable<string, Gtk.Box> card_map;

        /* Volume debounce timers per zone */
        private HashTable<string, uint> volume_timers;

        /* Flags to suppress control signals during programmatic updates */
        private bool updating_slider = false;
        private bool updating_dropdown = false;

        public ZonesView (NodeRepository node_repo,
                          ZoneStateRepository zone_state_repo,
                          CommandCorrelator correlator,
                          RendererStateRepository state_repo,
                          ActiveRendererRepository active_repo,
                          LeaseManager lease_mgr) {
            this.node_repo = node_repo;
            this.zone_state_repo = zone_state_repo;
            this.correlator = correlator;
            this.state_repo = state_repo;
            this.active_repo = active_repo;
            this.lease_mgr = lease_mgr;

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

            /* --- Master volume (active renderer) --- */
            var master_card = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);
            master_card.add_css_class ("mu-card");
            master_card.margin_bottom = 16;
            master_card.valign = Gtk.Align.CENTER;

            var master_label = new Gtk.Label ("Master");
            master_label.add_css_class ("heading-small");
            master_label.margin_end = 4;
            master_card.append (master_label);

            master_mute_btn = new Gtk.Button ();
            master_mute_btn.add_css_class ("flat");
            master_mute_btn.valign = Gtk.Align.CENTER;
            master_mute_btn.icon_name = "audio-volume-high-symbolic";
            master_mute_btn.tooltip_text = "Mute";
            master_mute_btn.clicked.connect (on_master_mute);
            master_card.append (master_mute_btn);

            master_scale = new Gtk.Scale.with_range (
                Gtk.Orientation.HORIZONTAL, 0.0, 100.0, 1.0);
            master_scale.add_css_class ("volume-scale");
            master_scale.hexpand = true;
            master_scale.draw_value = false;
            master_scale.value_changed.connect (() => {
                if (updating_master) return;
                master_pct_label.label = "%d%%".printf ((int) master_scale.get_value ());
                debounce_master_volume (master_scale.get_value () / 100.0);
            });
            master_card.append (master_scale);

            master_pct_label = new Gtk.Label ("100%");
            master_pct_label.add_css_class ("text-secondary");
            master_pct_label.width_chars = 4;
            master_pct_label.xalign = 1.0f;
            master_card.append (master_pct_label);

            outer.append (master_card);

            /* --- ZONES / SOURCES tabs --- */
            var tab_box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 4);
            tab_box.halign = Gtk.Align.START;
            tab_box.margin_bottom = 12;

            var zones_tab_btn = new Gtk.ToggleButton.with_label ("Zones");
            zones_tab_btn.add_css_class ("queue-toggle-button");
            zones_tab_btn.active = true;
            zones_tab_btn.add_css_class ("queue-toggle-active");
            tab_box.append (zones_tab_btn);

            var sources_tab_btn = new Gtk.ToggleButton.with_label ("Sources");
            sources_tab_btn.add_css_class ("queue-toggle-button");
            sources_tab_btn.group = zones_tab_btn;
            tab_box.append (sources_tab_btn);

            zones_tab_btn.toggled.connect (() => {
                if (zones_tab_btn.active) {
                    zones_tab_stack.visible_child_name = "zones";
                    zones_tab_btn.add_css_class ("queue-toggle-active");
                    sources_tab_btn.remove_css_class ("queue-toggle-active");
                }
            });
            sources_tab_btn.toggled.connect (() => {
                if (sources_tab_btn.active) {
                    zones_tab_stack.visible_child_name = "sources";
                    sources_tab_btn.add_css_class ("queue-toggle-active");
                    zones_tab_btn.remove_css_class ("queue-toggle-active");
                    rebuild_sources ();
                }
            });

            outer.append (tab_box);

            zones_tab_stack = new Gtk.Stack ();
            zones_tab_stack.transition_type = Gtk.StackTransitionType.CROSSFADE;
            zones_tab_stack.transition_duration = 150;

            /* Zones tab content */
            var zones_page = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);

            empty_label = new Gtk.Label ("No zones discovered yet");
            empty_label.add_css_class ("meta-label");
            empty_label.halign = Gtk.Align.START;
            empty_label.margin_bottom = 16;
            zones_page.append (empty_label);

            zone_list_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 8);
            zones_page.append (zone_list_box);

            zones_tab_stack.add_named (zones_page, "zones");

            /* Sources tab content */
            var sources_page = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);

            sources_empty_label = new Gtk.Label ("No sources advertised by zone controllers");
            sources_empty_label.add_css_class ("meta-label");
            sources_empty_label.halign = Gtk.Align.START;
            sources_empty_label.margin_bottom = 16;
            sources_page.append (sources_empty_label);

            sources_list_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 8);
            sources_page.append (sources_list_box);

            zones_tab_stack.add_named (sources_page, "sources");
            zones_tab_stack.visible_child_name = "zones";

            outer.append (zones_tab_stack);

            scroll.child = outer;
            this.child = scroll;

            update_master_controls ();
        }

        /* ---- Master volume (active renderer) ---- */

        private void update_master_controls () {
            var renderer_id = active_repo.active_renderer_id;
            var state = renderer_id.length > 0
                ? state_repo.get_state (renderer_id) : null;

            if (state == null || state.playback == null) {
                master_scale.sensitive = false;
                master_mute_btn.sensitive = false;
                return;
            }

            master_scale.sensitive = true;
            master_mute_btn.sensitive = true;

            updating_master = true;
            master_scale.set_value (state.playback.volume * 100.0);
            updating_master = false;

            master_pct_label.label = "%d%%".printf (
                (int) (state.playback.volume * 100.0));
            master_mute_btn.icon_name = state.playback.mute
                ? "audio-volume-muted-symbolic" : "audio-volume-high-symbolic";
            master_mute_btn.tooltip_text = state.playback.mute ? "Unmute" : "Mute";
        }

        private void debounce_master_volume (double volume) {
            if (master_volume_timer != 0) {
                Source.remove (master_volume_timer);
            }
            master_volume_timer = Timeout.add (150, () => {
                master_volume_timer = 0;
                send_renderer_command ("playback.setVolume",
                    PlaybackBodies.set_volume (volume));
                return Source.REMOVE;
            });
        }

        private void on_master_mute () {
            var renderer_id = active_repo.active_renderer_id;
            var state = renderer_id.length > 0
                ? state_repo.get_state (renderer_id) : null;
            var muted = (state != null && state.playback != null)
                ? state.playback.mute : false;
            send_renderer_command ("playback.setMute",
                PlaybackBodies.set_mute (!muted));
        }

        private void send_renderer_command (string cmd_type, Json.Object body) {
            var renderer_id = active_repo.active_renderer_id;
            if (renderer_id.length == 0) return;

            lease_mgr.ensure_lease.begin (renderer_id, (obj, res) => {
                var lease = lease_mgr.ensure_lease.end (res);
                if (lease != null) {
                    correlator.send_fire_and_forget (renderer_id, cmd_type, body, lease);
                }
            });
        }

        /* ---- Sources tab ---- */

        private void rebuild_sources () {
            Gtk.Widget? child = sources_list_box.get_first_child ();
            while (child != null) {
                var next = child.get_next_sibling ();
                sources_list_box.remove (child);
                child = next;
            }

            /* Aggregate distinct sources across zone controllers */
            var sources = new GenericArray<PresenceSource> ();
            var seen = new HashTable<string, bool> (str_hash, str_equal);
            var controllers = node_repo.get_zone_controllers ();
            for (uint i = 0; i < controllers.length; i++) {
                var ctrl_sources = controllers[i].sources;
                if (ctrl_sources == null) continue;
                for (uint j = 0; j < ctrl_sources.length; j++) {
                    if (seen.contains (ctrl_sources[j].id)) continue;
                    seen.insert (ctrl_sources[j].id, true);
                    sources.add (ctrl_sources[j]);
                }
            }

            sources_empty_label.visible = (sources.length == 0);

            var zones = node_repo.get_zones ();

            for (uint i = 0; i < sources.length; i++) {
                sources_list_box.append (build_source_card (sources[i], zones));
            }
        }

        private Gtk.Box build_source_card (PresenceSource source,
                                            GenericArray<Presence> zones) {
            var card = new Gtk.Box (Gtk.Orientation.VERTICAL, 8);
            card.add_css_class ("mu-card");

            uint assigned = 0;
            for (uint i = 0; i < zones.length; i++) {
                if (get_zone_source_id (zones[i].node_id) == source.id) assigned++;
            }

            var header = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);

            var name_label = new Gtk.Label (source.name);
            name_label.add_css_class ("renderer-name");
            name_label.halign = Gtk.Align.START;
            name_label.hexpand = true;
            name_label.ellipsize = Pango.EllipsizeMode.END;
            header.append (name_label);

            var count_label = new Gtk.Label ("%u / %u ZONES".printf (assigned, zones.length));
            count_label.add_css_class ("meta-label");
            header.append (count_label);

            card.append (header);

            /* Zone assignment checkboxes */
            for (uint i = 0; i < zones.length; i++) {
                var zone = zones[i];
                var row = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);

                var check = new Gtk.CheckButton.with_label (zone.name);
                check.active = (get_zone_source_id (zone.node_id) == source.id);
                check.sensitive = get_zone_connected (zone.node_id);
                var zone_id = zone.node_id;
                var source_id = source.id;
                check.toggled.connect (() => {
                    send_source_command (zone_id, check.active ? source_id : "");
                });
                row.append (check);

                card.append (row);
            }

            return card;
        }

        private void connect_signals () {
            node_added_id = node_repo.node_added.connect (on_node_added);
            node_removed_id = node_repo.node_removed.connect (on_node_removed);
            node_updated_id = node_repo.node_updated.connect (on_node_updated);
            state_changed_id = zone_state_repo.state_changed.connect (on_state_changed);

            renderer_state_id = state_repo.state_changed.connect ((node_id, state) => {
                if (node_id == active_repo.active_renderer_id) {
                    update_master_controls ();
                }
            });
            active_changed_id = active_repo.active_renderer_changed.connect ((node_id) => {
                update_master_controls ();
            });
        }

        public override void dispose () {
            if (node_added_id != 0) {
                node_repo.disconnect (node_added_id);
                node_added_id = 0;
            }
            if (node_removed_id != 0) {
                node_repo.disconnect (node_removed_id);
                node_removed_id = 0;
            }
            if (node_updated_id != 0) {
                node_repo.disconnect (node_updated_id);
                node_updated_id = 0;
            }
            if (state_changed_id != 0) {
                zone_state_repo.disconnect (state_changed_id);
                state_changed_id = 0;
            }
            if (renderer_state_id != 0) {
                state_repo.disconnect (renderer_state_id);
                renderer_state_id = 0;
            }
            if (active_changed_id != 0) {
                active_repo.disconnect (active_changed_id);
                active_changed_id = 0;
            }
            if (master_volume_timer != 0) {
                Source.remove (master_volume_timer);
                master_volume_timer = 0;
            }
            if (volume_timers != null) {
                volume_timers.foreach ((zone_id, timer_id) => {
                    Source.remove (timer_id);
                });
                volume_timers.remove_all ();
            }
            base.dispose ();
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

            var existing = card_map.lookup (presence.node_id);
            if (existing != null) {
                zone_list_box.remove (existing);
            }

            var card = build_zone_card (presence);
            zone_list_box.append (card);
            card_map.set (presence.node_id, card);
        }

        private void on_state_changed (string node_id, ZoneState state) {
            if (zones_tab_stack.visible_child_name == "sources") {
                rebuild_sources ();
            }
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
            if (!get_zone_connected (presence.node_id)) {
                card.add_css_class ("zone-card-offline");
            }
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
            vol_scale.sensitive = get_zone_connected (presence.node_id);
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

            vol_row.sensitive = get_zone_connected (presence.node_id);
            card.append (vol_row);

            /* --- Source selector row (only if sources available) --- */
            var sources = get_zone_sources (presence);
            if (sources != null && sources.length > 0) {
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

                var source_dropdown = build_source_dropdown (presence, sources);
                source_dropdown.hexpand = true;
                source_dropdown.halign = Gtk.Align.END;
                source_dropdown.sensitive = get_zone_connected (presence.node_id);
                source_row.append (source_dropdown);

                card.append (source_row);
            }

            return card;
        }

        private Gtk.DropDown build_source_dropdown (Presence presence,
                                                    GenericArray<PresenceSource> sources) {
            var string_list = new Gtk.StringList (null);
            string_list.append ("No source");
            uint active_idx = 0;
            var current_source = get_zone_source_id (presence.node_id);

            for (uint i = 0; i < sources.length; i++) {
                var src = sources[i];
                string_list.append (src.name);
                if (src.id == current_source) {
                    active_idx = i + 1;
                }
            }

            var dropdown = new Gtk.DropDown (string_list, null);
            dropdown.selected = active_idx;

            /* Store sources reference for lookup in handler */
            var node_id = presence.node_id;

            dropdown.notify["selected"].connect (() => {
                /* Ignore programmatic updates from state echoes, otherwise we
                 * bounce the source selection straight back as a command. */
                if (updating_dropdown) return;
                var idx = dropdown.selected;
                if (idx == Gtk.INVALID_LIST_POSITION) return;

                if (idx == 0) {
                    send_source_command (node_id, "");
                    return;
                }

                var source_index = idx - 1;
                if (source_index < sources.length) {
                    send_source_command (node_id, sources[source_index].id);
                }
            });

            return dropdown;
        }

        private void update_zone_state (string node_id, ZoneState state) {
            var card = card_map.lookup (node_id);
            if (card == null) return;

            if (state.connected) {
                card.remove_css_class ("zone-card-offline");
            } else {
                card.add_css_class ("zone-card-offline");
            }

            var presence = node_repo.get_node (node_id);
            if (presence != null) {
                var top_row = card.get_first_child () as Gtk.Box;
                if (top_row != null) {
                    var name_label = top_row.get_first_child () as Gtk.Label;
                    var source_label = name_label != null
                        ? name_label.get_next_sibling () as Gtk.Label : null;
                    if (source_label != null) {
                        source_label.label = get_current_source_name (presence);
                    }
                }
            }

            /* Navigate to volume row (second child of card) */
            var top_row = card.get_first_child ();
            if (top_row == null) return;

            var vol_row = top_row.get_next_sibling () as Gtk.Box;
            if (vol_row == null) return;

            /* Mute button (first child of vol_row) */
            var mute_btn = vol_row.get_first_child () as Gtk.Button;
            if (mute_btn != null) {
                mute_btn.icon_name = state.mute
                    ? "audio-volume-muted-symbolic" : "audio-volume-high-symbolic";
                mute_btn.tooltip_text = state.mute ? "Unmute" : "Mute";
            }

            /* Volume scale (second child) */
            var vol_scale = mute_btn != null
                ? mute_btn.get_next_sibling () as Gtk.Scale : null;
            if (vol_scale != null) {
                updating_slider = true;
                vol_scale.set_value (state.volume * 100.0);
                updating_slider = false;
            }

            /* Percentage label (third child) */
            var pct_label = vol_scale != null
                ? vol_scale.get_next_sibling () as Gtk.Label : null;
            if (pct_label != null) {
                pct_label.label = "%d%%".printf ((int) (state.volume * 100.0));
            }

            vol_row.sensitive = state.connected;
            if (presence != null) {
                update_source_dropdown (card, presence);
            }
        }

        private void update_source_dropdown (Gtk.Box card, Presence presence) {
            var sources = get_zone_sources (presence);
            if (sources == null || sources.length == 0) return;

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

            var current_source = get_zone_source_id (presence.node_id);
            uint selected = 0;
            for (uint i = 0; i < sources.length; i++) {
                if (sources[i].id == current_source) {
                    selected = i + 1;
                    break;
                }
            }

            if (dropdown.selected != selected) {
                updating_dropdown = true;
                dropdown.selected = selected;
                updating_dropdown = false;
            }

            dropdown.sensitive = get_zone_connected (presence.node_id);
        }

        /* ---- Commands ---- */

        private void send_volume_command (string node_id, double volume) {
            var body = new Json.Object ();
            body.set_string_member ("zoneId", node_id);
            body.set_double_member ("volume", volume);
            send_zone_command (node_id, "zone.setVolume", body);
        }

        private void send_mute_command (string node_id, bool mute) {
            var body = new Json.Object ();
            body.set_string_member ("zoneId", node_id);
            body.set_boolean_member ("mute", mute);
            send_zone_command (node_id, "zone.setMute", body);
        }

        private void send_source_command (string node_id, string source_id) {
            var body = new Json.Object ();
            body.set_string_member ("zoneId", node_id);
            body.set_string_member ("sourceId", source_id);
            send_zone_command (node_id, "zone.selectSource", body);
        }

        private void send_zone_command (string zone_id, string cmd_type, Json.Object body) {
            var controller_id = get_zone_controller_id (zone_id);
            if (controller_id == null || controller_id.length == 0) {
                warning ("ZonesView: no zone controller available for %s", zone_id);
                return;
            }
            correlator.send_fire_and_forget (controller_id, cmd_type, body);
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
            var source_id = get_zone_source_id (presence.node_id);
            if (source_id.length == 0) return "No source";

            var sources = get_zone_sources (presence);
            if (sources != null) {
                for (uint i = 0; i < sources.length; i++) {
                    if (sources[i].id == source_id) {
                        return sources[i].name;
                    }
                }
            }

            if (source_id.contains (":")) {
                return source_id.substring (source_id.last_index_of_char (':') + 1);
            }
            return source_id;
        }

        private double get_zone_volume (string node_id) {
            var state = zone_state_repo.get_state (node_id);
            return state != null ? state.volume : 0.0;
        }

        private bool get_zone_muted (string node_id) {
            var state = zone_state_repo.get_state (node_id);
            return state != null ? state.mute : false;
        }

        private bool get_zone_connected (string node_id) {
            var state = zone_state_repo.get_state (node_id);
            return state != null ? state.connected : true;
        }

        private string get_zone_source_id (string node_id) {
            var state = zone_state_repo.get_state (node_id);
            return state != null ? state.source_id : "";
        }

        private GenericArray<PresenceSource>? get_zone_sources (Presence presence) {
            if (presence.sources != null && presence.sources.length > 0) {
                return presence.sources;
            }

            if (presence.controller_id.length > 0) {
                var controller = node_repo.get_node (presence.controller_id);
                if (controller != null && controller.sources != null &&
                    controller.sources.length > 0) {
                    return controller.sources;
                }
            }

            var controllers = node_repo.get_zone_controllers ();
            if (controllers.length > 0 && controllers[0].sources != null &&
                controllers[0].sources.length > 0) {
                return controllers[0].sources;
            }

            return null;
        }

        private string? get_zone_controller_id (string zone_id) {
            var zone = node_repo.get_node (zone_id);
            if (zone != null && zone.controller_id.length > 0) {
                return zone.controller_id;
            }

            var controllers = node_repo.get_zone_controllers ();
            if (controllers.length > 0) {
                return controllers[0].node_id;
            }
            return null;
        }
    }
}
