/* gst_driver.vala — GStreamer playbin wrapper with spectrum element for FFT visualization. */

namespace Mu {

    public class GstDriver : Object {
        private Gst.Element? pipeline = null;
        private Gst.Bin? audio_bin = null;
        private uint bus_watch_id = 0;

        private const int SPECTRUM_BANDS = 28;
        private const int SPECTRUM_THRESHOLD = -80;
        private const uint64 SPECTRUM_INTERVAL = 50 * Gst.MSECOND;

        /* ---- Properties ---- */

        private double _volume = 0.7;
        public double volume {
            get { return _volume; }
            set {
                _volume = value;
                apply_volume ();
            }
        }

        private bool _muted = false;
        public bool muted {
            get { return _muted; }
            set {
                _muted = value;
                apply_volume ();
            }
        }

        /* ---- Signals ---- */

        public signal void track_finished ();
        public signal void spectrum_data (float[] magnitudes);
        public signal void playback_error (string message);

        /* ---- Playback control ---- */

        public void play (string url, int64 start_ms = 0) {
            teardown ();

            pipeline = Gst.ElementFactory.make ("playbin", "playbin");
            if (pipeline == null) {
                warning ("GstDriver: failed to create playbin element");
                return;
            }

            pipeline.set_property ("uri", url);

            /* Build custom audio bin: tee -> (queue->autoaudiosink) + (queue->spectrum->fakesink) */
            audio_bin = build_audio_bin ();
            if (audio_bin != null) {
                pipeline.set_property ("audio-sink", audio_bin);
            }

            apply_volume ();
            install_bus_watch ();

            pipeline.set_state (Gst.State.PLAYING);

            if (start_ms > 0) {
                /* Seek after transitioning to PLAYING — schedule on idle to let pipeline start */
                Idle.add (() => {
                    if (pipeline != null) {
                        seek_to (start_ms);
                    }
                    return Source.REMOVE;
                });
            }
        }

        public void pause () {
            if (pipeline != null) {
                pipeline.set_state (Gst.State.PAUSED);
            }
        }

        public void resume () {
            if (pipeline != null) {
                pipeline.set_state (Gst.State.PLAYING);
            }
        }

        public void stop () {
            teardown ();
        }

        public void seek_to (int64 position_ms) {
            if (pipeline == null) return;

            var pos_ns = position_ms * Gst.MSECOND;
            pipeline.seek_simple (
                Gst.Format.TIME,
                Gst.SeekFlags.FLUSH | Gst.SeekFlags.KEY_UNIT,
                pos_ns
            );
        }

        public bool query_position (out int64 pos_ms, out int64 dur_ms) {
            pos_ms = 0;
            dur_ms = 0;

            if (pipeline == null) return false;

            int64 pos_ns = 0;
            int64 dur_ns = 0;

            bool got_pos = pipeline.query_position (Gst.Format.TIME, out pos_ns);
            bool got_dur = pipeline.query_duration (Gst.Format.TIME, out dur_ns);

            if (got_pos && pos_ns >= 0) {
                pos_ms = pos_ns / (int64) Gst.MSECOND;
            }
            if (got_dur && dur_ns >= 0) {
                dur_ms = dur_ns / (int64) Gst.MSECOND;
            }

            return got_pos;
        }

        /* ---- Audio bin construction ---- */

        private Gst.Bin? build_audio_bin () {
            var bin = new Gst.Bin ("audio_bin");

            var tee = Gst.ElementFactory.make ("tee", "tee");
            var queue_audio = Gst.ElementFactory.make ("queue", "queue_audio");
            var sink = Gst.ElementFactory.make ("autoaudiosink", "audio_sink");
            var queue_spectrum = Gst.ElementFactory.make ("queue", "queue_spectrum");
            var spectrum = Gst.ElementFactory.make ("spectrum", "spectrum");
            var fakesink = Gst.ElementFactory.make ("fakesink", "fake_sink");

            if (tee == null || queue_audio == null || sink == null ||
                queue_spectrum == null || spectrum == null || fakesink == null) {
                warning ("GstDriver: failed to create audio bin elements");
                return null;
            }

            /* Configure spectrum */
            spectrum.set_property ("bands", SPECTRUM_BANDS);
            spectrum.set_property ("threshold", SPECTRUM_THRESHOLD);
            spectrum.set_property ("interval", SPECTRUM_INTERVAL);
            spectrum.set_property ("post-messages", true);

            /* Configure queues to prevent deadlocks */
            queue_audio.set_property ("max-size-buffers", 0);
            queue_audio.set_property ("max-size-bytes", 0);
            queue_audio.set_property ("max-size-time", (uint64) 0);
            queue_spectrum.set_property ("max-size-buffers", 0);
            queue_spectrum.set_property ("max-size-bytes", 0);
            queue_spectrum.set_property ("max-size-time", (uint64) 0);

            /* Fakesink: sync=true so spectrum gets real-time data, async=false */
            fakesink.set_property ("sync", true);
            fakesink.set_property ("async", false);

            bin.add_many (tee, queue_audio, sink, queue_spectrum, spectrum, fakesink);

            /* Link: tee -> queue_audio -> sink */
            if (!queue_audio.link (sink)) {
                warning ("GstDriver: failed to link queue_audio -> sink");
                return null;
            }

            /* Link: tee -> queue_spectrum -> spectrum -> fakesink */
            if (!queue_spectrum.link_many (spectrum, fakesink)) {
                warning ("GstDriver: failed to link spectrum chain");
                return null;
            }

            /* Request pads from tee and link to queues */
            var tee_audio_pad = tee.request_pad_simple ("src_%u");
            var tee_spectrum_pad = tee.request_pad_simple ("src_%u");
            var queue_audio_sink = queue_audio.get_static_pad ("sink");
            var queue_spectrum_sink = queue_spectrum.get_static_pad ("sink");

            if (tee_audio_pad == null || tee_spectrum_pad == null ||
                queue_audio_sink == null || queue_spectrum_sink == null) {
                warning ("GstDriver: failed to get pads for tee linking");
                return null;
            }

            if (tee_audio_pad.link (queue_audio_sink) != Gst.PadLinkReturn.OK) {
                warning ("GstDriver: failed to link tee -> queue_audio");
                return null;
            }
            if (tee_spectrum_pad.link (queue_spectrum_sink) != Gst.PadLinkReturn.OK) {
                warning ("GstDriver: failed to link tee -> queue_spectrum");
                return null;
            }

            /* Ghost pad: expose tee's sink pad as the bin's sink pad */
            var tee_sink = tee.get_static_pad ("sink");
            if (tee_sink != null) {
                var ghost = new Gst.GhostPad ("sink", tee_sink);
                bin.add_pad (ghost);
            }

            return bin;
        }

        /* ---- Bus watch ---- */

        private void install_bus_watch () {
            if (pipeline == null) return;

            var bus = pipeline.get_bus ();
            if (bus == null) return;

            bus_watch_id = bus.add_watch (Priority.DEFAULT, on_bus_message);
        }

        private bool on_bus_message (Gst.Bus bus, Gst.Message msg) {
            switch (msg.type) {
                case Gst.MessageType.EOS:
                    track_finished ();
                    break;

                case Gst.MessageType.ERROR:
                    GLib.Error err;
                    string debug_info;
                    msg.parse_error (out err, out debug_info);
                    warning ("GstDriver: pipeline error: %s (%s)", err.message, debug_info);
                    playback_error (err.message);
                    track_finished ();
                    break;

                case Gst.MessageType.ELEMENT:
                    unowned var structure = msg.get_structure ();
                    if (structure != null && structure.get_name () == "spectrum") {
                        parse_spectrum_message (structure);
                    }
                    break;

                default:
                    break;
            }
            return true;  /* Keep watching */
        }

        private void parse_spectrum_message (Gst.Structure structure) {
            unowned GLib.Value? magnitude_value = structure.get_value ("magnitude");
            if (magnitude_value == null) return;

            var magnitudes = new float[SPECTRUM_BANDS];
            for (int i = 0; i < SPECTRUM_BANDS; i++) {
                unowned GLib.Value? val = Gst.ValueList.get_value (magnitude_value, (uint) i);
                if (val != null) {
                    magnitudes[i] = (float) val.get_float ();
                }
            }

            spectrum_data (magnitudes);
        }

        /* ---- Volume ---- */

        private void apply_volume () {
            if (pipeline == null) return;
            pipeline.set_property ("volume", _muted ? 0.00001 : _volume);
        }

        /* ---- Teardown ---- */

        private void teardown () {
            if (bus_watch_id != 0) {
                Source.remove (bus_watch_id);
                bus_watch_id = 0;
            }

            if (pipeline != null) {
                pipeline.set_state (Gst.State.NULL);
                pipeline = null;
            }

            audio_bin = null;
        }
    }
}
