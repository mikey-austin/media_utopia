/* audio_visualizer.vala — 28-bar FFT audio frequency visualizer.
 * Renders GStreamer spectrum data as lime-colored vertical bars with
 * smooth rise/decay animation.
 */

namespace Mu {

    public class AudioVisualizer : Gtk.DrawingArea {

        private const int NUM_BARS = 28;
        private const float BAR_GAP = 2.0f;
        private const float BAR_RADIUS = 1.5f;

        /* Smoothing blend factors */
        private const float RISE_FACTOR = 0.7f;   /* fast rise: 70% target */
        private const float DECAY_FACTOR = 0.15f;  /* slow decay: 15% target */

        /* Lime color components (0.8, 1.0, 0.0 = #CCFF00) */
        private const double COLOR_R = 0.8;
        private const double COLOR_G = 1.0;
        private const double COLOR_B = 0.0;

        private float[] current_magnitudes;
        private float[] target_magnitudes;

        public AudioVisualizer () {
            current_magnitudes = new float[NUM_BARS];
            target_magnitudes = new float[NUM_BARS];

            for (int i = 0; i < NUM_BARS; i++) {
                current_magnitudes[i] = 0.0f;
                target_magnitudes[i] = 0.0f;
            }

            add_css_class ("visualizer");
            height_request = 48;
            hexpand = true;

            set_draw_func (draw_bars);
        }

        /**
         * Called with raw FFT magnitudes from GStreamer spectrum element.
         * Values are typically in the range -80dB to 0dB.
         */
        public void update_magnitudes (float[] magnitudes) {
            int count = int.min ((int) magnitudes.length, NUM_BARS);

            for (int i = 0; i < count; i++) {
                /* Normalize: -80..0 dB -> 0.0..1.0 */
                float norm = (magnitudes[i] + 80.0f) / 80.0f;
                norm = norm.clamp (0.0f, 1.0f);
                target_magnitudes[i] = norm;
            }

            /* Zero out any remaining bars */
            for (int i = count; i < NUM_BARS; i++) {
                target_magnitudes[i] = 0.0f;
            }

            /* Apply smoothing: fast rise, slow decay */
            for (int i = 0; i < NUM_BARS; i++) {
                float target = target_magnitudes[i];
                float current = current_magnitudes[i];

                if (target > current) {
                    /* Fast rise */
                    current_magnitudes[i] = current * (1.0f - RISE_FACTOR) + target * RISE_FACTOR;
                } else {
                    /* Slow decay */
                    current_magnitudes[i] = current * (1.0f - DECAY_FACTOR) + target * DECAY_FACTOR;
                }
            }

            queue_draw ();
        }

        /**
         * Clear all bars (e.g. when playback stops).
         */
        public void clear () {
            for (int i = 0; i < NUM_BARS; i++) {
                current_magnitudes[i] = 0.0f;
                target_magnitudes[i] = 0.0f;
            }
            queue_draw ();
        }

        /* ---- Drawing ---- */

        private void draw_bars (Gtk.DrawingArea area, Cairo.Context cr,
                                int width, int height) {
            /* Calculate bar dimensions */
            float total_gaps = BAR_GAP * (NUM_BARS - 1);
            float bar_width = ((float) width - total_gaps) / (float) NUM_BARS;

            if (bar_width < 1.0f) {
                bar_width = 1.0f;
            }

            for (int i = 0; i < NUM_BARS; i++) {
                float mag = current_magnitudes[i];

                /* Skip bars with negligible magnitude */
                if (mag < 0.005f) continue;

                float x = i * (bar_width + BAR_GAP);
                float bar_height = mag * (float) height;

                /* Minimum visible bar height */
                if (bar_height < 1.0f) {
                    bar_height = 1.0f;
                }

                float y = (float) height - bar_height;

                /* Variable alpha: 0.4 to 1.0 based on magnitude */
                double alpha = 0.4 + (double) mag * 0.6;

                cr.set_source_rgba (COLOR_R, COLOR_G, COLOR_B, alpha);

                /* Draw rounded-top rectangle */
                draw_rounded_top_rect (cr, (double) x, (double) y,
                                       (double) bar_width, (double) bar_height,
                                       (double) BAR_RADIUS);
                cr.fill ();
            }
        }

        /**
         * Draw a rectangle with rounded top corners and square bottom corners.
         */
        private static void draw_rounded_top_rect (Cairo.Context cr,
                                                    double x, double y,
                                                    double w, double h,
                                                    double r) {
            /* If the bar is too short for rounding, just draw a plain rect */
            if (h < r * 2 || w < r * 2) {
                cr.rectangle (x, y, w, h);
                return;
            }

            cr.new_sub_path ();

            /* Start at bottom-left */
            cr.move_to (x, y + h);

            /* Left edge -> top-left rounded corner */
            cr.line_to (x, y + r);
            cr.arc (x + r, y + r, r, Math.PI, 1.5 * Math.PI);

            /* Top edge -> top-right rounded corner */
            cr.arc (x + w - r, y + r, r, 1.5 * Math.PI, 2.0 * Math.PI);

            /* Right edge -> bottom-right square corner */
            cr.line_to (x + w, y + h);

            /* Close path (bottom edge) */
            cr.close_path ();
        }
    }
}
