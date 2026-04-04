/*
 * Minimal GStreamer bindings for duration query only.
 */

[CCode (cheader_filename = "gst/gst.h")]
namespace Gst {

    [CCode (cname = "GstFormat", cprefix = "GST_FORMAT_", has_type_id = false)]
    public enum Format {
        UNDEFINED = 0,
        DEFAULT = 1,
        BYTES = 2,
        TIME = 3,
        BUFFERS = 4,
        PERCENT = 5
    }

    [CCode (cname = "GstElement", cheader_filename = "gst/gst.h", type_id = "gst_element_get_type ()")]
    public class Element : GLib.Object {
        [CCode (cname = "gst_element_query_duration")]
        public bool query_duration (Format format, out int64 duration);
        [CCode (cname = "gst_element_query_position")]
        public bool query_position (Format format, out int64 position);
    }
}
