/* main.vala — Media Utopia desktop application entry point */

int main (string[] args) {
    /* GStreamer must be initialised once at process start before any
     * Gst.ElementFactory.make call. The local renderer's GstDriver
     * builds its playbin lazily on first play, so without this every
     * play attempt logs `gst_is_initialized() failed` and silently
     * fails to construct the pipeline. */
    Gst.init (ref args);

    var app = new Mu.Application ();
    return app.run (args);
}
