/*
 * GTK3 sidebar panel for selecting and controlling MU renderers.
 * Minimal UI: renderer dropdown + lease management.
 * RB's own transport controls handle playback.
 */

namespace Mu {

    public class ControllerPanel : Gtk.Box {

        private weak Controller controller;

        /* Widgets. */
        private Gtk.ComboBoxText renderer_combo;
        private Gtk.Label lease_status_label;
        private Gtk.Button lease_btn;

        /* Map combo index -> node_id. */
        private GenericArray<string> renderer_ids;

        public ControllerPanel (Controller controller) {
            Object (orientation: Gtk.Orientation.VERTICAL, spacing: 4);
            this.controller = controller;
            this.renderer_ids = new GenericArray<string> ();
            this.margin = 6;

            build_ui ();
            connect_signals ();
            refresh_renderer_list ();
        }

        private void build_ui () {
            /* Header. */
            var header = new Gtk.Label (null);
            header.set_markup ("<b>Media Utopia</b>");
            header.halign = Gtk.Align.START;
            this.pack_start (header, false, false, 2);

            /* Renderer selector. */
            renderer_combo = new Gtk.ComboBoxText ();
            renderer_combo.changed.connect (on_renderer_selected);
            this.pack_start (renderer_combo, false, false, 2);

            /* Lease status. */
            lease_status_label = new Gtk.Label ("No active session");
            lease_status_label.halign = Gtk.Align.START;
            lease_status_label.ellipsize = Pango.EllipsizeMode.END;
            var lsc = lease_status_label.get_style_context ();
            lsc.add_class ("dim-label");
            this.pack_start (lease_status_label, false, false, 2);

            /* Lease button. */
            lease_btn = new Gtk.Button.with_label ("Take Control");
            lease_btn.clicked.connect (on_lease_btn_clicked);
            lease_btn.sensitive = false;
            this.pack_start (lease_btn, false, false, 2);
        }

        private void connect_signals () {
            controller.renderer_state_changed.connect (on_state_changed);
            controller.renderer_list_changed.connect (refresh_renderer_list);
            controller.lease_changed.connect (on_lease_changed);
        }

        /* ── Renderer selection ────────────────────────────── */

        private void refresh_renderer_list () {
            renderer_combo.remove_all ();
            renderer_ids = new GenericArray<string> ();

            renderer_combo.append_text ("Local (no MU)");
            renderer_ids.add ("");

            var renderers = controller.get_renderers ();
            for (int i = 0; i < renderers.length; i++) {
                var p = renderers[i];
                renderer_combo.append_text (p.name);
                renderer_ids.add (p.node_id);
            }

            /* Re-select current if still present. */
            string? sel = controller.get_selected_node_id ();
            if (sel != null) {
                for (int i = 0; i < renderer_ids.length; i++) {
                    if (renderer_ids[i] == sel) {
                        renderer_combo.active = i;
                        return;
                    }
                }
            }
            renderer_combo.active = 0;
        }

        private void on_renderer_selected () {
            int idx = renderer_combo.active;
            if (idx <= 0 || idx >= renderer_ids.length) {
                controller.select_renderer (null);
                lease_btn.sensitive = false;
                lease_status_label.label = "Local playback";
                return;
            }
            controller.select_renderer (renderer_ids[idx]);
            lease_btn.sensitive = true;
            lease_status_label.label = "Connecting...";
        }

        /* ── Lease ─────────────────────────────────────────── */

        private void on_lease_btn_clicked () {
            if (controller.has_lease ()) {
                controller.do_release_lease ();
            } else {
                controller.do_acquire_lease ();
            }
        }

        private void on_lease_changed (bool has_lease) {
            if (has_lease) {
                lease_btn.label = "Release Control";
                lease_status_label.label = "You have control";
            } else {
                lease_btn.label = "Take Control";
                update_lease_from_state ();
            }
            lease_btn.sensitive = controller.get_selected_node_id () != null;
        }

        /* ── State updates ─────────────────────────────────── */

        private void on_state_changed (RendererState? state) {
            if (state == null) {
                if (controller.get_selected_node_id () != null)
                    lease_status_label.label = "Renderer offline";
                return;
            }

            if (!controller.has_lease ()) {
                update_lease_from_state ();
            }
        }

        private void update_lease_from_state () {
            var state = controller.get_current_state ();
            if (state != null && state.session != null &&
                state.session.owner.length > 0) {
                lease_status_label.label = @"Controlled by: $(state.session.owner)";
            } else {
                lease_status_label.label = "No active session";
            }
        }
    }
}
