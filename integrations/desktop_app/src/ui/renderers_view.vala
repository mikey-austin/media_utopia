/* renderers_view.vala — Mu.RenderersView : Adw.Bin
 * Shows discovered renderers with selection and lease control.
 */

namespace Mu {

    public class RenderersView : Adw.Bin {

        /* Service dependencies */
        private NodeRepository node_repo;
        private RendererStateRepository state_repo;
        private ActiveRendererRepository active_repo;
        private LeaseManager lease_mgr;
        private MqttClient mqtt;

        /* Local renderer ID */
        private string local_renderer_id;

        /* Layout widgets */
        private Gtk.Box local_card;
        private Gtk.Label local_status_label;
        private Gtk.Label network_heading_label;
        private Gtk.Label scanning_label;
        private Gtk.ListBox renderer_list;

        /* Signal handler IDs for cleanup */
        private ulong node_added_id = 0;
        private ulong node_removed_id = 0;
        private ulong node_updated_id = 0;
        private ulong state_changed_id = 0;
        private ulong active_changed_id = 0;
        private ulong connection_changed_id = 0;

        /* Track renderer rows by node_id */
        private HashTable<string, Gtk.ListBoxRow> row_map;

        public RenderersView (NodeRepository node_repo,
                              RendererStateRepository state_repo,
                              ActiveRendererRepository active_repo,
                              LeaseManager lease_mgr,
                              MqttClient mqtt) {
            this.node_repo = node_repo;
            this.state_repo = state_repo;
            this.active_repo = active_repo;
            this.lease_mgr = lease_mgr;
            this.mqtt = mqtt;

            var hostname = Environment.get_host_name ();
            this.local_renderer_id = "mu:renderer:gstreamer:desktop:%s:default".printf (hostname);

            row_map = new HashTable<string, Gtk.ListBoxRow> (str_hash, str_equal);

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
            var title = new Gtk.Label ("Renderers");
            title.add_css_class ("section-title");
            title.halign = Gtk.Align.START;
            outer.append (title);

            var subtitle = new Gtk.Label ("Select an output destination");
            subtitle.add_css_class ("meta-label");
            subtitle.halign = Gtk.Align.START;
            subtitle.margin_top = 2;
            subtitle.margin_bottom = 20;
            outer.append (subtitle);

            /* --- "This PC" section --- */
            var pc_heading = new Gtk.Label ("THIS PC");
            pc_heading.add_css_class ("meta-label");
            pc_heading.halign = Gtk.Align.START;
            pc_heading.margin_bottom = 8;
            outer.append (pc_heading);

            local_card = build_local_card ();
            outer.append (local_card);

            /* --- "Discovered on Network" section --- */
            var net_box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);
            net_box.margin_top = 24;
            net_box.margin_bottom = 8;
            net_box.halign = Gtk.Align.START;

            network_heading_label = new Gtk.Label ("DISCOVERED ON NETWORK");
            network_heading_label.add_css_class ("meta-label");
            net_box.append (network_heading_label);

            scanning_label = new Gtk.Label ("Scanning\u2026");
            scanning_label.add_css_class ("scanning-label");
            scanning_label.visible = (mqtt.connection_state == ConnectionState.CONNECTED);
            net_box.append (scanning_label);

            outer.append (net_box);

            /* Renderer list */
            renderer_list = new Gtk.ListBox ();
            renderer_list.selection_mode = Gtk.SelectionMode.NONE;
            renderer_list.add_css_class ("boxed-list-none");
            renderer_list.set_sort_func (sort_renderers);
            renderer_list.row_activated.connect (on_row_activated);
            outer.append (renderer_list);

            scroll.child = outer;
            this.child = scroll;
        }

        private Gtk.Box build_local_card () {
            var card = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 12);
            card.add_css_class ("mu-card");
            update_local_active_class (card);

            /* Make clickable via gesture */
            var click = new Gtk.GestureClick ();
            click.released.connect (() => {
                active_repo.set_active_renderer (local_renderer_id);
            });
            card.add_controller (click);

            /* Left side: text */
            var text_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 4);
            text_box.hexpand = true;

            var name_label = new Gtk.Label (Environment.get_host_name ());
            name_label.add_css_class ("renderer-name");
            name_label.halign = Gtk.Align.START;
            text_box.append (name_label);

            local_status_label = new Gtk.Label ("LOCAL PLAYBACK");
            local_status_label.add_css_class ("renderer-status");
            local_status_label.halign = Gtk.Align.START;
            text_box.append (local_status_label);

            card.append (text_box);

            /* Right side: chip */
            var chip = new Gtk.Label ("THIS PC");
            chip.add_css_class ("device-chip");
            chip.valign = Gtk.Align.CENTER;
            card.append (chip);

            return card;
        }

        private void connect_signals () {
            node_added_id = node_repo.node_added.connect (on_node_added);
            node_removed_id = node_repo.node_removed.connect (on_node_removed);
            node_updated_id = node_repo.node_updated.connect (on_node_updated);
            state_changed_id = state_repo.state_changed.connect (on_state_changed);
            active_changed_id = active_repo.active_renderer_changed.connect (on_active_changed);
            connection_changed_id = mqtt.connection_changed.connect (on_connection_changed);
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
                state_repo.disconnect (state_changed_id);
                state_changed_id = 0;
            }
            if (active_changed_id != 0) {
                active_repo.disconnect (active_changed_id);
                active_changed_id = 0;
            }
            if (connection_changed_id != 0) {
                mqtt.disconnect (connection_changed_id);
                connection_changed_id = 0;
            }
            base.dispose ();
        }

        private void populate_initial () {
            /* Populate with already-discovered renderers */
            var renderers = node_repo.get_renderers ();
            for (uint i = 0; i < renderers.length; i++) {
                var presence = renderers[i];
                /* Skip local renderer — it has its own dedicated card */
                if (presence.node_id == local_renderer_id) continue;
                add_renderer_row (presence);
            }

            /* Update local card status */
            update_local_status ();
        }

        /* ---- Signal handlers ---- */

        private void on_node_added (Presence presence) {
            if (presence.kind != "renderer") return;
            if (presence.node_id == local_renderer_id) {
                update_local_status ();
                return;
            }
            if (row_map.contains (presence.node_id)) return;
            add_renderer_row (presence);
        }

        private void on_node_removed (string node_id) {
            var row = row_map.lookup (node_id);
            if (row != null) {
                renderer_list.remove (row);
                row_map.remove (node_id);
            }
        }

        private void on_node_updated (Presence presence) {
            if (presence.kind != "renderer") return;
            if (presence.node_id == local_renderer_id) {
                update_local_status ();
                return;
            }
            update_renderer_row (presence.node_id);
        }

        private void on_state_changed (string node_id, RendererState state) {
            if (node_id == local_renderer_id) {
                update_local_status ();
                return;
            }
            update_renderer_row (node_id);
        }

        private void on_active_changed (string node_id) {
            /* Update local card highlighting */
            update_local_active_class (local_card);

            /* Update all network rows */
            row_map.foreach ((id, row) => {
                update_row_active_class (row, id);
            });
        }

        private void on_connection_changed (ConnectionState state) {
            scanning_label.visible = (state == ConnectionState.CONNECTED);
        }

        private void on_row_activated (Gtk.ListBoxRow row) {
            var node_id = row.name;
            if (node_id != null && node_id.length > 0) {
                active_repo.set_active_renderer (node_id);
            }
        }

        /* ---- Row management ---- */

        private void add_renderer_row (Presence presence) {
            var row = new Gtk.ListBoxRow ();
            row.name = presence.node_id;
            row.add_css_class ("renderer-row");
            row.focusable = true;
            row.activatable = true;

            var card = build_renderer_card (presence);
            row.child = card;

            renderer_list.append (row);
            row_map.set (presence.node_id, row);
        }

        private Gtk.Box build_renderer_card (Presence presence) {
            var card = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 12);
            card.add_css_class ("mu-card");
            card.margin_top = 4;
            card.margin_bottom = 4;

            if (active_repo.active_renderer_id == presence.node_id) {
                card.add_css_class ("active");
            }

            /* Left: text info */
            var text_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 3);
            text_box.hexpand = true;

            var name_label = new Gtk.Label (presence.name);
            name_label.add_css_class ("renderer-name");
            name_label.halign = Gtk.Align.START;
            name_label.ellipsize = Pango.EllipsizeMode.END;
            text_box.append (name_label);

            var status_label = new Gtk.Label (get_status_text (presence.node_id));
            status_label.add_css_class ("renderer-status");
            status_label.halign = Gtk.Align.START;
            status_label.ellipsize = Pango.EllipsizeMode.END;
            text_box.append (status_label);

            /* Lease owner label (hidden by default) */
            var lease_label = new Gtk.Label ("");
            lease_label.add_css_class ("lease-owner-label");
            lease_label.halign = Gtk.Align.START;
            lease_label.visible = false;
            text_box.append (lease_label);

            card.append (text_box);

            /* Right: badges + release button */
            var right_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 4);
            right_box.valign = Gtk.Align.CENTER;
            right_box.halign = Gtk.Align.END;

            /* HiRes badge (hidden by default) */
            var hires = new Gtk.Label ("Hi-Res");
            hires.add_css_class ("hires-badge");
            hires.visible = false;
            hires.halign = Gtk.Align.END;
            right_box.append (hires);

            /* Release button (hidden by default) */
            var release_btn = new Gtk.Button.with_label ("Release");
            release_btn.add_css_class ("release-button");
            release_btn.visible = false;
            release_btn.halign = Gtk.Align.END;
            release_btn.clicked.connect (() => {
                lease_mgr.release_lease.begin (presence.node_id);
            });
            right_box.append (release_btn);

            card.append (right_box);

            /* Apply current state */
            apply_state_to_card (presence.node_id, status_label, lease_label, hires, release_btn);

            return card;
        }

        private void update_renderer_row (string node_id) {
            var row = row_map.lookup (node_id);
            if (row == null) return;

            /* Find the card box (the row's child) */
            var card = row.child as Gtk.Box;
            if (card == null) return;

            /* Traverse the card to find the status label, lease label, hires badge, release button */
            var text_box = card.get_first_child () as Gtk.Box;
            if (text_box == null) return;

            Gtk.Label? status_label = null;
            Gtk.Label? lease_label = null;
            Gtk.Widget? child = text_box.get_first_child ();

            /* Skip name label */
            if (child != null) child = child.get_next_sibling ();
            /* Status label */
            if (child != null) {
                status_label = child as Gtk.Label;
                child = child.get_next_sibling ();
            }
            /* Lease label */
            if (child != null) {
                lease_label = child as Gtk.Label;
            }

            /* Right box: hires badge, release button */
            var right_box = text_box.get_next_sibling () as Gtk.Box;
            Gtk.Label? hires = null;
            Gtk.Button? release_btn = null;

            if (right_box != null) {
                var rchild = right_box.get_first_child ();
                if (rchild != null) {
                    hires = rchild as Gtk.Label;
                    rchild = rchild.get_next_sibling ();
                }
                if (rchild != null) {
                    release_btn = rchild as Gtk.Button;
                }
            }

            /* Update status text */
            if (status_label != null) {
                status_label.label = get_status_text (node_id);
            }

            /* Update state-dependent widgets */
            apply_state_to_card (node_id, status_label, lease_label, hires, release_btn);

            /* Also update name in case presence changed */
            var presence = node_repo.get_node (node_id);
            if (presence != null) {
                var name_child = text_box.get_first_child () as Gtk.Label;
                if (name_child != null) {
                    name_child.label = presence.name;
                }
            }
        }

        private void apply_state_to_card (string node_id,
                                           Gtk.Label? status_label,
                                           Gtk.Label? lease_label,
                                           Gtk.Label? hires_badge,
                                           Gtk.Button? release_btn) {
            var state = state_repo.get_state (node_id);

            if (state == null) {
                if (lease_label != null) lease_label.visible = false;
                if (hires_badge != null) hires_badge.visible = false;
                if (release_btn != null) release_btn.visible = false;
                return;
            }

            /* HiRes badge: check metadata for format info */
            if (hires_badge != null) {
                hires_badge.visible = check_hires (state);
            }

            /* Session/lease info */
            if (state.session != null && state.session.owner.length > 0) {
                if (lease_label != null) {
                    lease_label.label = "Session: %s".printf (state.session.owner);
                    lease_label.visible = true;
                }
                if (release_btn != null) {
                    release_btn.visible = true;
                }
            } else {
                if (lease_label != null) lease_label.visible = false;
                if (release_btn != null) release_btn.visible = false;
            }
        }

        /* ---- Helpers ---- */

        private string get_status_text (string node_id) {
            var state = state_repo.get_state (node_id);
            if (state == null) return "Ready to stream";

            if (state.playback == null) return "Standby";

            var status = state.playback.status;

            if (status == "playing" || status == "paused") {
                /* Try to get title/artist from current item display block */
                if (state.current != null && state.current.display != null) {
                    var display = state.current.display;
                    var track_title = display.title;
                    var artist = display.artist_display ();

                    if (track_title.length > 0) {
                        var playing_prefix = (status == "playing") ? "Playing" : "Paused";
                        if (artist.length > 0) {
                            return "%s: %s \u2014 %s".printf (playing_prefix, track_title, artist);
                        }
                        return "%s: %s".printf (playing_prefix, track_title);
                    }
                }

                return (status == "playing") ? "Playing" : "Paused";
            }

            if (status == "stopped") return "Ready to stream";

            return "Standby";
        }

        private bool check_hires (RendererState state) {
            /* Display block does not carry sample rate / bit depth, so the
             * hi-res indicator is unavailable until the protocol surfaces
             * those fields again (e.g. via attributes on library.getItem). */
            return false;
        }

        private void update_local_status () {
            var state = state_repo.get_state (local_renderer_id);
            if (state == null) {
                local_status_label.label = "LOCAL PLAYBACK";
                return;
            }

            var text = get_status_text (local_renderer_id);
            local_status_label.label = (text.length > 0) ? text : "LOCAL PLAYBACK";
        }

        private void update_local_active_class (Gtk.Box card) {
            if (active_repo.active_renderer_id == local_renderer_id) {
                card.add_css_class ("active");
            } else {
                card.remove_css_class ("active");
            }
        }

        private void update_row_active_class (Gtk.ListBoxRow row, string node_id) {
            var card = row.child as Gtk.Box;
            if (card == null) return;

            if (active_repo.active_renderer_id == node_id) {
                card.add_css_class ("active");
            } else {
                card.remove_css_class ("active");
            }
        }

        private int sort_renderers (Gtk.ListBoxRow a, Gtk.ListBoxRow b) {
            return strcmp (a.name, b.name);
        }
    }
}
