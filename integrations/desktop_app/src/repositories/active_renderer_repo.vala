/* active_renderer_repo.vala — Persists the currently selected renderer via GSettings. */

namespace Mu {

    public class ActiveRendererRepository : Object {
        private Settings settings;
        private string default_id;

        public string active_renderer_id { get; private set; default = ""; }
        public signal void active_renderer_changed (string node_id);

        public ActiveRendererRepository (Settings settings, string default_renderer_id) {
            this.settings = settings;
            this.default_id = default_renderer_id;

            /* Read initial value from GSettings */
            var stored = settings.get_string ("active-renderer-id");
            if (stored.length > 0) {
                active_renderer_id = stored;
            } else {
                active_renderer_id = default_renderer_id;
            }
        }

        /**
         * Change the active renderer and persist to GSettings.
         */
        public void set_active_renderer (string node_id) {
            if (node_id == active_renderer_id) return;

            active_renderer_id = node_id;
            settings.set_string ("active-renderer-id", node_id);
            active_renderer_changed (node_id);
        }
    }
}
