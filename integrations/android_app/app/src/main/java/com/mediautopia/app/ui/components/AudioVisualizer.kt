package com.mediautopia.app.ui.components

import android.media.audiofx.Visualizer
import android.util.Log
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import com.mediautopia.app.ui.theme.Secondary

/**
 * Audio frequency visualizer with smooth animated bars.
 *
 * Uses Compose animation to interpolate between FFT snapshots,
 * eliminating jumpiness. Capture rate is set to maximum for
 * responsive updates.
 */
@Composable
fun AudioVisualizer(
    audioSessionId: Int,
    modifier: Modifier = Modifier,
    barCount: Int = 28,
    barColor: Color = Secondary,
) {
    // Raw magnitudes from FFT callback — updated at capture rate.
    var rawMagnitudes by remember { mutableStateOf(FloatArray(barCount)) }

    DisposableEffect(audioSessionId) {
        var visualizer: Visualizer? = null
        // Smoothed values that persist across captures for decay effect.
        val smoothed = FloatArray(barCount)

        try {
            visualizer = Visualizer(audioSessionId).apply {
                captureSize = Visualizer.getCaptureSizeRange()[1]
                setDataCaptureListener(
                    object : Visualizer.OnDataCaptureListener {
                        override fun onWaveFormDataCapture(
                            vis: Visualizer, waveform: ByteArray, samplingRate: Int,
                        ) {}

                        override fun onFftDataCapture(
                            vis: Visualizer, fft: ByteArray, samplingRate: Int,
                        ) {
                            val magnitudes = FloatArray(barCount)
                            // Use logarithmic frequency distribution for more
                            // musical visualization (more bars for low frequencies).
                            val fftSize = fft.size / 2
                            for (i in 0 until barCount) {
                                // Map bar index to FFT bin range using log scale.
                                val startBin = (fftSize * Math.pow(i.toDouble() / barCount, 1.8) ).toInt().coerceIn(0, fftSize - 1)
                                val endBin = (fftSize * Math.pow((i + 1).toDouble() / barCount, 1.8)).toInt().coerceIn(startBin + 1, fftSize)

                                var sum = 0f
                                var count = 0
                                for (bin in startBin until endBin) {
                                    val real = fft[bin * 2].toFloat()
                                    val imag = fft[bin * 2 + 1].toFloat()
                                    sum += kotlin.math.sqrt(real * real + imag * imag)
                                    count++
                                }
                                val avg = if (count > 0) sum / count else 0f
                                magnitudes[i] = (avg / 60f).coerceIn(0f, 1f)
                            }

                            // Smooth: rise fast, fall slow (exponential decay).
                            for (i in magnitudes.indices) {
                                smoothed[i] = if (magnitudes[i] > smoothed[i]) {
                                    smoothed[i] * 0.3f + magnitudes[i] * 0.7f  // fast rise
                                } else {
                                    smoothed[i] * 0.85f + magnitudes[i] * 0.15f // slow decay
                                }
                            }
                            rawMagnitudes = smoothed.copyOf()
                        }
                    },
                    Visualizer.getMaxCaptureRate(),
                    false,
                    true,
                )
                enabled = true
            }
        } catch (e: Exception) {
            Log.w("AudioVisualizer", "Failed to create Visualizer: ${e.message}")
        }

        onDispose {
            try {
                visualizer?.enabled = false
                visualizer?.release()
            } catch (_: Exception) {}
        }
    }

    // Animate each bar for smooth Compose transitions.
    val animatedBars = rawMagnitudes.map { target ->
        val animated by animateFloatAsState(
            targetValue = target,
            animationSpec = tween(durationMillis = 80),
            label = "bar",
        )
        animated
    }

    Canvas(
        modifier = modifier
            .fillMaxWidth()
            .height(64.dp),
    ) {
        val barWidth = size.width / (barCount * 1.6f)
        val gap = barWidth * 0.6f
        val totalWidth = barCount * (barWidth + gap) - gap
        val offsetX = (size.width - totalWidth) / 2
        val cornerR = barWidth / 3

        for (i in animatedBars.indices) {
            val magnitude = animatedBars[i]
            val barHeight = (magnitude * size.height).coerceAtLeast(2f)
            val x = offsetX + i * (barWidth + gap)
            val alpha = 0.4f + magnitude * 0.6f

            drawRoundRect(
                color = barColor.copy(alpha = alpha),
                topLeft = Offset(x, size.height - barHeight),
                size = Size(barWidth, barHeight),
                cornerRadius = CornerRadius(cornerR, cornerR),
            )
        }
    }
}
